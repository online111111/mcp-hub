package testserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is a controllable fake MCP server for production and integration tests.
// It exposes controllable fixtures (echo_raw, image, fail_tool, block/cancel,
// counter, paged tools, crash) and transport helpers for in-memory, HTTP, and IO.
//
// Limitations and Design Notes:
//  1. In-Memory Preference:
//     Production tests prefer in-memory mcp.Server + mcp.NewInMemoryTransports() helpers
//     over external processes for fast, hermetic, race-free testing without zombie processes.
//  2. Crash Simulation:
//     In-process Go tests cannot call os.Exit() without terminating the test runner itself.
//     Safe crash simulation abruptly closes active server sessions and transports, triggering
//     client-side EOF / connection closed errors during or before tool execution.
//  3. Subprocess Management (spawn_child):
//     Spawning persistent grandchild processes and Windows Job Object lifecycle management
//     (Agent实施手册 T04) require platform-specific process management, which belongs to
//     internal/process. The internal/testserver package focuses on controllable MCP protocol fixtures.
//  4. Exact SDK v1.4.1 APIs:
//     The package strictly uses official SDK v1.4.1 types (mcp.NewServer, mcp.NewInMemoryTransports,
//     mcp.ToolHandler, mcp.CallToolRequest, mcp.CallToolResult, mcp.ListToolsResult, etc.).
type Server struct {
	mcpServer *mcp.Server

	EchoRaw  *EchoRawController
	Image    *ImageController
	FailTool *FailToolController
	Block    *BlockController
	Counter  *CounterController
	Paged    *PagedController
	Crash    *CrashController

	sessionsMu sync.Mutex
	sessions   []*mcp.ServerSession

	httpServerMu sync.Mutex
	httpServer   *httptest.Server
}

// Option configures Server creation.
type Option func(*serverConfig)

type serverConfig struct {
	name             string
	version          string
	pageSize         int
	registerDefaults bool
	customTools      []customToolRegistration
}

type customToolRegistration struct {
	tool    *mcp.Tool
	handler mcp.ToolHandler
}

// WithName sets the server implementation name.
func WithName(name string) Option {
	return func(c *serverConfig) {
		c.name = name
	}
}

// WithVersion sets the server implementation version.
func WithVersion(version string) Option {
	return func(c *serverConfig) {
		c.version = version
	}
}

// WithPageSize sets the default pagination page size for list methods.
func WithPageSize(size int) Option {
	return func(c *serverConfig) {
		c.pageSize = size
	}
}

// WithoutDefaultFixtures disables automatic registration of standard T03 fixtures.
func WithoutDefaultFixtures() Option {
	return func(c *serverConfig) {
		c.registerDefaults = false
	}
}

// WithCustomTool registers an additional tool on the test server.
func WithCustomTool(tool *mcp.Tool, handler mcp.ToolHandler) Option {
	return func(c *serverConfig) {
		c.customTools = append(c.customTools, customToolRegistration{
			tool:    tool,
			handler: handler,
		})
	}
}

// New creates and initializes a controllable Server.
func New(opts ...Option) *Server {
	cfg := &serverConfig{
		name:             "fake-mcp-testserver",
		version:          "1.0.0",
		pageSize:         2,
		registerDefaults: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	impl := &mcp.Implementation{
		Name:    cfg.name,
		Version: cfg.version,
	}
	serverOpts := &mcp.ServerOptions{
		PageSize: cfg.pageSize,
	}

	mcpSrv := mcp.NewServer(impl, serverOpts)

	s := &Server{
		mcpServer: mcpSrv,
		EchoRaw:   NewEchoRawController(),
		Image:     NewImageController(),
		FailTool:  NewFailToolController(),
		Block:     NewBlockController(),
		Counter:   NewCounterController(),
		Paged:     NewPagedController(cfg.pageSize),
		Crash:     NewCrashController(),
	}

	s.Paged.serverRef = s
	s.Crash.serverRef = s

	// Intercept requests for crash rejection, controllable multi-page pagination, and repeat-cursor fault injection
	mcpSrv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if s.Crash.HasCrashed() {
				return nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInternalError,
					Message: "server has crashed: session terminated",
				}
			}
			if method == "tools/list" && s.Paged.IsInterceptEnabled() {
				return s.Paged.HandleListTools(ctx, req)
			}
			return next(ctx, method, req)
		}
	})

	if cfg.registerDefaults {
		s.registerDefaultFixtures()
	}

	for _, ct := range cfg.customTools {
		mcpSrv.AddTool(ct.tool, ct.handler)
	}

	return s
}

