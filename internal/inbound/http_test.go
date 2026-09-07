package inbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeManager implements ManagerCallback for testing.
type fakeManager struct {
	mu              sync.Mutex
	ready           bool
	restartRequired bool
	reloadStatus    string
	servers         []ServerStatusDTO
	calls           []RecentCallDTO
}

func (f *fakeManager) IsReady() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ready
}

func (f *fakeManager) GetServerStatuses() []ServerStatusDTO {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.servers
}

func (f *fakeManager) GetRecentCalls() []RecentCallDTO {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeManager) RestartRequired() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restartRequired
}

func (f *fakeManager) LastReloadStatus() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloadStatus
}

// setupTestServer creates and starts an HTTPServer on an ephemeral port.
func setupTestServer(t *testing.T, mgr ManagerCallback, opts *HTTPServerOptions) (*HTTPServer, func()) {
	t.Helper()

	ln, err := BindLoopbackListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("BindLoopbackListener failed: %v", err)
	}

	server := NewHubServer("test-hub", "0.1.0")
	pub, err := NewPublisher(server, nil, nil)
	if err != nil {
		ln.Close()
		t.Fatalf("NewPublisher failed: %v", err)
	}

	httpSrv, err := NewHTTPServer(ln, pub, mgr, opts)
	if err != nil {
		ln.Close()
		t.Fatalf("NewHTTPServer failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		_ = httpSrv.Close()
	}

	return httpSrv, cleanup
}

func TestBindLoopbackListener(t *testing.T) {
	// Success on ephemeral port
	ln, err := BindLoopbackListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("expected success on loopback ephemeral, got: %v", err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.Port <= 0 {
		t.Fatalf("expected valid TCP port, got: %v", ln.Addr())
	}

	// Port collision
	portStr := fmt.Sprintf("127.0.0.1:%d", tcpAddr.Port)
	_, err = BindLoopbackListener(portStr)
	if err == nil {
		t.Fatalf("expected port conflict error on %s, got nil", portStr)
	}

	// Non-loopback rejection
	_, err = BindLoopbackListener("192.168.1.100:8080")
	if err == nil || !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("expected not loopback error, got: %v", err)
	}

	// Invalid format
	_, err = BindLoopbackListener("invalid-listen")
	if err == nil {
		t.Fatalf("expected invalid format error, got nil")
	}
}

