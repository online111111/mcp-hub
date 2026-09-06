package config_test

import (
	"fmt"
	"strings"
	"testing"

	"mcp-hub/internal/config"
)

func TestValidate_Version(t *testing.T) {
	tests := []struct {
		name    string
		version int
		wantErr bool
	}{
		{"missing version", 0, true},
		{"wrong version 2", 2, true},
		{"wrong version negative", -1, true},
		{"valid version 1", 1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Version: tc.version,
			}
			err := config.Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidate_EnabledField(t *testing.T) {
	f := false
	tr := true

	sDisabled := config.ServerConfig{
		Enabled: &f,
		Type:    config.ServerTypeStdio,
		Command: "node",
	}
	if sDisabled.IsEnabled() != false {
		t.Errorf("expected IsEnabled() == false, got true")
	}

	sEnabled := config.ServerConfig{
		Enabled: &tr,
		Type:    config.ServerTypeStdio,
		Command: "node",
	}
	if sEnabled.IsEnabled() != true {
		t.Errorf("expected IsEnabled() == true, got false")
	}

	sOmitted := config.ServerConfig{
		Enabled: nil,
		Type:    config.ServerTypeStdio,
		Command: "node",
	}
	if sOmitted.IsEnabled() != true {
		t.Errorf("expected default IsEnabled() == true for omitted field, got false")
	}
}

func TestValidate_ListenAddress(t *testing.T) {
	tests := []struct {
		listen  string
		wantErr bool
	}{
		{"", false}, // Defaults to 127.0.0.1:8080
		{"127.0.0.1:8080", false},
		{"127.0.0.2:9000", false},
		{"localhost:8080", false},
		{"[::1]:8080", false},
		{"0.0.0.0:8080", true},       // Non-loopback
		{"192.168.1.100:8080", true}, // External
		{"invalid-address", true},
		{"127.0.0.1:999999", true}, // Port out of range
	}

	for _, tc := range tests {
		t.Run(tc.listen, func(t *testing.T) {
			cfg := &config.Config{
				Version: 1,
				Hub:     config.HubConfig{Listen: tc.listen},
			}
			err := config.Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() listen %q error = %v, wantErr = %v", tc.listen, err, tc.wantErr)
			}
		})
	}
}

func TestValidate_ServerID(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"a", false},
		{"my-server_1", false},
		{"fs-01", false},
		{"123startwithnumber", true},
		{"UPPERCASE", true},
		{"has.dot", true},
		{"has space", true},
		{"tool!", true},
		{"", true},
		{strings.Repeat("a", 32), false},
		{strings.Repeat("a", 33), true}, // Max 32 chars
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			cfg := &config.Config{
				Version: 1,
				MCPServers: map[string]config.ServerConfig{
					tc.id: {
						Type:    config.ServerTypeStdio,
						Command: "node",
					},
				},
			}
			err := config.Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() id %q error = %v, wantErr = %v", tc.id, err, tc.wantErr)
			}
		})
	}
}

func TestValidate_MutualExclusivity(t *testing.T) {
	// stdio with url
	cfg1 := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"bad-stdio": {
				Type:    config.ServerTypeStdio,
				Command: "node",
				URL:     "http://127.0.0.1:8080",
			},
		},
	}
	if err := config.Validate(cfg1); err == nil {
		t.Error("expected error for stdio server with URL, got nil")
	}

	// stdio with headers
	cfg2 := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"bad-stdio-hdr": {
				Type:    config.ServerTypeStdio,
				Command: "node",
				Headers: map[string]string{"Authorization": "Bearer xxx"},
			},
		},
	}
	if err := config.Validate(cfg2); err == nil {
		t.Error("expected error for stdio server with headers, got nil")
	}

	// streamable_http with command
	cfg3 := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"bad-http": {
				Type:    config.ServerTypeStreamableHTTP,
				URL:     "https://example.com/mcp",
				Command: "node",
			},
		},
	}
	if err := config.Validate(cfg3); err == nil {
		t.Error("expected error for streamable_http server with command, got nil")
	}

	// streamable_http with cwd
	cfg4 := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"bad-http-cwd": {
				Type: config.ServerTypeStreamableHTTP,
				URL:  "https://example.com/mcp",
				Cwd:  "/some/dir",
			},
		},
	}
	if err := config.Validate(cfg4); err == nil {
		t.Error("expected error for streamable_http server with cwd, got nil")
	}

	// sse transport rejection
	cfgSSE := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"sse-srv": {
				Type: "sse",
				URL:  "http://127.0.0.1:8080/sse",
			},
		},
	}
	if err := config.Validate(cfgSSE); err == nil || !strings.Contains(err.Error(), "sse transport is not supported in P0") {
		t.Errorf("expected explicit sse rejection, got: %v", err)
	}
}

