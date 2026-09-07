package config

import "testing"

func TestResolveRejectsInvalidExpandedAuth(t *testing.T) {
	for _, tc := range []struct {
		name, bearer, admin string
		public, enabled     bool
	}{
		{"empty bearer", "", "admin", true, true},
		{"whitespace bearer", " \t", "admin", true, true},
		{"empty admin", "mcp", "", false, true},
		{"identical credentials", "same", "same", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Version: 1, Hub: HubConfig{PublicMode: tc.public, PublicURL: "https://hub.example", AllowedHosts: []string{"hub.example"}, Auth: HubAuthConfig{BearerToken: "${BEARER}"}, Admin: AdminConfig{Enabled: tc.enabled, Token: "${ADMIN}"}}}
			_, err := Resolve(cfg, t.TempDir(), func(k string) (string, bool) {
				if k == "BEARER" {
					return tc.bearer, true
				}
				return tc.admin, true
			})
			if err == nil {
				t.Fatal("accepted invalid expanded authentication configuration")
			}
		})
	}
}
