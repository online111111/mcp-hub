package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestEmbeddedAdminUsesCSPCompatibleModuleEvents(t *testing.T) {
	index, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	app, err := webFiles.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(index, []byte(`script type="module"`)) {
		t.Fatal("admin entrypoint is not loaded as a module")
	}
	if bytes.Contains(index, []byte("onclick=")) || bytes.Contains(app, []byte("onclick=")) {
		t.Fatal("inline event handler is incompatible with the admin CSP")
	}
}

func TestValidOriginUsesRequestOriginInLocalMode(t *testing.T) {
	h, err := New(Options{ConfigPath: "config.json", AdminToken: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/admin/v1/auth/login", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	if !h.validOrigin(req) {
		t.Fatal("same-origin local request was rejected")
	}
	req.Header.Set("Origin", "http://attacker.example")
	if h.validOrigin(req) {
		t.Fatal("cross-origin local request was accepted")
	}
}

func TestDecodeJSONBodyRejectsTrailingValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"token":"one"} {"token":"two"}`))
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(req, &body); err == nil {
		t.Fatal("expected trailing JSON value to be rejected")
	}
}

func TestDecodeJSONBodyRejectsOversizedInput(t *testing.T) {
	body := `{"token":"` + strings.Repeat("x", maxAdminBodySize) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	var target struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(req, &target); err == nil {
		t.Fatal("expected oversized JSON body to be rejected")
	}
}

func newAdminTest(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := []byte(`{"version":1,"hub":{"admin":{"enabled":true,"token":"${ADMIN_TOKEN}"}},"mcpServers":{"remote":{"enabled":true,"type":"streamable_http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer ${REMOTE_TOKEN}"},"tools":{"disabled":[]}}}}`)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_TOKEN", "admin-secret-0123456789-0123456789-abc")
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
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/v1/auth/login", strings.NewReader(`{"token":"admin-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	// Browsers send Origin on the same-origin fetch used by the admin page.
	request.Header.Set("Origin", server.URL)
	resp, err := client.Do(request)
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
	moduleRequest := httptest.NewRequest(http.MethodGet, "/admin/app-core.mjs", nil)
	moduleResponse := httptest.NewRecorder()
	h.ServeHTTP(moduleResponse, moduleRequest)
	if moduleResponse.Code != http.StatusOK || !strings.Contains(moduleResponse.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("unexpected module response %d (%s)", moduleResponse.Code, moduleResponse.Header().Get("Content-Type"))
	}
}

func TestAdminRollsBackConfigWhenLiveReloadFails(t *testing.T) {
	h, configPath := newAdminTest(t)
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	reloadCalls := 0
	h.opts.Reload = func(context.Context) error {
		reloadCalls++
		if reloadCalls == 1 {
			return errors.New("apply failed")
		}
		return nil
	}
	raw, digest, err := h.readRawConfig()
	if err != nil {
		t.Fatal(err)
	}
	delete(raw.MCPServers, "remote")
	request := httptest.NewRequest(http.MethodDelete, "/api/admin/v1/servers/remote", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	h.writeConfig(response, request, raw, digest)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected reload failure, got %d: %s", response.Code, response.Body.String())
	}
	if reloadCalls != 2 {
		t.Fatalf("expected failed apply plus runtime restore, got %d reload calls", reloadCalls)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("configuration was not rolled back\nwant: %s\n got: %s", original, restored)
	}
}

func TestAdminPreflightFailureDoesNotPersistOrReload(t *testing.T) {
	h, configPath := newAdminTest(t)
	original, err := config.ReadFileLimited(configPath)
	if err != nil {
		t.Fatal(err)
	}
	h.opts.Preflight = func(context.Context, *config.ResolvedConfig) error {
		return errors.New("server \"remote\" preflight failed: connection_refused")
	}
	reloads := 0
	h.opts.Reload = func(context.Context) error { reloads++; return nil }
	raw, digest, err := h.readRawConfig()
	if err != nil {
		t.Fatal(err)
	}
	srv := raw.MCPServers["remote"]
	srv.URL = "https://changed.example/mcp"
	raw.MCPServers["remote"] = srv
	req := httptest.NewRequest(http.MethodPut, "/api/admin/v1/servers/remote", nil)
	resp := httptest.NewRecorder()
	h.writeConfig(resp, req, raw, digest)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected preflight rejection, got %d: %s", resp.Code, resp.Body.String())
	}
	if reloads != 0 {
		t.Fatalf("reload ran despite failed preflight: %d", reloads)
	}
	after, err := config.ReadFileLimited(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("failed preflight changed the persisted configuration")
	}
}
