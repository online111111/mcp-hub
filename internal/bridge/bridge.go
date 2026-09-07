package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/buildinfo"
	"mcp-hub/internal/downstream"
)

var (
	// ErrInvalidEndpoint indicates the provided Hub endpoint URL is empty or malformed.
	ErrInvalidEndpoint = errors.New("invalid bridge endpoint URL")

	// ErrHubSessionClosed indicates the upstream Hub session is closed or unavailable.
	ErrHubSessionClosed = errors.New("hub session is closed")

	// ErrBridgeClosed indicates the bridge itself is closed.
	ErrBridgeClosed = errors.New("bridge is closed")
)

const (
	// DefaultStartupTimeout is the fallback timeout for initial connection and tool listing.
	DefaultStartupTimeout = 10 * time.Second
)

// Options holds configuration for running an MCP stdio-to-Hub bridge.
type Options struct {
	// Endpoint is the full HTTP/HTTPS URL of the running Hub MCP endpoint (e.g. "http://127.0.0.1:8080/mcp").
	Endpoint string

	// Stdin is the input stream from the local MCP client (e.g. IDE/agent). Defaults to os.Stdin.
	Stdin io.Reader

	// Stdout is the output stream to the local MCP client. Defaults to os.Stdout.
	// Only valid MCP JSON-RPC protocol messages are written here.
	Stdout io.Writer

	// Stderr is the diagnostics stream. Defaults to os.Stderr.
	// All bridge logs, warnings, and errors are written here.
	Stderr io.Writer

	// BaseRoundTripper optionally overrides http.DefaultTransport for tests or proxies.
	BaseRoundTripper http.RoundTripper

	// Headers are attached to Hub requests, typically Authorization for public mode.
	Headers map[string]string

	// StartupTimeout specifies the timeout for initial Hub connection and tool discovery.
	StartupTimeout time.Duration

	// ClientInfo describes the bridge client connecting to Hub.
	ClientInfo *mcp.Implementation

	// ServerInfo describes the local SDK server exposing tools to the local stdio client.
	ServerInfo *mcp.Implementation
}

