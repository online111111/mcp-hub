package cli

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"mcp-hub/internal/netpolicy"
)

const (
	hubTokenEnv        = "MCP_HUB_TOKEN"
	maxHubResponseSize = int64(8 * 1024 * 1024)
)

var errHubResponseTooLarge = errors.New("Hub response exceeds 8 MiB limit")

type boundedReadCloser struct {
	r io.Reader
	c io.Closer
	n int64
}

func (b *boundedReadCloser) Read(p []byte) (int, error) {
	if b.n <= 0 {
		return 0, errHubResponseTooLarge
	}
	if int64(len(p)) > b.n {
		p = p[:b.n]
	}
	n, err := b.r.Read(p)
	b.n -= int64(n)
	if err == io.EOF {
		return n, err
	}
	if b.n == 0 && err == nil {
		return n, errHubResponseTooLarge
	}
	return n, err
}

func (b *boundedReadCloser) Close() error { return b.c.Close() }

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := netpolicy.ValidateHubEndpoint(req.URL.String()); err != nil {
		return nil, err
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	request := req
	if strings.TrimSpace(t.token) != "" {
		request = req.Clone(req.Context())
		request.Header = req.Header.Clone()
		request.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		resp.Body = &boundedReadCloser{r: resp.Body, c: resp.Body, n: maxHubResponseSize + 1}
	}
	return resp, nil
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
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
