package runtime

import (
	"context"
	"encoding/json"
	"github.com/online111111/mcp-manager/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReloadBoundsReadBeforeInvokingLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"mcpServers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	c := NewController(nil, path, config.DefaultListen, func(data []byte, path string) (*config.Config, *config.ResolvedConfig, error) {
		called = true
		return config.Parse(data, filepath.Dir(path))
	})
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", config.MaxConfigFileSize+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.ReloadNow(context.Background()); err == nil {
		t.Fatal("oversized config accepted")
	}
	if called {
		t.Fatal("unbounded snapshot reached loader")
	}
}

func TestReloadKeepsStartupAndMCPAuthDomainsSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	oldAdmin, newAdmin, mcpToken := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	cfg := config.Config{Version: 1, Hub: config.HubConfig{Admin: config.AdminConfig{Enabled: true, Token: oldAdmin}, Auth: config.HubAuthConfig{BearerToken: mcpToken}}}
	write := func() {
		t.Helper()
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	c := NewController(nil, path, config.DefaultListen, nil)
	cfg.Hub.Admin.Token = newAdmin
	cfg.Hub.Auth.BearerToken = oldAdmin
	write()
	if err := c.ReloadNow(context.Background()); err == nil {
		t.Fatal("reload accepted an active Admin credential as MCP credential")
	}
	if got := c.BearerTokens(); len(got) != 1 || got[0] != mcpToken {
		t.Fatal("rejected reload changed active credentials")
	}
}
