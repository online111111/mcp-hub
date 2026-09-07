package cli

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const hubTokenEnv = "MCP_HUB_TOKEN"

// bearerTransport injects Hub authentication without mutating the caller's
// request. It is shared by the status and doctor commands so public-Hub
// diagnostics use the same credentials as the stdio bridge.
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if strings.TrimSpace(t.token) == "" {
		return base.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return base.RoundTrip(clone)
}

func hubToken(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return os.Getenv(hubTokenEnv)
}

func newHubHTTPClient(token string, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: bearerTransport{token: token},
		Timeout:   timeout,
		// bearerTransport adds credentials on every request, including redirects.
		// Never follow a response-selected URL, even on the same origin. Return
		// the original response so diagnostics report its non-OK status and the
		// caller remains responsible for closing its body.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
