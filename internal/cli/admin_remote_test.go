package cli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-hub/internal/admin"
)

func newRemoteAdminCLITestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := []byte(`{
  "version": 1,
  "hub": {"listen": "127.0.0.1:8080"},
  "defaults": {"startupTimeout": "20s", "callTimeout": "60s", "maxConcurrency": 8},
  "mcpServers": {
    "remote": {
      "enabled": true,
      "type": "streamable_http",
      "url": "https://example.com/mcp",
      "headers": {"Authorization": "Bearer ${REMOTE_TOKEN}"},
      "tools": {"disabled": []}
    }
  }
}`)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REMOTE_TOKEN", "remote-secret")
	h, err := admin.New(admin.Options{ConfigPath: cfgPath, AdminToken: "admin-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func writeServerJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoteAdminCLIListGetAddEditDelete(t *testing.T) {
	ts := newRemoteAdminCLITestServer(t)
	base := []string{"--endpoint", ts.URL, "--token", "admin-secret"}

	var out, errOut bytes.Buffer
	code := runAdmin(append([]string{"list"}, base...), &out, &errOut)
	if code != ExitSuccess {
		t.Fatalf("list failed (%d): %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "remote\tstreamable_http\ttrue\thttps://example.com/mcp") {
		t.Fatalf("unexpected list output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = runAdmin(append([]string{"get", "remote"}, base...), &out, &errOut)
	if code != ExitSuccess {
		t.Fatalf("get failed (%d): %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "remote-secret") || !strings.Contains(out.String(), "__MCP_HUB_SECRET_SET__") {
		t.Fatalf("get did not preserve redaction: %s", out.String())
	}

	addFile := writeServerJSON(t, map[string]any{
		"enabled": true,
		"type":    "stdio",
		"command": "example-mcp",
		"args":    []string{"--serve"},
	})
	out.Reset()
	errOut.Reset()
	addArgs := append([]string{"add", "local", "--file", addFile}, base...)
	code = runAdmin(addArgs, &out, &errOut)
	if code != ExitSuccess {
		t.Fatalf("add failed (%d): %s", code, errOut.String())
	}

	editFile := writeServerJSON(t, map[string]any{
		"enabled": false,
		"type":    "stdio",
		"command": "example-mcp-v2",
		"args":    []string{"--serve", "--safe"},
	})
	out.Reset()
	errOut.Reset()
	editArgs := append([]string{"edit", "local", "--file", editFile}, base...)
	code = runAdmin(editArgs, &out, &errOut)
	if code != ExitSuccess {
		t.Fatalf("edit failed (%d): %s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = runAdmin(append([]string{"get", "local"}, base...), &out, &errOut)
	if code != ExitSuccess {
		t.Fatalf("get edited server failed (%d): %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "example-mcp-v2") || !strings.Contains(out.String(), `"enabled": false`) {
		t.Fatalf("edit was not persisted: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	deleteArgs := append([]string{"delete", "local", "--yes"}, base...)
	code = runAdmin(deleteArgs, &out, &errOut)
	if code != ExitSuccess {
		t.Fatalf("delete failed (%d): %s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = runAdmin(append([]string{"get", "local"}, base...), &out, &errOut)
	if code != ExitInvalidParams || !strings.Contains(errOut.String(), "does not exist") {
		t.Fatalf("deleted server still available: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestRemoteAdminCLIRequiresConfirmationAndRejectsRemoteHTTP(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runAdmin([]string{"delete", "x", "--endpoint", "https://hub.example.com", "--token", "admin"}, &out, &errOut)
	if code != ExitInvalidParams || !strings.Contains(errOut.String(), "--yes is required") {
		t.Fatalf("delete confirmation not enforced: code=%d err=%q", code, errOut.String())
	}

	errOut.Reset()
	if _, code := newRemoteAdminClient("http://hub.example.com", "admin", &errOut); code != ExitInvalidParams {
		t.Fatalf("remote plaintext HTTP was accepted: code=%d err=%q", code, errOut.String())
	}
}
