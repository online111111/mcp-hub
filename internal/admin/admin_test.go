package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newAdminTest(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := []byte(`{"version":1,"hub":{"admin":{"enabled":true,"token":"${ADMIN_TOKEN}"}},"mcpServers":{"remote":{"enabled":true,"type":"streamable_http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer ${REMOTE_TOKEN}"},"tools":{"disabled":[]}}}}`)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_TOKEN", "admin-secret")
	t.Setenv("REMOTE_TOKEN", "remote-secret")
	h, err := New(Options{ConfigPath: cfgPath, AdminToken: "admin-secret", SessionTimeout: time.Hour, Status: func() any { return map[string]any{"ok": true} }})
	if err != nil {
		t.Fatal(err)
	}
	return h, cfgPath
}

func login(t *testing.T, server *httptest.Server) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Post(server.URL+"/api/admin/v1/auth/login", "application/json", strings.NewReader(`{"token":"admin-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var body struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return client, body.CSRF
}

func TestAdminLoginConfigRedactionAndCSRF(t *testing.T) {
	h, _ := newAdminTest(t)
	ts := httptest.NewServer(h)
	defer ts.Close()
	client, csrf := login(t, ts)

	resp, err := client.Get(ts.URL + "/api/admin/v1/config")
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(resp.Body)
	resp.Body.Close()
	if strings.Contains(raw.String(), "remote-secret") || strings.Contains(raw.String(), "Bearer ${REMOTE_TOKEN}") {
		t.Fatalf("secret leaked: %s", raw.String())
	}
	if !strings.Contains(raw.String(), secretSentinel) {
		t.Fatalf("expected secret sentinel: %s", raw.String())
	}
	etag := resp.Header.Get("ETag")

	request, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/admin/v1/servers/remote", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", etag)
	withoutCSRF, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	withoutCSRF.Body.Close()
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("expected CSRF rejection, got %d", withoutCSRF.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/admin/v1/servers/remote", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", etag)
	request.Header.Set("X-CSRF-Token", csrf)
	deleted, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("delete status %d", deleted.StatusCode)
	}
}

func TestAdminRejectsCrossOriginAndStaleCAS(t *testing.T) {
	h, _ := newAdminTest(t)
	ts := httptest.NewServer(h)
	defer ts.Close()
	client, csrf := login(t, ts)

	cross, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/admin/v1/config", nil)
	cross.Header.Set("Origin", "https://evil.example")
	resp, err := client.Do(cross)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected origin rejection, got %d", resp.StatusCode)
	}

	body := `{"enabled":true,"type":"streamable_http","url":"https://example.com/mcp","tools":{"disabled":[]}}`
	put, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/admin/v1/servers/new", strings.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	put.Header.Set("If-Match", "stale")
	put.Header.Set("X-CSRF-Token", csrf)
	resp, err = client.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", resp.StatusCode)
	}
}

func TestEmbeddedAdminPage(t *testing.T) {
	h, _ := newAdminTest(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "MCP Hub") {
		t.Fatalf("unexpected page response %d: %s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
}
