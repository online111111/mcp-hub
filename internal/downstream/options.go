package downstream

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	ErrUnsupportedTransport = errors.New("unsupported downstream transport type")
	ErrInvalidOptions       = errors.New("invalid downstream options")
	ErrForbiddenHeader      = errors.New("forbidden header in downstream configuration")
	ErrRedirectNotAllowed   = errors.New("downstream HTTP redirects are not allowed")
	ErrDuplicateCursor      = errors.New("duplicate cursor detected during pagination")
	ErrDuplicateToolName    = errors.New("duplicate tool name detected during tool discovery")
	ErrSessionClosed        = errors.New("downstream session is closed")
)

// TransportType represents the type of transport used to connect to the downstream MCP server.
type TransportType string

const (
	TransportTypeStreamableHTTP TransportType = "streamable_http"
	TransportTypeIO             TransportType = "io"
)

// HTTPOptions holds typed configuration for establishing a Streamable HTTP downstream connection.
type HTTPOptions struct {
	// Endpoint is the full HTTP/HTTPS URL of the MCP server endpoint.
	Endpoint string

	// Headers holds custom headers to inject on every outgoing HTTP request.
	Headers map[string]string

	// BaseRoundTripper optionally overrides http.DefaultTransport.
	BaseRoundTripper http.RoundTripper

	// DisableStandaloneSSE controls whether standalone SSE GET stream is disabled.
	DisableStandaloneSSE bool

	// MaxRetries controls reconnect attempts for the Streamable HTTP transport.
	// Zero keeps the SDK default; a negative value disables reconnect attempts.
	MaxRetries int

	// StartupTimeout specifies the timeout for connection and initialization.
	StartupTimeout time.Duration

	// ClientInfo describes this client implementation during initialization.
	ClientInfo *mcp.Implementation

	// OnToolListChanged is an optional callback triggered when a tools/list_changed notification arrives.
	OnToolListChanged func()
}

// Validate checks whether the HTTPOptions are valid.
func (o *HTTPOptions) Validate() error {
	if o.Endpoint == "" {
		return fmt.Errorf("%w: endpoint URL must not be empty", ErrInvalidOptions)
	}
	if err := ValidateHeaders(o.Headers); err != nil {
		return err
	}
	return nil
}

// IOOptions holds typed configuration for establishing an IO-based (stdio/pipe) downstream connection.
type IOOptions struct {
	// Reader is the input stream from the downstream process or pipe.
	Reader io.ReadCloser

	// Writer is the output stream to the downstream process or pipe.
	Writer io.WriteCloser

	// StartupTimeout specifies the timeout for connection and initialization.
	StartupTimeout time.Duration

	// ClientInfo describes this client implementation during initialization.
	ClientInfo *mcp.Implementation

	// OnToolListChanged is an optional callback triggered when a tools/list_changed notification arrives.
	OnToolListChanged func()
}

// Validate checks whether the IOOptions are valid.
func (o *IOOptions) Validate() error {
	if o.Reader == nil {
		return fmt.Errorf("%w: reader must not be nil", ErrInvalidOptions)
	}
	if o.Writer == nil {
		return fmt.Errorf("%w: writer must not be nil", ErrInvalidOptions)
	}
	return nil
}

// Options is a unified options struct supporting both HTTP and IO transports.
type Options struct {
	Type TransportType

	// Streamable HTTP options
	Endpoint             string
	Headers              map[string]string
	BaseRoundTripper     http.RoundTripper
	DisableStandaloneSSE bool
	MaxRetries           int

	// IO options
	Reader io.ReadCloser
	Writer io.WriteCloser

	// Common options
	StartupTimeout    time.Duration
	ClientInfo        *mcp.Implementation
	OnToolListChanged func()
}

// Validate checks whether the Options are valid for the specified transport type.
func (o *Options) Validate() error {
	switch o.Type {
	case TransportTypeStreamableHTTP:
		httpOpts := HTTPOptions{
			Endpoint:             o.Endpoint,
			Headers:              o.Headers,
			BaseRoundTripper:     o.BaseRoundTripper,
			DisableStandaloneSSE: o.DisableStandaloneSSE,
			MaxRetries:           o.MaxRetries,
		}
		return httpOpts.Validate()
	case TransportTypeIO:
		ioOpts := IOOptions{
			Reader: o.Reader,
			Writer: o.Writer,
		}
		return ioOpts.Validate()
	case "":
		return fmt.Errorf("%w: transport type must not be empty", ErrUnsupportedTransport)
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedTransport, o.Type)
	}
}