func TestSecurityMiddleware_HostAndOrigin(t *testing.T) {
	srv, cleanup := setupTestServer(t, nil, nil)
	defer cleanup()

	port := srv.Port()
	client := &http.Client{Timeout: 5 * time.Second}

	cases := []struct {
		name       string
		host       string
		origin     string
		fwdHost    string
		wantStatus int
	}{
		{
			name:       "valid 127.0.0.1 with port",
			host:       "127.0.0.1:" + port,
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid localhost with port",
			host:       "localhost:" + port,
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid [::1] with port",
			host:       "[::1]:" + port,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid external host",
			host:       "attacker.com:" + port,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "invalid port mismatch",
			host:       "127.0.0.1:9999",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "forbidden origin null",
			host:       "127.0.0.1:" + port,
			origin:     "null",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "forbidden origin localhost",
			host:       "127.0.0.1:" + port,
			origin:     "http://localhost:" + port,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "forbidden origin empty",
			host:       "127.0.0.1:" + port,
			origin:     "",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "x-forwarded-host ignored when host valid",
			host:       "127.0.0.1:" + port,
			fwdHost:    "attacker.com",
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL()+"/healthz", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Host = tc.host
			if tc.origin != "" || strings.Contains(tc.name, "origin empty") {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fwdHost != "" {
				req.Header.Set("X-Forwarded-Host", tc.fwdHost)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("got status %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestEndpoints_HealthzReadyzStatus(t *testing.T) {
	mgr := &fakeManager{
		ready:           true,
		restartRequired: false,
		reloadStatus:    "reloaded successfully",
		servers: []ServerStatusDTO{
			{
				ID:                   "srv-1",
				State:                "Ready",
				PublishedToolCount:   3,
				UnpublishedToolCount: 0,
				ActiveCalls:          1,
				DesiredRevision:      2,
				ActiveRevision:       2,
			},
		},
		calls: []RecentCallDTO{
			{
				RequestID:  "req-1",
				Time:       time.Now().Format(time.RFC3339),
				DurationMs: 42,
				Tool:       "toolA",
				ServerID:   "srv-1",
				Outcome:    "success",
			},
		},
	}

	srv, cleanup := setupTestServer(t, mgr, &HTTPServerOptions{Version: "1.2.3"})
	defer cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. /healthz GET
	t.Run("healthz GET", func(t *testing.T) {
		resp, err := client.Get(srv.URL() + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want 200", resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("got Cache-Control %q, want no-store", cc)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode healthz body: %v", err)
		}
		if body["status"] != "ok" {
			t.Errorf("got status %v, want ok", body["status"])
		}
	})

	// 2. /healthz non-GET -> 405
	t.Run("healthz POST rejected", func(t *testing.T) {
		resp, err := client.Post(srv.URL()+"/healthz", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("POST /healthz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("got status %d, want 405", resp.StatusCode)
		}
	})

	// 3. /readyz ready -> 200
	t.Run("readyz ready", func(t *testing.T) {
		resp, err := client.Get(srv.URL() + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want 200", resp.StatusCode)
		}
	})

	// 4. /readyz not ready -> 503
	t.Run("readyz not ready", func(t *testing.T) {
		mgr.mu.Lock()
		mgr.ready = false
		mgr.mu.Unlock()

		resp, err := client.Get(srv.URL() + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("got status %d, want 503", resp.StatusCode)
		}

		// restore
		mgr.mu.Lock()
		mgr.ready = true
		mgr.mu.Unlock()
	})

	// 5. /api/v1/status GET
	t.Run("status GET", func(t *testing.T) {
		resp, err := client.Get(srv.URL() + "/api/v1/status")
		if err != nil {
			t.Fatalf("GET /api/v1/status failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want 200", resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("got Cache-Control %q, want no-store", cc)
		}

		rawBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read status body: %v", err)
		}

		// Contract: must NOT leak secrets, env, url, command, headers, params, raw stack
		bodyStr := string(rawBody)
		for _, forbidden := range []string{"url", "headers", "command", "args", "env", "params", "result"} {
			if strings.Contains(strings.ToLower(bodyStr), `"`+forbidden+`":`) {
				t.Errorf("status response leaked forbidden field %q: %s", forbidden, bodyStr)
			}
		}

		var st StatusDTO
		if err := json.Unmarshal(rawBody, &st); err != nil {
			t.Fatalf("failed to unmarshal StatusDTO: %v", err)
		}

		if st.Version != "1.2.3" {
			t.Errorf("got version %q, want 1.2.3", st.Version)
		}
		if st.LastReloadStatus != "reloaded successfully" {
			t.Errorf("got lastReloadStatus %q, want 'reloaded successfully'", st.LastReloadStatus)
		}
		if len(st.Servers) != 1 || st.Servers[0].ID != "srv-1" {
			t.Errorf("unexpected servers: %+v", st.Servers)
		}
		if len(st.RecentCalls) != 1 || st.RecentCalls[0].Tool != "toolA" {
			t.Errorf("unexpected recent calls: %+v", st.RecentCalls)
		}
	})
}

func TestPublicModeRequiresHTTPSHostAndBearer(t *testing.T) {
	ln, err := BindLoopbackListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewHubServer("test-hub", "0.1.0")
	pub, _ := NewPublisher(server, nil, nil)
	srv, err := NewHTTPServer(ln, pub, nil, &HTTPServerOptions{PublicMode: true, PublicURL: "https://hub.example.com", AllowedHosts: []string{"hub.example.com"}, TrustedProxies: []string{"127.0.0.1/32"}, BearerToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()

	request, _ := http.NewRequest(http.MethodGet, srv.URL()+"/readyz", nil)
	request.Host = "hub.example.com"
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected bearer rejection, got %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodGet, srv.URL()+"/readyz", nil)
	request.Host = "hub.example.com"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected public authenticated request, got %d", response.StatusCode)
	}

	mcpReq, _ := http.NewRequest(http.MethodPost, srv.URL()+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`))
	mcpReq.Host = "hub.example.com"
	mcpReq.Header.Set("X-Forwarded-Proto", "https")
	mcpReq.Header.Set("Authorization", "Bearer secret")
	mcpReq.Header.Set("Content-Type", "application/json")
	mcpReq.Header.Set("Accept", "application/json, text/event-stream")
	mcpResp, err := http.DefaultClient.Do(mcpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer mcpResp.Body.Close()
	if mcpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(mcpResp.Body)
		t.Fatalf("expected 200 OK from public /mcp, got %d: %s", mcpResp.StatusCode, string(body))
	}
}

func TestMCP_InitializeAndListTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := BindLoopbackListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("BindLoopbackListener failed: %v", err)
	}

	hubServer := NewHubServer("test-hub", "0.1.0")
	pub, err := NewPublisher(hubServer, nil, nil)
	if err != nil {
		ln.Close()
		t.Fatalf("NewPublisher failed: %v", err)
	}

	// Publish a tool to the publisher
	rawTools := []*mcp.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather for city",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
			},
		},
	}
	_, err = pub.PublishServer("weather-server", rawTools, nil)
	if err != nil {
		ln.Close()
		t.Fatalf("PublishServer failed: %v", err)
	}

	httpSrv, err := NewHTTPServer(ln, pub, nil, nil)
	if err != nil {
		ln.Close()
		t.Fatalf("NewHTTPServer failed: %v", err)
	}

	go func() {
		_ = httpSrv.Serve()
	}()
	defer func() {
		_ = httpSrv.Shutdown(ctx)
		_ = httpSrv.Close()
	}()

	// Connect MCP client to /mcp
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: httpSrv.URL() + "/mcp"}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer session.Close()

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("session.ListTools failed: %v", err)
	}

	if len(toolsResult.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolsResult.Tools))
	}
	if toolsResult.Tools[0].Name != "weather-server__get_weather" {
		t.Errorf("expected tool name 'weather-server__get_weather', got %q", toolsResult.Tools[0].Name)
	}
}

func TestMCP_SessionCapacityAndAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const maxSessions = 2
	srv, cleanup := setupTestServer(t, nil, &HTTPServerOptions{MaxSessions: maxSessions})
	defer cleanup()

	var sessions []*mcp.ClientSession
	defer func() {
		for _, s := range sessions {
			_ = s.Close()
		}
	}()

	// Connect up to capacity
	for i := 0; i < maxSessions; i++ {
		client := mcp.NewClient(&mcp.Implementation{Name: fmt.Sprintf("client-%d", i), Version: "1.0.0"}, nil)
		tr := &mcp.StreamableClientTransport{Endpoint: srv.URL() + "/mcp"}
		cs, err := client.Connect(ctx, tr, nil)
		if err != nil {
			t.Fatalf("client %d connect failed: %v", i, err)
		}
		sessions = append(sessions, cs)
	}

	// Next connection must be rejected with capacity error (503)
	clientOverLimit := mcp.NewClient(&mcp.Implementation{Name: "over-limit", Version: "1.0.0"}, nil)
	trOverLimit := &mcp.StreamableClientTransport{Endpoint: srv.URL() + "/mcp"}
	_, err := clientOverLimit.Connect(ctx, trOverLimit, nil)
	if err == nil {
		t.Fatalf("expected connection over capacity limit to fail, but it succeeded")
	}

	// Close one active session
	_ = sessions[0].Close()
	sessions = sessions[1:]

	// Wait boundedly for session cleanup to settle
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		for range srv.publisher.Server().Sessions() {
			count++
		}
		if count < maxSessions {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now a new connection must succeed without slot leaking
	clientRetry := mcp.NewClient(&mcp.Implementation{Name: "client-retry", Version: "1.0.0"}, nil)
	trRetry := &mcp.StreamableClientTransport{Endpoint: srv.URL() + "/mcp"}
	retrySession, err := clientRetry.Connect(ctx, trRetry, nil)
	if err != nil {
		t.Fatalf("retry connect after session close failed: %v", err)
	}
	_ = retrySession.Close()
}

func TestMCP_FailedInitDoesNotLeakSlot(t *testing.T) {
	srv, cleanup := setupTestServer(t, nil, &HTTPServerOptions{MaxSessions: 1})
	defer cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	// Send an invalid JSON-RPC body to /mcp without session ID
	req, err := http.NewRequest(http.MethodPost, srv.URL()+"/mcp", bytes.NewBufferString("not valid json"))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp failed: %v", err)
	}
	_ = resp.Body.Close()

	// Active sessions should remain 0
	count := 0
	for range srv.publisher.Server().Sessions() {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 sessions after failed init, got %d", count)
	}
}

func TestHTTPServerURLUsesBoundIPv6Host(t *testing.T) {
	ln, err := BindLoopbackListener("[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer ln.Close()

	server := NewHubServer("test-hub", "0.1.0")
	pub, err := NewPublisher(server, nil, nil)
	if err != nil {
		t.Fatalf("NewPublisher failed: %v", err)
	}
	httpSrv, err := NewHTTPServer(ln, pub, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPServer failed: %v", err)
	}
	if !strings.HasPrefix(httpSrv.URL(), "http://[::1]:") {
		t.Fatalf("expected IPv6 loopback URL, got %q", httpSrv.URL())
	}
}

func TestMCP_MaxBodySize(t *testing.T) {
	const bodyCap = 1024 // test with small 1 KiB limit
	srv, cleanup := setupTestServer(t, nil, &HTTPServerOptions{MaxBodySize: bodyCap})
	defer cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	// Send oversized body
	oversized := make([]byte, bodyCap+512)
	req, err := http.NewRequest(http.MethodPost, srv.URL()+"/mcp", bytes.NewReader(oversized))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		// Connection dropped or reset is acceptable when MaxBytesReader closes body
		return
	}
	defer resp.Body.Close()

	// Should not be OK
	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected non-OK status for oversized body, got 200")
	}
}
