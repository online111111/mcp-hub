package netpolicy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateMCPHTTPURL enforces the common transport security policy for MCP HTTP
// connections. HTTPS may be remote; plaintext HTTP is restricted to loopback.
// Userinfo and fragments are rejected. Query strings remain permitted because
// third-party MCP endpoints may legitimately use them.
func ValidateMCPHTTPURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("endpoint URL must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Hostname() == "" || u.Opaque != "" {
		return nil, fmt.Errorf("endpoint URL must contain a host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("endpoint URL userinfo is forbidden")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("endpoint URL fragment is forbidden")
	}

	switch strings.ToLower(u.Scheme) {
	case "https":
		return u, nil
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("plaintext HTTP endpoint is only permitted for loopback hosts; use HTTPS for remote connections")
		}
		return u, nil
	default:
		return nil, fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
	}
}

// ValidateHubEndpoint is intentionally stricter than the generic downstream
// policy. User-supplied Hub CLI/bridge endpoints may be echoed in diagnostics,
// so query strings are not accepted as part of the Hub endpoint contract.
func ValidateHubEndpoint(raw string) (*url.URL, error) {
	u, err := ValidateMCPHTTPURL(raw)
	if err != nil {
		return nil, err
	}
	if u.RawQuery != "" || u.ForceQuery {
		return nil, fmt.Errorf("endpoint URL query is forbidden")
	}
	return u, nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SafeURL returns an operator-facing URL with credentials/query/fragment
// stripped even if the caller is handling an invalid URL.
func SafeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "<invalid-endpoint>"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}
