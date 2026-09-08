package config_test

import (
	"strings"
	"testing"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestParseImportSourceRejectsTrailingJSON(t *testing.T) {
	data := []byte(`{"mcpServers":{"x":{"command":"node"}}} {"extra":true}`)
	if _, err := config.ParseImportSource(data, ""); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}

func TestImportPreviewRedactsArgsAndURLDetails(t *testing.T) {
	data := []byte(`{
		"mcpServers": {
			"stdio": {"command":"node","args":["--api-key","super-secret"]},
			"remote": {"type":"http","url":"https://example.com/private/token-path?token=secret"}
		}
	}`)
	preview, err := config.ImportServers(data, t.TempDir()+"/config.json", config.ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportServers: %v", err)
	}
	text := ""
	for _, item := range preview.ServersToAdd {
		text += item.Command + item.URL
		if item.ID == "stdio" && item.ArgCount != 2 {
			t.Fatalf("ArgCount = %d, want 2", item.ArgCount)
		}
		if item.ID == "remote" && item.URL != "https://example.com" {
			t.Fatalf("preview URL = %q", item.URL)
		}
	}
	for _, secret := range []string{"super-secret", "token-path", "token=secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("preview leaked %q: %s", secret, text)
		}
	}
}
