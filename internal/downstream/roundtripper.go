package downstream

import (
	"fmt"
	"net/http"
	"strings"
)

var forbiddenHeaders = map[string]struct{}{
	"host":                 {},
	"content-length":       {},
	"connection":           {},
	"transfer-encoding":    {},
	"upgrade":              {},
	"trailer":              {},
	"te":                   {},
	"keep-alive":           {},
	"proxy-authorization":  {},
	"proxy-connection":     {},
	"mcp-session-id":       {},
	"mcp-protocol-version": {},
}

// ValidateHeaders ensures that user-provided headers do not contain forbidden transport-level headers.
func ValidateHeaders(headers map[string]string) error {
	for k := range headers {
		lower := strings.ToLower(strings.TrimSpace(k))
		if _, forbidden := forbiddenHeaders[lower]; forbidden {
			return fmt.Errorf("%w: header %q is forbidden in downstream HTTP configuration", ErrForbiddenHeader, k)
		}
	}
	return nil
}

// HeaderInjectingRoundTripper is an http.RoundTripper that clones the outgoing request
// and injects configured headers without modifying the original request.
type HeaderInjectingRoundTripper struct {
	Base    http.RoundTripper
	Headers map[string]string
}

// RoundTrip executes a single HTTP transaction, injecting configured headers into a clone of the request.
func (rt *HeaderInjectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	if req2.Header == nil {
		req2.Header = make(http.Header)
	}
	for k, v := range rt.Headers {
		req2.Header.Set(k, v)
	}
	base := rt.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

// newHTTPClient creates an *http.Client configured for downstream Streamable HTTP:
// - Injects custom headers via HeaderInjectingRoundTripper
// - Disables following HTTP redirects (avoiding credential leaks across origins)
// - Leaves Client.Timeout at 0 so persistent SSE streams are not terminated prematurely
func newHTTPClient(headers map[string]string, baseRT http.RoundTripper) (*http.Client, error) {
	if err := ValidateHeaders(headers); err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &HeaderInjectingRoundTripper{
			Base:    baseRT,
			Headers: headers,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return ErrRedirectNotAllowed
		},
		Timeout: 0,
	}, nil
}