// Validate validates the Options.
func (o *Options) Validate() error {
	if o.Endpoint == "" {
		return fmt.Errorf("%w: endpoint URL must not be empty", ErrInvalidEndpoint)
	}

	u, err := url.Parse(o.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q (must be a valid http:// or https:// URL with host)", ErrInvalidEndpoint, o.Endpoint)
	}

	return nil
}

// Bridge connects a local stdio MCP client to a running remote/local Hub over Streamable HTTP.
// It exposes all Hub public tools dynamically to the local client, forwards CallTool requests
// with raw arguments, updates tools on Hub ToolListChanged notifications, and exits on Hub session failure.
type Bridge struct {
	opts Options

	hubSession *downstream.Session
	server     *mcp.Server

	toolsMu      sync.RWMutex
	currentTools map[string]*mcp.Tool

	syncMu sync.Mutex

	// Hub failure tracking
	hubErrMu      sync.Mutex
	hubFailureErr error

	closed atomic.Bool
	cancel context.CancelCauseFunc

	// The SDK's list_changed notification is suppressed while the initial
	// registry is populated. It is enabled before the local session starts.
	localToolCaps *mcp.ToolCapabilities

	// changeSeq/syncedSeq form a coalesced refresh watermark. Ordinary local
	// tools/list requests are served from the local SDK registry unless a newer
	// Hub notification is pending.
	changeSeq  atomic.Uint64
	syncedSeq  atomic.Uint64
	syncNeeded atomic.Bool

	logger *slog.Logger
}

// New creates a new Bridge instance with the given options.
func New(opts Options) (*Bridge, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StartupTimeout <= 0 {
		opts.StartupTimeout = DefaultStartupTimeout
	}

	if opts.ClientInfo == nil {
		opts.ClientInfo = &mcp.Implementation{
			Name:    "mcp-hub-bridge",
			Version: buildinfo.Version,
		}
	}

	if opts.ServerInfo == nil {
		opts.ServerInfo = &mcp.Implementation{
			Name:    "mcp-hub-bridge",
			Version: buildinfo.Version,
		}
	}

	logger := slog.New(slog.NewTextHandler(opts.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	b := &Bridge{
		opts:         opts,
		currentTools: make(map[string]*mcp.Tool),
		logger:       logger,
	}

	return b, nil
}

// Run connects to Hub at endpoint, discovers initial tools, and serves over stdin/stdout.
// It blocks until the client disconnects, ctx is cancelled, or the Hub session terminates.
// Stdout receives strictly MCP JSON-RPC messages; all logs and diagnostics go to stderr.
func Run(ctx context.Context, endpoint string, stdin io.Reader, stdout, stderr io.Writer) error {
	return RunWithOptions(ctx, Options{Endpoint: endpoint, Stdin: stdin, Stdout: stdout, Stderr: stderr})
}

// RunAuthenticated runs a bridge with a public-Hub bearer token.
func RunAuthenticated(ctx context.Context, endpoint, token string, stdin io.Reader, stdout, stderr io.Writer) error {
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return RunWithOptions(ctx, Options{Endpoint: endpoint, Headers: headers, Stdin: stdin, Stdout: stdout, Stderr: stderr})
}

// RunWithOptions runs the bridge using full Options.
func RunWithOptions(ctx context.Context, opts Options) error {
	b, err := New(opts)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	b.cancel = cancel

	// 1. Connect to Hub via Streamable HTTP ClientSession using downstream.DialHTTP.
	// Note: downstream.DialHTTP explicitly sets ClientOptions.Capabilities = &mcp.ClientCapabilities{},
	// ensuring client roots capabilities are disabled per MCP-Hub contract.
	connectCtx := runCtx
	if b.opts.StartupTimeout > 0 {
		var connCancel context.CancelFunc
		connectCtx, connCancel = context.WithTimeout(runCtx, b.opts.StartupTimeout)
		defer connCancel()
	}

	hubSession, err := downstream.DialHTTP(connectCtx, downstream.HTTPOptions{
		Endpoint:         b.opts.Endpoint,
		Headers:          b.opts.Headers,
		BaseRoundTripper: b.opts.BaseRoundTripper,
		// A bridge must terminate promptly when its Hub disappears. Disable
		// transport-level reconnect retries; the local stdio client can relaunch
		// the bridge, while a stale reconnect would keep the Hub test/server
		// process alive after shutdown.
		MaxRetries:     -1,
		StartupTimeout: b.opts.StartupTimeout,
		ClientInfo:     b.opts.ClientInfo,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to Hub at %s: %w", b.opts.Endpoint, err)
	}
	defer hubSession.Close()
	b.hubSession = hubSession

	// 2. Fetch all initial public tools from Hub before accepting local client requests.
	initialTools, err := hubSession.ListAllTools(connectCtx)
	if err != nil {
		return fmt.Errorf("failed to fetch initial tools from Hub: %w", err)
	}

	// 3. Create local SDK Server.
	// Explicitly configure DiscardHandler for SDK Server so it NEVER writes log messages to stdout.
	b.localToolCaps = &mcp.ToolCapabilities{}
	b.server = mcp.NewServer(b.opts.ServerInfo, &mcp.ServerOptions{
		Logger: slog.New(slog.DiscardHandler),
		Capabilities: &mcp.ServerCapabilities{
			// Suppress the initial AddTool notification while no local session
			// exists. The capability is enabled immediately before Run.
			Tools: b.localToolCaps,
		},
	})
	// A local client can issue tools/list immediately after receiving the
	// SDK's debounced list_changed notification. Serialize list requests with
	// the complete remove/add batch so it cannot observe an intermediate table.
	b.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(methodCtx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				// A notification normally completes the refresh before the
				// client's follow-up list request. If it is still pending, make
				// the list request the linearization point.
				if b.syncNeeded.Load() || b.changeSeq.Load() != b.syncedSeq.Load() {
					if err := b.syncTools(methodCtx); err != nil {
						return nil, err
					}
				}
				b.toolsMu.RLock()
				defer b.toolsMu.RUnlock()
			}
			return next(methodCtx, method, req)
		}
	})

	// 4. Register initial tools on local Server. There are no local sessions
	// before Server.Run starts, so this cannot expose a partially populated
	// registry. AddTool/RemoveTools provide the SDK-native list_changed behavior
	// for any session that is connected later; no timing-based startup delay is
	// needed here.
	b.updateRegisteredTools(initialTools)

	// Enable list_changed only after the initial registry has been populated and
	// immediately before accepting the local client. This avoids a stale startup
	// notification racing with the first post-initialize tools/list request while
	// retaining notifications for all subsequent directory changes.
	b.localToolCaps.ListChanged = true
	b.syncedSeq.Store(b.changeSeq.Load())

	// 5. Watch for Hub ToolListChanged notifications in background.
	go b.watchToolListChanges(runCtx)

	// 6. Monitor Hub session failure in background.
	// If Hub closes or fails unexpectedly, cancel runCtx to terminate local server promptly.
	go b.monitorHubSession(runCtx)

	// 7. Expose tools over Stdio / IO transport.
	transport := &mcp.IOTransport{
		Reader: toReadCloser(b.opts.Stdin),
		Writer: toWriteCloser(b.opts.Stdout),
	}

	// 8. Serve local client.
	serverErr := b.server.Run(runCtx, transport)
	b.closed.Store(true)
	// Stop the Hub client before returning. StreamableClientSession.Close
	// cancels its standalone SSE reader, so the test/program can close its
	// HTTP server without waiting on a persistent GET connection.
	cancel(nil)
	_ = hubSession.Close()

	// Determine exit condition:
	// Prioritize Hub session failure error if one occurred.
	b.hubErrMu.Lock()
	failure := b.hubFailureErr
	b.hubErrMu.Unlock()
	if failure != nil {
		return fmt.Errorf("hub session terminated: %w", failure)
	}

	// Check if parent ctx was cancelled.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Normal client disconnection (e.g. EOF on stdin or a closed pipe) settles
	// cleanly. The SDK may surface the underlying pipe close instead of io.EOF.
	if isNormalLocalDisconnect(serverErr) {
		return nil
	}

	return serverErr
}

// monitorHubSession waits for the Hub session connection to end.
// If the Hub disconnects unexpectedly while bridge is running, it records the failure and cancels runCtx.
func (b *Bridge) monitorHubSession(ctx context.Context) {
	if b.hubSession == nil {
		return
	}

	waitErr := b.hubSession.RawSession().Wait()

	b.hubErrMu.Lock()
	defer b.hubErrMu.Unlock()

	if !b.closed.Load() && ctx.Err() == nil {
		b.hubFailureErr = waitErr
		if b.hubFailureErr == nil {
			b.hubFailureErr = ErrHubSessionClosed
		}
		if b.cancel != nil {
			b.cancel(b.hubFailureErr)
		}
	}
}

// watchToolListChanges processes Hub tools/list_changed notifications without blocking SDK callbacks.
func (b *Bridge) watchToolListChanges(ctx context.Context) {
	changeCh := b.hubSession.ToolListChangedChan()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changeCh:
			if !ok {
				return
			}
			b.changeSeq.Add(1)
			b.syncNeeded.Store(true)
			// Drain the current notification burst, then wait a bounded
			// interval for the Hub's publisher debounce to finish.
			for {
				select {
				case _, ok := <-changeCh:
					if !ok {
						return
					}
					b.changeSeq.Add(1)
					b.syncNeeded.Store(true)
				default:
					goto burstDrained
				}
			}
		burstDrained:
			select {
			case <-time.After(25 * time.Millisecond):
			case <-ctx.Done():
				return
			}
			if err := b.syncTools(ctx); err != nil {
				if ctx.Err() == nil && !b.closed.Load() {
					b.logger.Warn("failed to synchronize tool list from Hub", "error", err)
				}
			}
		}
	}
}

