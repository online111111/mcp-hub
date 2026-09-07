package config_test

import (
	"testing"

	"mcp-hub/internal/config"
)

func TestResolve_StdioAmbientSecretsRequireExplicitOptIn(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "github-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("HTTP_PROXY", "http://user:pass@example.invalid")
	t.Setenv("MCP_EXPLICIT_SECRET", "explicit-secret")

	cfg := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"local": {
				Type:    config.ServerTypeStdio,
				Command: "example-mcp",
				Env: map[string]string{
					"FORWARDED_SECRET": "${MCP_EXPLICIT_SECRET}",
				},
			},
		},
	}

	resolved, err := config.Resolve(cfg, "/tmp", nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	env := resolved.Servers["local"].Env
	for _, name := range []string{"GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "HTTP_PROXY", "MCP_EXPLICIT_SECRET"} {
		if _, ok := env[name]; ok {
			t.Fatalf("stdio environment inherited ambient %s", name)
		}
	}
	if got := env["FORWARDED_SECRET"]; got != "explicit-secret" {
		t.Fatalf("explicit server.env opt-in = %q, want explicit-secret", got)
	}
}