func (s *Server) registerDefaultFixtures() {
	s.mcpServer.AddTool(s.EchoRaw.Tool(), s.EchoRaw.Handler)
	s.mcpServer.AddTool(s.Image.Tool(), s.Image.Handler)
	s.mcpServer.AddTool(s.FailTool.Tool(), s.FailTool.Handler)
	s.mcpServer.AddTool(s.Block.Tool(), s.Block.Handler)
	s.mcpServer.AddTool(s.Counter.Tool(), s.Counter.Handler)
	s.mcpServer.AddTool(s.Crash.Tool(), s.Crash.Handler)

	for _, t := range s.Paged.Tools() {
		s.mcpServer.AddTool(t, defaultPagedToolHandler(t.Name))
	}
}

// MCPServer returns the underlying SDK Server instance.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcpServer
}

// trackSession records an active server session.
func (s *Server) trackSession(session *mcp.ServerSession) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions = append(s.sessions, session)
}

// CloseSession closes a single tracked server session safely.
func (s *Server) CloseSession(session *mcp.ServerSession) {
	if session == nil {
		return
	}
	s.sessionsMu.Lock()
	for i, ss := range s.sessions {
		if ss == session {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			break
		}
	}
	s.sessionsMu.Unlock()
	_ = session.Close()
}

// closeAllSessions closes and clears all tracked server sessions.
func (s *Server) closeAllSessions() {
	s.sessionsMu.Lock()
	sessions := append([]*mcp.ServerSession(nil), s.sessions...)
	s.sessions = nil
	s.sessionsMu.Unlock()

	for _, session := range sessions {
		_ = session.Close()
	}
}

// safeAsyncCloseSessions schedules an asynchronous session closure after a short delay,
// ensuring the in-flight tool handler returns its response before the session connection is closed.
func (s *Server) safeAsyncCloseSessions() {
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.closeAllSessions()
	}()
}

// NewInMemoryClientTransport creates an in-memory transport pair, connects the server
// transport to the internal MCP server, and returns the paired client transport.
// Callers can pass this transport directly to mcp.NewClient.Connect.
func (s *Server) NewInMemoryClientTransport(ctx context.Context) (*mcp.InMemoryTransport, error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect server in-memory transport: %w", err)
	}
	s.trackSession(serverSession)
	return clientTransport, nil
}

// ConnectClient connects both the server and a new test client in-memory, returning
// the connected ClientSession and a cleanup function that terminates both sessions.
func (s *Server) ConnectClient(ctx context.Context, clientOpts *mcp.ClientOptions) (*mcp.ClientSession, func(), error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := s.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect server session: %w", err)
	}
	s.trackSession(serverSession)

	if clientOpts == nil {
		clientOpts = &mcp.ClientOptions{
			Capabilities: &mcp.ClientCapabilities{},
		}
	} else if clientOpts.Capabilities == nil {
		clientOpts.Capabilities = &mcp.ClientCapabilities{}
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, clientOpts)

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		s.CloseSession(serverSession)
		return nil, nil, fmt.Errorf("failed to connect client session: %w", err)
	}

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			_ = clientSession.Close()
			s.CloseSession(serverSession)
		})
	}
	return clientSession, cleanup, nil
}

