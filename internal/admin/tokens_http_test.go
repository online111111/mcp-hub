package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestAdminTokenCRUDThroughHTTP(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	primary := strings.Repeat("p", config.MinAuthTokenLength)
	adminEnv := strings.Repeat("a", config.MinAuthTokenLength)
	t.Setenv("MCP_PRIMARY", primary)
	t.Setenv("ADMIN_LONG", adminEnv)
	data := []byte(`{"version":1,"hub":{"auth":{"bearerToken":"${MCP_PRIMARY}"},"admin":{"enabled":true,"token":"${ADMIN_LONG}"}},"mcpServers":{}}`)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{ConfigPath: cfgPath, AdminToken: "admin-secret", SessionTimeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	client, csrf := login(t, ts)

	getTokens := func() ([]tokenDTO, string) {
		t.Helper()
		resp, err := client.Get(ts.URL + "/api/admin/v1/tokens")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tokens status %d", resp.StatusCode)
		}
		var body struct {
			Tokens []tokenDTO `json:"tokens"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Tokens, resp.Header.Get("ETag")
	}

	tokens, etag := getTokens()
	if len(tokens) != 1 || tokens[0].Token != primary || !tokens[0].Legacy {
		t.Fatalf("unexpected initial tokens: %#v", tokens)
	}

	add, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/admin/v1/tokens", strings.NewReader(`{"token":""}`))
	add.Header.Set("Content-Type", "application/json")
	add.Header.Set("If-Match", etag)
	add.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(add)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add token status %d", resp.StatusCode)
	}

	tokens, etag = getTokens()
	if len(tokens) != 2 || len(tokens[1].Token) < config.MinAuthTokenLength || tokens[1].Legacy {
		t.Fatalf("unexpected tokens after add: %#v", tokens)
	}

	remove, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/admin/v1/tokens/1", strings.NewReader(`{}`))
	remove.Header.Set("Content-Type", "application/json")
	remove.Header.Set("If-Match", etag)
	remove.Header.Set("X-CSRF-Token", csrf)
	resp, err = client.Do(remove)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete token status %d", resp.StatusCode)
	}

	tokens, _ = getTokens()
	if len(tokens) != 1 || tokens[0].Token != primary {
		t.Fatalf("unexpected tokens after delete: %#v", tokens)
	}
}
