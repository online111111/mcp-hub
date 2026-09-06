package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// ImportOptions defines the options passed to the Import function.
type ImportOptions struct {
	RemoteType string // Optional override, e.g. "streamable_http"
	DryRun     bool   // If true, do not write changes to target file
	Yes        bool   // Explicit confirmation flag required to write
}

// ServerPreviewItem represents a sanitized preview of an imported server.
// It explicitly omits and redacts credentials and secret values.
type ServerPreviewItem struct {
	ID         string     `json:"id"`
	Type       ServerType `json:"type"`
	Command    string     `json:"command,omitempty"`
	Args       []string   `json:"args,omitempty"`
	Cwd        string     `json:"cwd,omitempty"`
	URL        string     `json:"url,omitempty"`
	HeaderKeys []string   `json:"headerKeys,omitempty"` // Only names of headers, no values
	EnvKeys    []string   `json:"envKeys,omitempty"`    // Only names of env vars, no values
	Enabled    bool       `json:"enabled"`
}

// ImportPreview represents the planned changes of an import operation.
type ImportPreview struct {
	TargetConfigPath string              `json:"targetConfigPath"`
	ExistingCount    int                 `json:"existingCount"`
	NewCount         int                 `json:"newCount"`
	ServersToAdd     []ServerPreviewItem `json:"serversToAdd"`
}

type importSourceContainer struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ParseImportSource strictly parses raw import JSON bytes.
func ParseImportSource(data []byte, remoteType string) (map[string]ServerConfig, error) {
	if len(data) > MaxConfigFileSize {
		return nil, ErrConfigFileTooLarge
	}

	if err := CheckDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("import source duplicate key check failed: %w", err)
	}

	var container importSourceContainer
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&container); err != nil {
		return nil, fmt.Errorf("import source JSON decode error: %w", err)
	}

	if len(container.MCPServers) == 0 {
		return nil, fmt.Errorf("import source contains no servers under 'mcpServers'")
	}

	normalized := make(map[string]ServerConfig, len(container.MCPServers))
	for id, s := range container.MCPServers {
		normServer := s

		// Normalize type based on 5.3 specifications
		if normServer.Type == "" {
			if normServer.Command != "" {
				normServer.Type = ServerTypeStdio
			} else if normServer.URL != "" {
				if remoteType == string(ServerTypeStreamableHTTP) {
					normServer.Type = ServerTypeStreamableHTTP
				} else {
					return nil, fmt.Errorf("server %q has URL but no type; specify --remote-type streamable_http to import", id)
				}
			} else {
				return nil, fmt.Errorf("server %q must have either command (for stdio) or url (for streamable_http)", id)
			}
		} else if normServer.Type == "http" {
			normServer.Type = ServerTypeStreamableHTTP
		} else if normServer.Type == "sse" {
			return nil, fmt.Errorf("server %q: sse transport is not supported in P0", id)
		}

		if err := validateServer(id, normServer); err != nil {
			return nil, fmt.Errorf("invalid server %q: %w", id, err)
		}

		normalized[id] = normServer
	}

	return normalized, nil
}

// ImportServers imports servers from sourceData into targetConfigPath according to opts.
func ImportServers(sourceData []byte, targetConfigPath string, opts ImportOptions) (*ImportPreview, error) {
	if opts.DryRun && opts.Yes {
		return nil, fmt.Errorf("--dry-run and --yes are mutually exclusive")
	}

	newServers, err := ParseImportSource(sourceData, opts.RemoteType)
	if err != nil {
		return nil, err
	}

	// Read or initialize target config
	var targetCfg Config
	var targetDigest string

	existingData, err := os.ReadFile(targetConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize default target config
			targetCfg = Config{
				Version: CurrentVersion,
				Hub: HubConfig{
					Listen: DefaultListen,
				},
				Defaults: DefaultsConfig{
					StartupTimeout: "20s",
					CallTimeout:    "60s",
					MaxConcurrency: DefaultMaxConcurrency,
				},
				MCPServers: make(map[string]ServerConfig),
			}
		} else {
			return nil, fmt.Errorf("failed to read target config: %w", err)
		}
	} else {
		targetDigest = ComputeDigest(existingData)
		if err := DecodeStrict(existingData, &targetCfg); err != nil {
			return nil, fmt.Errorf("target config is invalid: %w", err)
		}
		if err := Validate(&targetCfg); err != nil {
			return nil, fmt.Errorf("target config validation failed: %w", err)
		}
		if targetCfg.MCPServers == nil {
			targetCfg.MCPServers = make(map[string]ServerConfig)
		}
	}

	// Conflict detection and limit checks
	for id := range newServers {
		if _, exists := targetCfg.MCPServers[id]; exists {
			return nil, fmt.Errorf("server ID %q already exists in target config (overwriting is rejected by default)", id)
		}
	}

	totalServers := len(targetCfg.MCPServers) + len(newServers)
	if totalServers > MaxServers {
		return nil, fmt.Errorf("importing %d servers would result in %d servers, exceeding maximum limit of %d", len(newServers), totalServers, MaxServers)
	}

	// Prepare sanitized preview (credentials/secrets stripped)
	var previewItems []ServerPreviewItem
	for id, s := range newServers {
		var headerKeys []string
		for h := range s.Headers {
			headerKeys = append(headerKeys, h)
		}
		sort.Strings(headerKeys)

		var envKeys []string
		for e := range s.Env {
			envKeys = append(envKeys, e)
		}
		sort.Strings(envKeys)

		previewItems = append(previewItems, ServerPreviewItem{
			ID:         id,
			Type:       s.Type,
			Command:    s.Command,
			Args:       s.Args,
			Cwd:        s.Cwd,
			URL:        s.URL,
			HeaderKeys: headerKeys,
			EnvKeys:    envKeys,
			Enabled:    s.IsEnabled(),
		})
	}
	sort.Slice(previewItems, func(i, j int) bool {
		return previewItems[i].ID < previewItems[j].ID
	})

	preview := &ImportPreview{
		TargetConfigPath: targetConfigPath,
		ExistingCount:    len(targetCfg.MCPServers),
		NewCount:         len(newServers),
		ServersToAdd:     previewItems,
	}

	if opts.DryRun {
		return preview, nil
	}

	if !opts.Yes {
		return preview, fmt.Errorf("confirmation required: specify --yes to apply import to %q", targetConfigPath)
	}

	// Merge servers
	for id, s := range newServers {
		targetCfg.MCPServers[id] = s
	}

	// Serialize merged configuration
	outData, err := json.MarshalIndent(targetCfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged config: %w", err)
	}
	outData = append(outData, '\n')

	// Write atomically with CAS
	if err := WriteConfigFileAtomic(targetConfigPath, outData, targetDigest); err != nil {
		return nil, fmt.Errorf("failed to write updated config: %w", err)
	}

	return preview, nil
}
