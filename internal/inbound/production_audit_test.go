package inbound

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLocalConfiguredBearerProtectsMCPAndDiagnostics(t *testing.T) {
	token := strings.Repeat("a", 32)
	for _, path := range []string{"/mcp", "/readyz", "/api/v1/status"} {
		for _, supplied := range []string{"", "wrong", token} {
			s := &HTTPServer{boundPort: "8080", bearerToken: token}
			req := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
			if supplied != "" {
				req.Header.Set("Authorization", "Bearer "+supplied)
			}
			w := httptest.NewRecorder()
			s.wrapSecurityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, req)
			want := 401
			if supplied == token {
				want = 200
			}
			if w.Code != want {
				t.Errorf("path=%s valid=%t: got %d want %d", path, supplied == token, w.Code, want)
			}
		}
	}
}

func TestEmptyAuthoritativeTokenSetNeverRestoresStartupToken(t *testing.T) {
	s := &HTTPServer{publicMode: true, bearerToken: strings.Repeat("a", 32), bearerTokensProvider: func() []string { return nil }}
	req := httptest.NewRequest("GET", "https://mcp.example/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+s.bearerToken)
	if s.authorizedBearer(req) {
		t.Fatal("removed startup credential was restored")
	}
}

func TestUnconfiguredLocalAndHealthRemainAccessible(t *testing.T) {
	for _, tc := range []struct{ path, token string }{{"/mcp", ""}, {"/healthz", strings.Repeat("a", 32)}} {
		s := &HTTPServer{boundPort: "8080", bearerToken: tc.token}
		w := httptest.NewRecorder()
		s.wrapSecurityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8080"+tc.path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: got %d", tc.path, w.Code)
		}
	}
}

func TestPublisherCallbackConcurrentReplacement(t *testing.T) {
	p, err := NewPublisher(NewHubServer("audit", "test"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	callback := func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			p.SetRouterCallback(callback)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_, _ = p.dispatchTool(context.Background(), nil)
		}
	}()
	wg.Wait()
}
