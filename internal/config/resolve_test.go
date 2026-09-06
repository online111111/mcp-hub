package config_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-hub/internal/config"
)

func TestResolve_PathsWithSpaces(t *testing.T) {
	configDir := filepath.FromSlash("C:/Project Spaces/Hub Config")
	cfg := &config.Config{
		Version: 1,
		Defaults: config.DefaultsConfig{
			StartupTimeout: "15s",
			CallTimeout:    "45s",
			MaxConcurrency: 10,
		},
		MCPServers: map[string]config.ServerConfig{
			"local": {
				Type:    config.ServerTypeStdio,
				Command: "./bin folder/my tool.exe",
				Args:    []string{"--data", "D:/data with spaces/dir"},
				Cwd:     "sub folder with spaces",
				Env: map[string]string{
					"VAR": "val",
				},
			},
			"npx-server": {
				Type:    config.ServerTypeStdio,
				Command: "npx",
				Args:    []string{"-y", "some-pkg"},
			},
		},
	}

	lookup := func(k string) (string, bool) {
		return "val", true
	}

	resolved, err := config.Resolve(cfg, configDir, lookup)
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}

	local := resolved.Servers["local"]

	// Relative command with separator should be joined with configDir
	expectedCmd := filepath.Join(configDir, "bin folder", "my tool.exe")
	if local.Command != expectedCmd {
		t.Errorf("expected command %q, got %q", expectedCmd, local.Command)
	}

	// Cwd with space should be joined with configDir
	expectedCwd := filepath.Join(configDir, "sub folder with spaces")
	if local.Cwd != expectedCwd {
		t.Errorf("expected cwd %q, got %q", expectedCwd, local.Cwd)
	}

	// Args should be left untouched
	if len(local.Args) != 2 || local.Args[1] != "D:/data with spaces/dir" {
		t.Errorf("expected args untouched, got %v", local.Args)
	}

	// Bare command without separators should remain bare
	npxSrv := resolved.Servers["npx-server"]
	if npxSrv.Command != "npx" {
		t.Errorf("expected bare command 'npx', got %q", npxSrv.Command)
	}

	// Default cwd when omitted should be configDir
	if npxSrv.Cwd != configDir {
		t.Errorf("expected default cwd %q, got %q", configDir, npxSrv.Cwd)
	}

	// Defaults inherited
	if local.StartupTimeout != 15*time.Second {
		t.Errorf("expected startup timeout 15s, got %v", local.StartupTimeout)
	}
	if local.CallTimeout != 45*time.Second {
		t.Errorf("expected call timeout 45s, got %v", local.CallTimeout)
	}
	if local.MaxConcurrency != 10 {
		t.Errorf("expected maxConcurrency 10, got %d", local.MaxConcurrency)
	}
}

func TestResolve_EnvAndSecretExpansion(t *testing.T) {
	configDir := filepath.FromSlash("D:/config")
	cfg := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"remote": {
				Type: config.ServerTypeStreamableHTTP,
				URL:  "https://api.example.com/mcp",
				Headers: map[string]string{
					"Authorization": "Bearer ${SECRET_TOKEN}",
				},
			},
		},
	}

	// 1. Missing secret
	_, err := config.Resolve(cfg, configDir, func(string) (string, bool) { return "", false })
	if err == nil || !strings.Contains(err.Error(), "SECRET_TOKEN") {
		t.Fatalf("expected error mentioning SECRET_TOKEN, got: %v", err)
	}

	// 2. Present secret
	resolved, err := config.Resolve(cfg, configDir, func(k string) (string, bool) {
		if k == "SECRET_TOKEN" {
			return "token-xyz-999", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}

	remoteSrv := resolved.Servers["remote"]
	if remoteSrv.Headers["Authorization"] != "Bearer token-xyz-999" {
		t.Errorf("expected expanded auth header, got %q", remoteSrv.Headers["Authorization"])
	}

	// Ensure raw cfg still contains unexpanded ${SECRET_TOKEN}
	if cfg.MCPServers["remote"].Headers["Authorization"] != "Bearer ${SECRET_TOKEN}" {
		t.Errorf("raw config was mutated! got %q", cfg.MCPServers["remote"].Headers["Authorization"])
	}
}

func TestResolve_DoesNotInheritHubAuthSecretsIntoStdioEnv(t *testing.T) {
	t.Setenv("MCP_HUB_TOKEN", "hub-mcp-secret")
	t.Setenv("MCP_HUB_ADMIN_TOKEN", "hub-admin-secret")

	cfg := &config.Config{
		Version: 1,
		Hub: config.HubConfig{
			Auth:  config.HubAuthConfig{BearerToken: "${MCP_HUB_TOKEN}"},
			Admin: config.AdminConfig{Enabled: true, Token: "${MCP_HUB_ADMIN_TOKEN}"},
		},
		MCPServers: map[string]config.ServerConfig{
			"local": {
				Type:    config.ServerTypeStdio,
				Command: "/bin/true",
				Env: map[string]string{
					"EXPLICIT_SHARED_SECRET": "${MCP_HUB_TOKEN}",
				},
			},
		},
	}

	resolved, err := config.Resolve(cfg, "/tmp", nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	env := resolved.Servers["local"].Env
	if _, ok := env["MCP_HUB_TOKEN"]; ok {
		t.Fatal("stdio environment inherited MCP_HUB_TOKEN")
	}
	if _, ok := env["MCP_HUB_ADMIN_TOKEN"]; ok {
		t.Fatal("stdio environment inherited MCP_HUB_ADMIN_TOKEN")
	}
	if got := env["EXPLICIT_SHARED_SECRET"]; got != "hub-mcp-secret" {
		t.Fatalf("explicit server env should still be allowed, got %q", got)
	}
}
