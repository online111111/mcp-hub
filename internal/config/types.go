package config

import (
	"time"
)

const (
	CurrentVersion        = 1
	MaxConfigFileSize     = 1 * 1024 * 1024 // 1 MiB
	MaxServers            = 32
	MaxServerIDLength     = 32
	DefaultListen         = "127.0.0.1:8080"
	DefaultStartupTimeout = 20 * time.Second
	DefaultCallTimeout    = 60 * time.Second
	DefaultMaxConcurrency = 8
	MaxDuration           = 24 * time.Hour
	MaxRecentCalls        = 200
	MinConcurrency        = 1
	MaxConcurrency        = 64
	MinAuthTokenLength    = 32
)

// ServerType represents the transport protocol for a downstream MCP server.
type ServerType string

const (
	ServerTypeStdio          ServerType = "stdio"
	ServerTypeStreamableHTTP ServerType = "streamable_http"
)

// Config represents the raw configuration as persisted in config.json.
// Raw environment variable references (${NAME}) and secrets are preserved as-is.
type Config struct {
	Version    int                     `json:"version"`
	Hub        HubConfig               `json:"hub,omitempty"`
	Defaults   DefaultsConfig          `json:"defaults,omitempty"`
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// HubConfig holds configuration for the Hub's own inbound endpoints.
type HubConfig struct {
	Listen         string        `json:"listen,omitempty"`
	PublicMode     bool          `json:"publicMode,omitempty"`
	PublicURL      string        `json:"publicUrl,omitempty"`
	AllowedHosts   []string      `json:"allowedHosts,omitempty"`
	TrustedProxies []string      `json:"trustedProxies,omitempty"`
	Auth           HubAuthConfig `json:"auth,omitempty"`
	Admin          AdminConfig   `json:"admin,omitempty"`
}

// HubAuthConfig protects the public MCP and diagnostic endpoints. BearerToken is
// the original single-token field and remains supported for backwards
// compatibility. BearerTokens adds optional additional tokens. Every entry uses
// the same one-pass ${NAME} expansion as downstream secrets.
type HubAuthConfig struct {
	BearerToken  string   `json:"bearerToken,omitempty"`
	BearerTokens []string `json:"bearerTokens,omitempty"`
}

// AdminConfig enables the embedded management UI. Token is the bootstrap admin
// credential and should normally be an environment reference.
type AdminConfig struct {
	Enabled        bool   `json:"enabled,omitempty"`
	Token          string `json:"token,omitempty"`
	SessionTimeout string `json:"sessionTimeout,omitempty"`
}

// DefaultsConfig holds default timeout and concurrency values across downstream servers.
type DefaultsConfig struct {
	StartupTimeout string `json:"startupTimeout,omitempty"`
	CallTimeout    string `json:"callTimeout,omitempty"`
	MaxConcurrency int    `json:"maxConcurrency,omitempty"`
}

// ToolsConfig holds tool filtering configuration.
type ToolsConfig struct {
	Disabled []string `json:"disabled,omitempty"`
}

// ServerConfig represents the raw configuration of one downstream MCP server.
type ServerConfig struct {
	Enabled *bool      `json:"enabled,omitempty"`
	Type    ServerType `json:"type"`

	// stdio fields
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// streamable_http fields
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Tool filtering
	Tools ToolsConfig `json:"tools,omitempty"`

	// Server-level overrides
	StartupTimeout string `json:"startupTimeout,omitempty"`
	CallTimeout    string `json:"callTimeout,omitempty"`
	MaxConcurrency int    `json:"maxConcurrency,omitempty"`
}

// IsEnabled returns true if Enabled is not explicitly set to false.
func (s *ServerConfig) IsEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

// ResolvedConfig represents the evaluated runtime configuration.
// It is intentionally decoupled from Config so evaluated secrets/headers cannot be serialized back to disk.
type ResolvedConfig struct {
	Version             int
	Listen              string
	PublicMode          bool
	PublicURL           string
	AllowedHosts        []string
	TrustedProxies      []string
	// BearerToken remains the first effective MCP token for compatibility with
	// older internal callers. BearerTokens is the authoritative complete set.
	BearerToken         string
	BearerTokens        []string
	AdminEnabled        bool
	AdminToken          string
	AdminSessionTimeout time.Duration
	DefaultStartup      time.Duration
	DefaultCall         time.Duration
	DefaultConcurrency  int
	Servers             map[string]ResolvedServer
}

// ResolvedServer represents the evaluated runtime configuration.
type ResolvedServer struct {
	ID      string
	Enabled bool
	Type    ServerType

	// stdio fields (command and cwd resolved against config dir)
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string // Process env merged with evaluated server env

	// streamable_http fields
	URL     string
	Headers map[string]string // Evaluated headers

	// Tool filtering
	DisabledTools map[string]struct{}

	// Evaluated timeouts & concurrency
	StartupTimeout time.Duration
	CallTimeout    time.Duration
	MaxConcurrency int
}