func TestValidate_HTTPURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://remote.example.com/mcp", false},
		{"http://127.0.0.1:8080/mcp", false},
		{"http://localhost:8080/mcp", false},
		{"http://[::1]:8080/mcp", false},
		{"http://remote.example.com/mcp", true},     // Plain http to remote forbidden
		{"https://user:pass@example.com/mcp", true}, // Userinfo forbidden
		{"https://example.com/mcp#frag", true},      // Fragment forbidden
		{"ftp://example.com", true},                 // Unsupported scheme
	}

	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			cfg := &config.Config{
				Version: 1,
				MCPServers: map[string]config.ServerConfig{
					"srv": {
						Type: config.ServerTypeStreamableHTTP,
						URL:  tc.url,
					},
				},
			}
			err := config.Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() url %q error = %v, wantErr = %v", tc.url, err, tc.wantErr)
			}
		})
	}
}

func TestValidate_ForbiddenHeaders(t *testing.T) {
	forbidden := []string{
		"Host", "host", "HOST",
		"Content-Length", "content-length",
		"Connection", "connection",
		"Mcp-Session-Id", "mcp-session-id",
		"MCP-Protocol-Version", "mcp-protocol-version",
		"Transfer-Encoding", "upgrade",
	}

	for _, h := range forbidden {
		t.Run(h, func(t *testing.T) {
			cfg := &config.Config{
				Version: 1,
				MCPServers: map[string]config.ServerConfig{
					"srv": {
						Type: config.ServerTypeStreamableHTTP,
						URL:  "https://example.com/mcp",
						Headers: map[string]string{
							h: "val",
						},
					},
				},
			}
			err := config.Validate(cfg)
			if err == nil {
				t.Errorf("expected forbidden header error for %q, got nil", h)
			}
		})
	}
}

func TestValidate_TimeoutsAndConcurrency(t *testing.T) {
	// Negative duration
	cfg1 := &config.Config{
		Version: 1,
		Defaults: config.DefaultsConfig{
			CallTimeout: "-5s",
		},
	}
	if err := config.Validate(cfg1); err == nil {
		t.Error("expected error for negative callTimeout, got nil")
	}

	// > 24h duration
	cfg2 := &config.Config{
		Version: 1,
		Defaults: config.DefaultsConfig{
			StartupTimeout: "25h",
		},
	}
	if err := config.Validate(cfg2); err == nil {
		t.Error("expected error for startupTimeout > 24h, got nil")
	}

	// Concurrency out of range
	cfg3 := &config.Config{
		Version: 1,
		Defaults: config.DefaultsConfig{
			MaxConcurrency: 70,
		},
	}
	if err := config.Validate(cfg3); err == nil {
		t.Error("expected error for maxConcurrency > 64, got nil")
	}
}

func TestValidate_MaxServers(t *testing.T) {
	cfg := &config.Config{
		Version:    1,
		MCPServers: make(map[string]config.ServerConfig),
	}
	for i := 0; i < 33; i++ {
		cfg.MCPServers[fmt.Sprintf("s%02d", i)] = config.ServerConfig{
			Type:    config.ServerTypeStdio,
			Command: "node",
		}
	}

	err := config.Validate(cfg)
	if err == nil {
		t.Fatal("expected error when server count exceeds 32, got nil")
	}
}

func TestValidate_ToolsDisabled(t *testing.T) {
	// Duplicate tool in disabled list
	cfg1 := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"srv": {
				Type:    config.ServerTypeStdio,
				Command: "node",
				Tools: config.ToolsConfig{
					Disabled: []string{"tool1", "tool1"},
				},
			},
		},
	}
	if err := config.Validate(cfg1); err == nil {
		t.Error("expected error on duplicate tool name in tools.disabled, got nil")
	}

	// Empty string in disabled list
	cfg2 := &config.Config{
		Version: 1,
		MCPServers: map[string]config.ServerConfig{
			"srv": {
				Type:    config.ServerTypeStdio,
				Command: "node",
				Tools: config.ToolsConfig{
					Disabled: []string{""},
				},
			},
		},
	}
	if err := config.Validate(cfg2); err == nil {
		t.Error("expected error on empty tool name in tools.disabled, got nil")
	}
}
