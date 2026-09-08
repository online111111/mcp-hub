package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	serverIDRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

	forbiddenHeaders = map[string]struct{}{
		"host":                 {},
		"content-length":       {},
		"connection":           {},
		"mcp-session-id":       {},
		"mcp-protocol-version": {},
		"transfer-encoding":    {},
		"upgrade":              {},
		"trailer":              {},
		"te":                   {},
		"keep-alive":           {},
		"proxy-authorization":  {},
		"proxy-connection":     {},
	}
)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Version != CurrentVersion {
		return fmt.Errorf("invalid version %d: version must be %d", cfg.Version, CurrentVersion)
	}

	listen := cfg.Hub.Listen
	if listen == "" {
		listen = DefaultListen
	}
	if err := validateListenAddress(listen, cfg.Hub.PublicMode); err != nil {
		return fmt.Errorf("invalid hub.listen: %w", err)
	}
	if err := validateRawBearerTokens(cfg.Hub.Auth); err != nil {
		return err
	}
	if cfg.Hub.PublicMode {
		if strings.TrimSpace(cfg.Hub.PublicURL) == "" {
			return fmt.Errorf("hub.publicUrl is required when publicMode is enabled")
		}
		u, err := url.Parse(cfg.Hub.PublicURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.Hub.PublicURL, "#") {
			return fmt.Errorf("hub.publicUrl must be an origin-only https URL")
		}
		if len(cfg.Hub.AllowedHosts) == 0 {
			return fmt.Errorf("hub.allowedHosts is required when publicMode is enabled")
		}
		if !hasRawBearerToken(cfg.Hub.Auth) {
			return fmt.Errorf("hub.auth requires bearerToken or bearerTokens when publicMode is enabled")
		}
	}
	if cfg.Hub.Admin.Enabled {
		if strings.TrimSpace(cfg.Hub.Admin.Token) == "" {
			return fmt.Errorf("hub.admin.token is required when admin is enabled")
		}
		if cfg.Hub.Admin.SessionTimeout != "" {
			d, err := time.ParseDuration(cfg.Hub.Admin.SessionTimeout)
			if err != nil || d < time.Minute || d > 24*time.Hour {
				return fmt.Errorf("hub.admin.sessionTimeout must be between 1m and 24h")
			}
		}
	}
	for _, host := range cfg.Hub.AllowedHosts {
		if strings.TrimSpace(host) == "" || strings.ContainsAny(host, "/\\") {
			return fmt.Errorf("invalid hub.allowedHosts entry %q", host)
		}
	}
	for _, cidr := range cfg.Hub.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid hub.trustedProxies entry %q", cidr)
		}
	}

	if cfg.Defaults.StartupTimeout != "" {
		d, err := time.ParseDuration(cfg.Defaults.StartupTimeout)
		if err != nil || d <= 0 || d > MaxDuration {
			return fmt.Errorf("invalid defaults.startupTimeout %q: must be positive duration <= 24h", cfg.Defaults.StartupTimeout)
		}
	}
	if cfg.Defaults.CallTimeout != "" {
		d, err := time.ParseDuration(cfg.Defaults.CallTimeout)
		if err != nil || d <= 0 || d > MaxDuration {
			return fmt.Errorf("invalid defaults.callTimeout %q: must be positive duration <= 24h", cfg.Defaults.CallTimeout)
		}
	}
	if cfg.Defaults.MaxConcurrency != 0 {
		if cfg.Defaults.MaxConcurrency < MinConcurrency || cfg.Defaults.MaxConcurrency > MaxConcurrency {
			return fmt.Errorf("invalid defaults.maxConcurrency %d: must be between %d and %d", cfg.Defaults.MaxConcurrency, MinConcurrency, MaxConcurrency)
		}
	}

	if len(cfg.MCPServers) > MaxServers {
		return fmt.Errorf("mcpServers count %d exceeds maximum limit of %d", len(cfg.MCPServers), MaxServers)
	}
	for id, s := range cfg.MCPServers {
		if err := validateServer(id, s); err != nil {
			return fmt.Errorf("server %q: %w", id, err)
		}
	}
	return nil
}

func hasRawBearerToken(auth HubAuthConfig) bool {
	if strings.TrimSpace(auth.BearerToken) != "" {
		return true
	}
	for _, token := range auth.BearerTokens {
		if strings.TrimSpace(token) != "" {
			return true
		}
	}
	return false
}

func validateRawBearerTokens(auth HubAuthConfig) error {
	seen := make(map[string]struct{}, 1+len(auth.BearerTokens))
	if strings.TrimSpace(auth.BearerToken) != "" {
		seen[auth.BearerToken] = struct{}{}
	}
	for i, token := range auth.BearerTokens {
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("hub.auth.bearerTokens[%d] must not be empty", i)
		}
		if _, duplicate := seen[token]; duplicate {
			return fmt.Errorf("hub.auth bearer tokens must be unique")
		}
		seen[token] = struct{}{}
	}
	return nil
}

