package config

import (
	"strings"
	"testing"
)

func TestValidatePublicModeRequirements(t *testing.T) {
	base := Config{Version: 1, Hub: HubConfig{Listen: "0.0.0.0:8080"}, MCPServers: map[string]ServerConfig{}}
	if err := Validate(&base); err == nil {
		t.Fatal("expected non-loopback bind rejection")
	}
	base.Hub.PublicMode = true
	if err := Validate(&base); err == nil || !strings.Contains(err.Error(), "publicUrl") {
		t.Fatalf("expected publicUrl requirement, got %v", err)
	}
	base.Hub.PublicURL = "https://hub.example.com"
	base.Hub.AllowedHosts = []string{"hub.example.com"}
	base.Hub.Auth.BearerToken = "${HUB_TOKEN}"
	base.Hub.Admin = AdminConfig{Enabled: true, Token: "${ADMIN_TOKEN}", SessionTimeout: "30m"}
	if err := Validate(&base); err != nil {
		t.Fatalf("expected valid public config: %v", err)
	}
}

func TestResolvePublicSecrets(t *testing.T) {
	cfg := &Config{Version: 1, Hub: HubConfig{Listen: "0.0.0.0:8080", PublicMode: true, PublicURL: "https://hub.example.com", AllowedHosts: []string{"hub.example.com"}, Auth: HubAuthConfig{BearerToken: "${HUB_TOKEN}"}, Admin: AdminConfig{Enabled: true, Token: "${ADMIN_TOKEN}"}}, MCPServers: map[string]ServerConfig{}}
	values := map[string]string{"HUB_TOKEN": "mcp-secret", "ADMIN_TOKEN": "admin-secret"}
	resolved, err := Resolve(cfg, ".", func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if resolved.BearerToken != "mcp-secret" || resolved.AdminToken != "admin-secret" || !resolved.PublicMode {
		t.Fatalf("unexpected resolved public config: %+v", resolved)
	}
}