// syncTools re-lists all tools from Hub and dynamically updates local Server AddTool/RemoveTools.
func (b *Bridge) syncTools(ctx context.Context) error {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	tools, err := b.hubSession.ListAllTools(ctx)
	if err != nil {
		return err
	}

	b.updateRegisteredTools(tools)
	b.syncedSeq.Store(b.changeSeq.Load())
	b.syncNeeded.Store(false)
	return nil
}

// updateRegisteredTools computes tool additions and removals, updating the local SDK server.
// AddTool and RemoveTools in SDK v1.4.1 automatically notify connected sessions with notifications/tools/list_changed.
func (b *Bridge) updateRegisteredTools(tools []*mcp.Tool) {
	b.toolsMu.Lock()
	defer b.toolsMu.Unlock()

	newTools := make(map[string]*mcp.Tool, len(tools))
	for _, t := range tools {
		if t != nil && t.Name != "" {
			// Ensure InputSchema is a valid object map to avoid SDK panics
			if t.InputSchema == nil {
				t.InputSchema = map[string]any{"type": "object"}
			}
			newTools[t.Name] = t
		}
	}

	// Calculate tools to remove
	var toRemove []string
	for oldName := range b.currentTools {
		if _, exists := newTools[oldName]; !exists {
			toRemove = append(toRemove, oldName)
		}
	}

	if len(toRemove) > 0 {
		b.server.RemoveTools(toRemove...)
		for _, name := range toRemove {
			delete(b.currentTools, name)
		}
	}

	// Add or update tools
	for name, tool := range newTools {
		old, exists := b.currentTools[name]
		if exists {
			oldJSON, oldErr := json.Marshal(old)
			newJSON, newErr := json.Marshal(tool)
			if oldErr == nil && newErr == nil && string(oldJSON) == string(newJSON) {
				continue
			}
		}
		b.server.AddTool(tool, b.dispatchTool)
		b.currentTools[name] = tool
	}
}

