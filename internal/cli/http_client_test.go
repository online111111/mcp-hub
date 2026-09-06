package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBearerTransportInjectsTokenWithoutMutatingRequest(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://hub.example/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := bearerTransport{
		token: "secret-token",
		base: roundTripFunc(func(got *http.Request) (*http.Response, error) {
			if value := got.Header.Get("Authorization"); value != "Bearer secret-token" {
				t.Fatalf("unexpected Authorization header %q", value)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
				Request:    got,
			}, nil
		}),
	}
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if value := req.Header.Get("Authorization"); value != "" {
		t.Fatalf("original request was mutated: %q", value)
	}
}

func TestHubTokenPrefersExplicitValue(t *testing.T) {
	t.Setenv(hubTokenEnv, "from-env")
	if got := hubToken("from-flag"); got != "from-flag" {
		t.Fatalf("hubToken() = %q", got)
	}
	if got := hubToken(""); got != "from-env" {
		t.Fatalf("hubToken() = %q", got)
	}
}

func TestStatusCommandAuthenticatesToPublicHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/status" {
			http.NotFound(w, req)
			return
		}
		if got := req.Header.Get("Authorization"); got != "Bearer public-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"0.3.0","servers":[],"recentCalls":[]}`)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--endpoint", server.URL, "--token", "public-token", "--json"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("status exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"version": "0.3.0"`) {
		t.Fatalf("unexpected status output: %s", stdout.String())
	}
}