// StartHTTP starts an httptest.Server using mcp.NewStreamableHTTPHandler and returns
// its endpoint URL and cleanup function.
func (s *Server) StartHTTP() (string, func(), error) {
	s.httpServerMu.Lock()
	defer s.httpServerMu.Unlock()

	if s.httpServer != nil {
		return s.httpServer.URL, s.httpServer.Close, nil
	}

	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	ts := httptest.NewServer(handler)
	s.httpServer = ts

	cleanup := func() {
		s.httpServerMu.Lock()
		defer s.httpServerMu.Unlock()
		if s.httpServer != nil {
			s.httpServer.Close()
			s.httpServer = nil
		}
	}
	return ts.URL, cleanup, nil
}

type cancellablePipe struct {
	r    *io.PipeReader
	w    *io.PipeWriter
	once sync.Once
}

type cancellableReader struct{ *cancellablePipe }

func (r *cancellableReader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (r *cancellableReader) Close() error {
	r.once.Do(func() {
		_ = r.w.CloseWithError(io.ErrClosedPipe)
		_ = r.r.Close()
	})
	return nil
}

type cancellableWriter struct{ *cancellablePipe }

func (w *cancellableWriter) Write(p []byte) (int, error) {
	return w.w.Write(p)
}

func (w *cancellableWriter) Close() error {
	w.once.Do(func() {
		_ = w.r.CloseWithError(io.ErrClosedPipe)
		_ = w.w.Close()
	})
	return nil
}

func newCancellablePipe() (io.ReadCloser, io.WriteCloser) {
	r, w := io.Pipe()
	cp := &cancellablePipe{r: r, w: w}
	return &cancellableReader{cp}, &cancellableWriter{cp}
}

// NewIOTransports creates two paired IOTransports connected via cancellable pipes (representing stdio)
// where closing either end unblocks and closes the other end, avoiding deadlocks.
func (s *Server) NewIOTransports() (clientTransport *mcp.IOTransport, serverTransport *mcp.IOTransport, cleanup func()) {
	c2sR, c2sW := newCancellablePipe()
	s2cR, s2cW := newCancellablePipe()

	clientTransport = &mcp.IOTransport{Reader: s2cR, Writer: c2sW}
	serverTransport = &mcp.IOTransport{Reader: c2sR, Writer: s2cW}

	cleanup = func() {
		_ = c2sW.Close()
		_ = c2sR.Close()
		_ = s2cW.Close()
		_ = s2cR.Close()
	}
	return clientTransport, serverTransport, cleanup
}

// NewIOClientTransport connects the server via an IOTransport and returns the client-side
// IOTransport and a cleanup function.
func (s *Server) NewIOClientTransport(ctx context.Context) (*mcp.IOTransport, func(), error) {
	ct, st, pipeCleanup := s.NewIOTransports()
	serverSession, err := s.mcpServer.Connect(ctx, st, nil)
	if err != nil {
		pipeCleanup()
		return nil, nil, fmt.Errorf("failed to connect server IO transport: %w", err)
	}
	s.trackSession(serverSession)

	cleanup := func() {
		_ = serverSession.Close()
		pipeCleanup()
	}
	return ct, cleanup, nil
}

// ResetAll resets the state and call counters of all fixture controllers.
func (s *Server) ResetAll() {
	s.EchoRaw.Reset()
	s.Image.Reset()
	s.FailTool.Reset()
	s.Block.Reset()
	s.Counter.Reset()
	s.Paged.Reset()
	s.Crash.Reset()
}

// Close closes all active sessions and stops any running HTTP server.
func (s *Server) Close() error {
	s.closeAllSessions()

	s.httpServerMu.Lock()
	if s.httpServer != nil {
		s.httpServer.Close()
		s.httpServer = nil
	}
	s.httpServerMu.Unlock()
	return nil
}
