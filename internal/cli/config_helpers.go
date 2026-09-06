package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mcp-hub/internal/config"
)

// ValidateConfig reads, strictly parses, validates and resolves a configuration file from disk.
// It never starts child processes or performs network calls.
func ValidateConfig(configPath string) (*config.Config, *config.ResolvedConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config file %q: %w", configPath, err)
	}

	var cfg config.Config
	if err := config.DecodeStrict(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("strict decode failed: %w", err)
	}

	if err := config.Validate(&cfg); err != nil {
		return nil, nil, fmt.Errorf("validation failed: %w", err)
	}

	configDir := filepath.Dir(configPath)
	resolved, err := config.Resolve(&cfg, configDir, os.LookupEnv)
	if err != nil {
		return &cfg, nil, fmt.Errorf("environment resolution failed: %w", err)
	}

	return &cfg, resolved, nil
}

// RunImport performs an import from a source configuration file into a target configuration file.
func RunImport(fromPath, targetConfigPath string, remoteType string, dryRun, yes bool) (*config.ImportPreview, error) {
	data, err := os.ReadFile(fromPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read import source file %q: %w", fromPath, err)
	}

	opts := config.ImportOptions{
		RemoteType: remoteType,
		DryRun:     dryRun,
		Yes:        yes,
	}

	return config.ImportServers(data, targetConfigPath, opts)
}

// FormatPreview formats an ImportPreview for terminal display, guaranteeing that
// no secret values or credentials are displayed.
func FormatPreview(preview *config.ImportPreview) string {
	if preview == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Target Config: %s\n", preview.TargetConfigPath))
	sb.WriteString(fmt.Sprintf("Existing Servers: %d | New Servers to Add: %d\n\n", preview.ExistingCount, preview.NewCount))

	for i, s := range preview.ServersToAdd {
		sb.WriteString(fmt.Sprintf("[%d] Server ID: %s (Type: %s, Enabled: %v)\n", i+1, s.ID, s.Type, s.Enabled))
		if s.Type == config.ServerTypeStdio {
			sb.WriteString(fmt.Sprintf("    Command: %s\n", s.Command))
			if len(s.Args) > 0 {
				sb.WriteString(fmt.Sprintf("    Args: %s\n", strings.Join(s.Args, " ")))
			}
			if s.Cwd != "" {
				sb.WriteString(fmt.Sprintf("    Cwd: %s\n", s.Cwd))
			}
			if len(s.EnvKeys) > 0 {
				sb.WriteString(fmt.Sprintf("    Environment Keys: %s\n", strings.Join(s.EnvKeys, ", ")))
			}
		} else if s.Type == config.ServerTypeStreamableHTTP {
			sb.WriteString(fmt.Sprintf("    URL: %s\n", s.URL))
			if len(s.HeaderKeys) > 0 {
				sb.WriteString(fmt.Sprintf("    Configured Header Names: %s\n", strings.Join(s.HeaderKeys, ", ")))
			}
		}
	}

	return sb.String()
}
