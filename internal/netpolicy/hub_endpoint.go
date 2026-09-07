package netpolicy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateHubEndpoint enforces the transport policy for clients connecting to
// this Hub: HTTPS may be remote; plaintext HTTP is restricted to loopback.
// URL credentials, query strings and fragments are rejected because they can
// leak through diagnostics and are not part of the Hub endpoint contract.
func ValidateHubEndpoint(raw string) (*url.URL, error) {
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
	if u.RawQuery != "" || u.ForceQuery {
		return nil, fmt.Errorf("endpoint URL query is forbidden")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("endpoint URL fragment is forbidden")
	}

	switch strings.ToLower(u.Scheme) {
	case "https":
		return u, nil
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("plaintext HTTP endpoint is only permitted for loopback hosts; use HTTPS for remote Hub connections")
		}
		return u, nil
	default:
		return nil, fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
	}
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
