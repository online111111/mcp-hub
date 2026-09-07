package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBearerTransportRejectsRemotePlainHTTPBeforeSending(t *testing.T) {
	called := false
	tr := bearerTransport{
		token: "secret",
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api/v1/status", nil)
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("expected remote plaintext HTTP to be rejected")
	}
	if called {
		t.Fatal("base transport was called for unsafe endpoint")
	}
}

func TestBearerTransportCapsResponseBody(t *testing.T) {
	tr := bearerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(strings.Repeat("x", int(maxHubResponseSize)+1024))),
			Header: make(http.Header),
		}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/status", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("expected oversized response read to fail")
	}
}