func validateListenAddress(listen string, publicMode bool) error {
	host, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("must be formatted as host:port (%w)", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be an integer between 1 and 65535, got %q", portStr)
	}
	if !isLoopbackHost(host) && !publicMode {
		return fmt.Errorf("listen host %q is not loopback; enable hub.publicMode with authentication for public binding", host)
	}
	if publicMode && strings.TrimSpace(host) == "" {
		return fmt.Errorf("public listen host must be explicit (for example 0.0.0.0 or ::)")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func rejectCaseFoldedDuplicates(values map[string]string, field string) error {
	seen := make(map[string]string, len(values))
	for key := range values {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			return fmt.Errorf("%s cannot contain an empty key", field)
		}
		if previous, ok := seen[normalized]; ok {
			return fmt.Errorf("%s contains case-insensitive duplicate keys %q and %q", field, previous, key)
		}
		seen[normalized] = key
	}
	return nil
}

func validateServer(id string, s ServerConfig) error {
	if !serverIDRegex.MatchString(id) {
		return fmt.Errorf("invalid server ID: must match %s", serverIDRegex.String())
	}

	switch s.Type {
	case ServerTypeStdio:
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("command is required for stdio server")
		}
		if s.URL != "" {
			return fmt.Errorf("url is forbidden for stdio server")
		}
		if len(s.Headers) > 0 {
			return fmt.Errorf("headers are forbidden for stdio server")
		}
		// Environment keys are case-insensitive on Windows. Rejecting case-folded
		// duplicates on every platform keeps configs portable and avoids a random
		// winner when the same config is later run on Windows.
		if err := rejectCaseFoldedDuplicates(s.Env, "env"); err != nil {
			return err
		}

	case ServerTypeStreamableHTTP:
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("url is required for streamable_http server")
		}
		if s.Command != "" {
			return fmt.Errorf("command is forbidden for streamable_http server")
		}
		if len(s.Args) > 0 {
			return fmt.Errorf("args are forbidden for streamable_http server")
		}
		if s.Cwd != "" {
			return fmt.Errorf("cwd is forbidden for streamable_http server")
		}
		if len(s.Env) > 0 {
			return fmt.Errorf("env is forbidden for streamable_http server")
		}
		if err := validateHTTPURL(s.URL); err != nil {
			return err
		}
		if err := rejectCaseFoldedDuplicates(s.Headers, "headers"); err != nil {
			return err
		}
		for h := range s.Headers {
			normalized := strings.ToLower(strings.TrimSpace(h))
			if _, forbidden := forbiddenHeaders[normalized]; forbidden {
				return fmt.Errorf("forbidden transport header %q", h)
			}
		}

	case "sse":
		return fmt.Errorf("sse transport is not supported in P0")
	case "":
		return fmt.Errorf("type is required (must be 'stdio' or 'streamable_http')")
	default:
		return fmt.Errorf("unsupported type %q (must be 'stdio' or 'streamable_http')", s.Type)
	}

	if s.StartupTimeout != "" {
		d, err := time.ParseDuration(s.StartupTimeout)
		if err != nil || d <= 0 || d > MaxDuration {
			return fmt.Errorf("invalid startupTimeout %q: must be positive duration <= 24h", s.StartupTimeout)
		}
	}
	if s.CallTimeout != "" {
		d, err := time.ParseDuration(s.CallTimeout)
		if err != nil || d <= 0 || d > MaxDuration {
			return fmt.Errorf("invalid callTimeout %q: must be positive duration <= 24h", s.CallTimeout)
		}
	}
	if s.MaxConcurrency != 0 {
		if s.MaxConcurrency < MinConcurrency || s.MaxConcurrency > MaxConcurrency {
			return fmt.Errorf("invalid maxConcurrency %d: must be between %d and %d", s.MaxConcurrency, MinConcurrency, MaxConcurrency)
		}
	}

	seen := make(map[string]struct{})
	for _, toolName := range s.Tools.Disabled {
		if strings.TrimSpace(toolName) == "" {
			return fmt.Errorf("tools.disabled cannot contain empty tool names")
		}
		if _, exists := seen[toolName]; exists {
			return fmt.Errorf("duplicate tool name %q in tools.disabled", toolName)
		}
		seen[toolName] = struct{}{}
	}
	return nil
}

func validateHTTPURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Hostname() == "" || u.Opaque != "" {
		return fmt.Errorf("url must contain a host")
	}
	if u.User != nil {
		return fmt.Errorf("url userinfo is forbidden")
	}
	if u.Fragment != "" {
		return fmt.Errorf("url fragment is forbidden")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("unsupported url scheme %q: only https or loopback http are permitted", scheme)
	}
	if scheme == "http" {
		hostname := u.Hostname()
		if !isLoopbackHost(hostname) {
			return fmt.Errorf("plain http url is only permitted for loopback addresses (%q is not loopback); use https for remote services", hostname)
		}
	}
	return nil
}