// dispatchTool forwards a CallTool request to the Hub using the public tool name and raw arguments.
func (b *Bridge) dispatchTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return nil, errors.New("invalid call tool request: missing parameters")
	}

	if b.closed.Load() {
		return nil, ErrBridgeClosed
	}

	b.hubErrMu.Lock()
	failure := b.hubFailureErr
	b.hubErrMu.Unlock()
	if failure != nil {
		return nil, ErrHubSessionClosed
	}

	if b.hubSession == nil || b.hubSession.IsClosed() {
		return nil, ErrHubSessionClosed
	}

	// Forward tool call with raw arguments to the Hub public tool name
	return b.hubSession.CallToolWithParams(ctx, &mcp.CallToolParams{
		Meta:      req.Params.Meta,
		Name:      req.Params.Name,
		Arguments: req.Params.Arguments,
	})
}

// nopWriteCloser wraps an io.Writer with a no-op Close method.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

func toReadCloser(r io.Reader) io.ReadCloser {
	if r == nil {
		return io.NopCloser(os.Stdin)
	}
	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}
	return io.NopCloser(r)
}

func toWriteCloser(w io.Writer) io.WriteCloser {
	if w == nil {
		return nopWriteCloser{os.Stdout}
	}
	// Never close standard file descriptors
	if f, ok := w.(*os.File); ok && (f == os.Stdout || f == os.Stderr) {
		return nopWriteCloser{w}
	}
	if wc, ok := w.(io.WriteCloser); ok {
		return wc
	}
	return nopWriteCloser{w}
}
