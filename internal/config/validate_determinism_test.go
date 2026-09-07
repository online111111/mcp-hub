package config

import "testing"

func TestValidateRejectsCaseInsensitiveHeaderDuplicates(t *testing.T) {
	cfg := &Config{
		Version: CurrentVersion,
		Hub:     HubConfig{Listen: DefaultListen},
		MCPServers: map[string]ServerConfig{
			"remote": {
				Type: ServerTypeStreamableHTTP,
				URL:  "https://example.com/mcp",
				Headers: map[string]string{
					"Authorization": "one",
					"authorization": "two",
				},
			},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected case-insensitive duplicate headers to be rejected")
	}
}

func TestValidateRejectsCaseInsensitiveEnvDuplicates(t *testing.T) {
	cfg := &Config{
		Version: CurrentVersion,
		Hub:     HubConfig{Listen: DefaultListen},
		MCPServers: map[string]ServerConfig{
			"local": {
				Type:    ServerTypeStdio,
				Command: "node",
				Env: map[string]string{
					"PATH": "one",
					"Path": "two",
				},
			},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected case-insensitive duplicate env keys to be rejected")
	}
}
