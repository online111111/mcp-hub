package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestBearerTokenChangesHotReloadWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	one := strings.Repeat("a", config.MinAuthTokenLength)
	two := strings.Repeat("b", config.MinAuthTokenLength)
	write := func(tokens []string) {
		t.Helper()
		cfg := config.Config{
			Version: config.CurrentVersion,
			Hub: config.HubConfig{
				Listen: "127.0.0.1:8080",
				Auth: config.HubAuthConfig{BearerTokens: tokens},
			},
			MCPServers: map[string]config.ServerConfig{},
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	write([]string{one})
	controller := NewController(nil, path, "127.0.0.1:8080", nil)
	if got := controller.BearerTokens(); len(got) != 1 || got[0] != one {
		t.Fatalf("unexpected startup tokens: %#v", got)
	}

	write([]string{two})
	if err := controller.ReloadNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if controller.RestartRequired() {
		t.Fatal("MCP token-only change should not require restart")
	}
	if got := controller.BearerTokens(); len(got) != 1 || got[0] != two {
		t.Fatalf("hot-reloaded tokens not published: %#v", got)
	}
}
