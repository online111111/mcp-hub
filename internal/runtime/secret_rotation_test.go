package runtime

import (
	"testing"

	"mcp-hub/internal/config"
)

func TestStripStartupSecretsKeepsActiveTokensOutOfStdioEnv(t *testing.T) {
	startup := &config.ResolvedConfig{BearerToken: "old-mcp", AdminToken: "old-admin"}
	resolved := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"stdio": {
				ID:   "stdio",
				Type: config.ServerTypeStdio,
				Env: map[string]string{
					"OLD_MCP":   "old-mcp",
					"OLD_ADMIN": "old-admin",
					"SAFE":      "keep-me",
				},
			},
			"http": {
				ID:   "http",
				Type: config.ServerTypeStreamableHTTP,
				Headers: map[string]string{"Authorization": "Bearer old-mcp"},
			},
		},
	}

	stripStartupSecrets(resolved, startup)
	stdio := resolved.Servers["stdio"]
	if _, ok := stdio.Env["OLD_MCP"]; ok {
		t.Fatal("active MCP token remained in stdio environment")
	}
	if _, ok := stdio.Env["OLD_ADMIN"]; ok {
		t.Fatal("active admin token remained in stdio environment")
	}
	if got := stdio.Env["SAFE"]; got != "keep-me" {
		t.Fatalf("safe env changed: %q", got)
	}
}
