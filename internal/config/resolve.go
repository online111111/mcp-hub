package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Resolve validates the raw Config and evaluates runtime dependencies:
// - Expands ${NAME} references in env and headers (missing vars return an error).
// - Resolves relative paths (cwd and command with separators) against configDir.
// - Merges process environment with server explicit env.
// - Applies default timeouts and concurrency.
func Resolve(cfg *Config, configDir string, lookupEnv func(string) (string, bool)) (*ResolvedConfig, error) {
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	listen := cfg.Hub.Listen
	if listen == "" {
		listen = DefaultListen
	}

	bearerToken := ""
	if cfg.Hub.Auth.BearerToken != "" {
		var err error
		bearerToken, err = ExpandEnv(cfg.Hub.Auth.BearerToken, lookupEnv)
		if err != nil {
			return nil, fmt.Errorf("hub auth bearer token: %w", err)
		}
	}
	adminToken := ""
	if cfg.Hub.Admin.Token != "" {
		var err error
		adminToken, err = ExpandEnv(cfg.Hub.Admin.Token, lookupEnv)
		if err != nil {
			return nil, fmt.Errorf("hub admin token: %w", err)
		}
	}
	// Validate the effective credentials, not just the non-empty ${NAME} syntax.
	if cfg.Hub.PublicMode && strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("hub auth bearer token must not be empty after expansion")
	}
	if cfg.Hub.Admin.Enabled && strings.TrimSpace(adminToken) == "" {
		return nil, fmt.Errorf("hub admin token must not be empty after expansion")
	}
	if cfg.Hub.Admin.Enabled && bearerToken != "" && bearerToken == adminToken {
		return nil, fmt.Errorf("hub MCP and admin tokens must be distinct")
	}
	adminSessionTimeout := 30 * time.Minute
	if cfg.Hub.Admin.SessionTimeout != "" {
		adminSessionTimeout, _ = time.ParseDuration(cfg.Hub.Admin.SessionTimeout)
	}

	defaultStartup := DefaultStartupTimeout
	if cfg.Defaults.StartupTimeout != "" {
		d, err := time.ParseDuration(cfg.Defaults.StartupTimeout)
		if err == nil && d > 0 {
			defaultStartup = d
		}
	}

	defaultCall := DefaultCallTimeout
	if cfg.Defaults.CallTimeout != "" {
		d, err := time.ParseDuration(cfg.Defaults.CallTimeout)
		if err == nil && d > 0 {
			defaultCall = d
		}
	}

	defaultConcurrency := DefaultMaxConcurrency
	if cfg.Defaults.MaxConcurrency > 0 {
		defaultConcurrency = cfg.Defaults.MaxConcurrency
	}

	resolved := &ResolvedConfig{
		Version:             cfg.Version,
		Listen:              listen,
		PublicMode:          cfg.Hub.PublicMode,
		PublicURL:           strings.TrimRight(cfg.Hub.PublicURL, "/"),
		AllowedHosts:        append([]string(nil), cfg.Hub.AllowedHosts...),
		TrustedProxies:      append([]string(nil), cfg.Hub.TrustedProxies...),
		BearerToken:         bearerToken,
		AdminEnabled:        cfg.Hub.Admin.Enabled,
		AdminToken:          adminToken,
		AdminSessionTimeout: adminSessionTimeout,
		DefaultStartup:      defaultStartup,
		DefaultCall:         defaultCall,
		DefaultConcurrency:  defaultConcurrency,
		Servers:             make(map[string]ResolvedServer, len(cfg.MCPServers)),
	}

	// Downstream stdio servers receive only a compatibility baseline from the
	// Hub process environment. Secrets and service credentials are never
	// inherited implicitly; users can opt a variable in explicitly with
	// server.env, including via ${NAME} expansion.
	isWindows := runtime.GOOS == "windows"
	baseEnv := safeInheritedEnv(filterInheritedSecrets(os.Environ(), bearerToken, adminToken), isWindows)

	for id, s := range cfg.MCPServers {
		rs := ResolvedServer{
			ID:             id,
			Enabled:        s.IsEnabled(),
			Type:           s.Type,
			StartupTimeout: defaultStartup,
			CallTimeout:    defaultCall,
			MaxConcurrency: defaultConcurrency,
			DisabledTools:  make(map[string]struct{}, len(s.Tools.Disabled)),
		}

		for _, dt := range s.Tools.Disabled {
			rs.DisabledTools[dt] = struct{}{}
		}

		if s.StartupTimeout != "" {
			d, err := time.ParseDuration(s.StartupTimeout)
			if err == nil && d > 0 {
				rs.StartupTimeout = d
			}
		}
		if s.CallTimeout != "" {
			d, err := time.ParseDuration(s.CallTimeout)
			if err == nil && d > 0 {
				rs.CallTimeout = d
			}
		}
		if s.MaxConcurrency > 0 {
			rs.MaxConcurrency = s.MaxConcurrency
		}

		switch s.Type {
		case ServerTypeStdio:
			// Resolve command path if it has directory separators
			cmd := s.Command
			if strings.ContainsAny(cmd, `/\\`) {
				if !filepath.IsAbs(cmd) && configDir != "" {
					cmd = filepath.Clean(filepath.Join(configDir, cmd))
				}
			}
			rs.Command = cmd

			// Copy args untouched
			rs.Args = make([]string, len(s.Args))
			copy(rs.Args, s.Args)

			// Resolve cwd
			cwd := s.Cwd
			if cwd == "" {
				cwd = configDir
			} else if !filepath.IsAbs(cwd) && configDir != "" {
				cwd = filepath.Clean(filepath.Join(configDir, cwd))
			}
			rs.Cwd = cwd

			// Expand explicit env
			expandedEnv := make(map[string]string, len(s.Env))
			for k, v := range s.Env {
				expandedVal, err := ExpandEnv(v, lookupEnv)
				if err != nil {
					return nil, fmt.Errorf("server %q env %q: %w", id, k, err)
				}
				expandedEnv[k] = expandedVal
			}

			// Merge with base environment
			rs.Env = MergeEnv(baseEnv, expandedEnv, isWindows)

		case ServerTypeStreamableHTTP:
			rs.URL = s.URL
			rs.Headers = make(map[string]string, len(s.Headers))
			for k, v := range s.Headers {
				expandedVal, err := ExpandEnv(v, lookupEnv)
				if err != nil {
					return nil, fmt.Errorf("server %q header %q: %w", id, k, err)
				}
				rs.Headers[k] = expandedVal
			}
		}

		resolved.Servers[id] = rs
	}

	return resolved, nil
}

func filterInheritedSecrets(base []string, secrets ...string) []string {
	if len(base) == 0 {
		return nil
	}
	out := make([]string, 0, len(base))
	for _, entry := range base {
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			out = append(out, entry)
			continue
		}
		value := entry[eq+1:]
		blocked := false
		for _, secret := range secrets {
			if secret != "" && value == secret {
				blocked = true
				break
			}
		}
		if !blocked {
			out = append(out, entry)
		}
	}
	return out
}
