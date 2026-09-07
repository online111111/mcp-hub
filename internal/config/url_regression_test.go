package config

import "testing"

func TestConfigRejectsHopByHopHeaders(t *testing.T) {
	for _, name := range []string{"Trailer", "TE", "Keep-Alive", "Proxy-Authorization", "Proxy-Connection"} {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{Version: 1, MCPServers: map[string]ServerConfig{"remote": {Type: ServerTypeStreamableHTTP, URL: "https://example.com/mcp", Headers: map[string]string{name: "value"}}}}
			if err := Validate(cfg); err == nil {
				t.Fatalf("accepted hop-by-hop header %q", name)
			}
		})
	}
}

func TestPublicURLRejectsNonOrigin(t *testing.T) {
	for _, raw := range []string{"https://hub.example/admin", "https://hub.example/%2f", "https://hub.example?", "https://hub.example/#", "https://:443"} {
		t.Run(raw, func(t *testing.T) {
			cfg := &Config{Version: 1, Hub: HubConfig{PublicMode: true, PublicURL: raw, AllowedHosts: []string{"hub.example"}, Auth: HubAuthConfig{BearerToken: "token"}}}
			if err := Validate(cfg); err == nil {
				t.Fatalf("accepted non-origin publicUrl %q", raw)
			}
		})
	}
}

func TestHTTPURLRejectsMissingHost(t *testing.T) {
	for _, raw := range []string{"https:", "https:///mcp", "https://:443/mcp", "https:opaque"} {
		t.Run(raw, func(t *testing.T) {
			if err := validateHTTPURL(raw); err == nil {
				t.Fatalf("accepted HTTP URL without host: %q", raw)
			}
		})
	}
}

func TestPublicURLAllowsOriginAndRootSlash(t *testing.T) {
	for _, raw := range []string{"https://hub.example", "https://hub.example/", "https://[::1]:8443"} {
		cfg := &Config{Version: 1, Hub: HubConfig{PublicMode: true, PublicURL: raw, AllowedHosts: []string{"hub.example"}, Auth: HubAuthConfig{BearerToken: "token"}}}
		if err := Validate(cfg); err != nil {
			t.Fatalf("valid origin %q rejected: %v", raw, err)
		}
	}
}
