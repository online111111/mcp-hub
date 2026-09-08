package config

import (
	"strings"
	"testing"
)

func multiAuthConfig(auth HubAuthConfig) *Config {
	return &Config{
		Version: CurrentVersion,
		Hub: HubConfig{
			Listen:       "127.0.0.1:8080",
			PublicMode:   true,
			PublicURL:    "https://mcp.example.com",
			AllowedHosts: []string{"mcp.example.com"},
			Auth:         auth,
		},
		MCPServers: map[string]ServerConfig{},
	}
}

func TestResolveSupportsLegacyAndAdditionalBearerTokens(t *testing.T) {
	primary := strings.Repeat("p", MinAuthTokenLength)
	secondary := strings.Repeat("s", MinAuthTokenLength)
	cfg := multiAuthConfig(HubAuthConfig{
		BearerToken:  "${PRIMARY_TOKEN}",
		BearerTokens: []string{secondary},
	})
	lookup := func(name string) (string, bool) {
		if name == "PRIMARY_TOKEN" {
			return primary, true
		}
		return "", false
	}

	resolved, err := Resolve(cfg, t.TempDir(), lookup)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.BearerToken != primary {
		t.Fatalf("legacy first token mismatch: %q", resolved.BearerToken)
	}
	if len(resolved.BearerTokens) != 2 || resolved.BearerTokens[0] != primary || resolved.BearerTokens[1] != secondary {
		t.Fatalf("unexpected bearer token set: %#v", resolved.BearerTokens)
	}
}

func TestPublicModeAcceptsBearerTokensWithoutLegacyField(t *testing.T) {
	cfg := multiAuthConfig(HubAuthConfig{BearerTokens: []string{strings.Repeat("a", MinAuthTokenLength)}})
	if err := Validate(cfg); err != nil {
		t.Fatalf("additional token list should satisfy public auth: %v", err)
	}
	if _, err := Resolve(cfg, t.TempDir(), nil); err != nil {
		t.Fatalf("additional token list should resolve: %v", err)
	}
}

func TestResolveRejectsEffectiveDuplicateBearerTokens(t *testing.T) {
	token := strings.Repeat("d", MinAuthTokenLength)
	cfg := multiAuthConfig(HubAuthConfig{BearerToken: "${ONE}", BearerTokens: []string{"${TWO}"}})
	lookup := func(name string) (string, bool) {
		if name == "ONE" || name == "TWO" {
			return token, true
		}
		return "", false
	}
	if _, err := Resolve(cfg, t.TempDir(), lookup); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("expected effective duplicate rejection, got %v", err)
	}
}
