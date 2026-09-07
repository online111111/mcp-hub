package runtime

import (
	"context"
	"encoding/json"
	"mcp-hub/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyTrustListsDoNotRequireRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"version":1,"hub":{"allowedHosts":[],"trustedProxies":[]},"mcpServers":{}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	c := NewController(nil, path, config.DefaultListen, nil)
	if err := c.ReloadNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.RestartRequired() {
		t.Fatal("unchanged empty trust lists incorrectly require restart")
	}
}

func TestReloadNowFailureUpdatesDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"mcpServers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewController(nil, path, config.DefaultListen, nil)
	if err := os.WriteFile(path, []byte(`invalid secret-config`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.ReloadNow(context.Background()); err == nil {
		t.Fatal("expected parse failure")
	}
	if c.LastReloadStatus() != "reload rejected: invalid configuration" {
		t.Fatalf("stale diagnostic: %s", c.LastReloadStatus())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := c.ReloadNow(context.Background()); err == nil {
		t.Fatal("expected read failure")
	}
	if c.LastReloadStatus() != "reload rejected: configuration unavailable" {
		t.Fatalf("stale diagnostic: %s", c.LastReloadStatus())
	}
}

func TestStartupBoundSettingsRequireRestart(t *testing.T) {
	cases := map[string]func(*config.HubConfig){
		"bearer token":    func(h *config.HubConfig) { h.Auth.BearerToken = "new-bearer-secret" },
		"admin token":     func(h *config.HubConfig) { h.Admin.Token = "new-admin-secret" },
		"admin enabled":   func(h *config.HubConfig) { h.Admin.Enabled = true; h.Admin.Token = "new-admin-secret" },
		"session timeout": func(h *config.HubConfig) { h.Admin.SessionTimeout = "2h" },
		"proxy trust":     func(h *config.HubConfig) { h.TrustedProxies = []string{"127.0.0.0/8"} },
		"allowed hosts":   func(h *config.HubConfig) { h.AllowedHosts = []string{"example.com"} },
		"public URL":      func(h *config.HubConfig) { h.PublicURL = "https://example.com" },
		"public mode": func(h *config.HubConfig) {
			h.PublicMode = true
			h.AllowedHosts = []string{"example.com"}
			h.PublicURL = "https://example.com"
			h.Auth.BearerToken = "new-bearer-secret"
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			original := []byte(`{"version":1,"mcpServers":{}}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			c := NewController(nil, path, config.DefaultListen, nil)
			raw := config.Config{Version: 1, MCPServers: map[string]config.ServerConfig{}}
			change(&raw.Hub)
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := c.ReloadNow(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !c.RestartRequired() {
				t.Fatal("startup-bound change incorrectly reported hot-reloaded")
			}
			if strings.Contains(c.LastReloadStatus(), "secret") {
				t.Fatal("status leaked a credential")
			}
			if err := c.ReloadNow(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !c.RestartRequired() {
				t.Fatal("restart warning cleared on repeated reload")
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := c.ReloadNow(context.Background()); err != nil {
				t.Fatal(err)
			}
			if c.RestartRequired() {
				t.Fatal("restoring startup settings did not clear warning")
			}
		})
	}
}
