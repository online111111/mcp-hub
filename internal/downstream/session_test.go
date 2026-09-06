package downstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func serverImpl(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:    name,
		Version: "1.0.0",
	}
}

// cancellablePipe wraps an io.Pipe such that closing either reader or writer
// unblocks and terminates both ends with io.ErrClosedPipe, preventing test deadlocks.
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

// newTestPipePair creates client and server IOTransports over cancellable bidirectional pipes.
// Calling cleanup closes all pipe ends to ensure no hanging goroutines remain.
func newTestPipePair() (clientTransport *mcp.IOTransport, serverTransport *mcp.IOTransport, cleanup func()) {
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

// 1. Options validation and forbidden headers
func TestOptionsValidation(t *testing.T) {
	t.Run("HTTPOptions_MissingEndpoint", func(t *testing.T) {
		opts := HTTPOptions{}
		err := opts.Validate()
		if !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("expected ErrInvalidOptions, got %v", err)
		}
	})

	t.Run("HTTPOptions_ForbiddenHeaders", func(t *testing.T) {
		forbidden := []string{
			"Host",
			"host",
			"Content-Length",
			"content-length",
			"Connection",
			"connection",
			"Mcp-Session-Id",
			"mcp-session-id",
			"MCP-Protocol-Version",
			"mcp-protocol-version",
		}
		for _, h := range forbidden {
			opts := HTTPOptions{
				Endpoint: "http://127.0.0.1:8080/mcp",
				Headers:  map[string]string{h: "value"},
			}
			err := opts.Validate()
			if !errors.Is(err, ErrForbiddenHeader) {
				t.Fatalf("header %q: expected ErrForbiddenHeader, got %v", h, err)
			}
		}
	})

	t.Run("IOOptions_MissingStreams", func(t *testing.T) {
		opts := IOOptions{}
		err := opts.Validate()
		if !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("expected ErrInvalidOptions, got %v", err)
		}
	})

	t.Run("Options_UnifiedValidation", func(t *testing.T) {
		opts := Options{Type: "invalid_type"}
		err := opts.Validate()
		if !errors.Is(err, ErrUnsupportedTransport) {
			t.Fatalf("expected ErrUnsupportedTransport, got %v", err)
		}
	})
}

// 2. HeaderInjectingRoundTripper: request cloning and header injection
func TestRoundTripper_HeaderInjection(t *testing.T) {
	var receivedHeaders http.Header
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	headers := map[string]string{
		"X-Custom-Auth": "bearer-token-123",
		"X-Service-ID":  "test-srv",
	}

	client := &http.Client{
		Transport: &HeaderInjectingRoundTripper{
			Base:    http.DefaultTransport,
			Headers: headers,
		},
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Original-Header", "original-value")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()

	// Verify original request header was not modified
	if req.Header.Get("X-Custom-Auth") != "" {
		t.Fatalf("original request Header was mutated!")
	}
	if req.Header.Get("Original-Header") != "original-value" {
		t.Fatalf("original header changed: %v", req.Header.Get("Original-Header"))
	}

	// Verify server received injected headers
	mu.Lock()
	defer mu.Unlock()
	if receivedHeaders.Get("X-Custom-Auth") != "bearer-token-123" {
		t.Fatalf("expected X-Custom-Auth injected, got %v", receivedHeaders.Get("X-Custom-Auth"))
	}
	if receivedHeaders.Get("X-Service-ID") != "test-srv" {
		t.Fatalf("expected X-Service-ID injected, got %v", receivedHeaders.Get("X-Service-ID"))
	}
	if receivedHeaders.Get("Original-Header") != "original-value" {
		t.Fatalf("expected Original-Header passed, got %v", receivedHeaders.Get("Original-Header"))
	}
}

// 3. No redirects: Verify redirects are rejected
func TestNoRedirects(t *testing.T) {
	var redirectHit atomic.Bool

	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	redirectOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirectOrigin.Close()

	httpClient, err := newHTTPClient(nil, nil)
	if err != nil {
		t.Fatalf("newHTTPClient: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, redirectOrigin.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	_, err = httpClient.Do(req)
	if err == nil {
		t.Fatalf("expected error on redirect, got nil")
	}
	if !errors.Is(err, ErrRedirectNotAllowed) {
		t.Fatalf("expected ErrRedirectNotAllowed, got %v", err)
	}
	if redirectHit.Load() {
		t.Fatalf("redirect target was unexpectedly contacted!")
	}
}

// 4. Streamable HTTP: Initialize, headers injection, ListTools, CallTool, Close
func TestStreamableHTTP_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var receivedAuthHeader atomic.Value

	server := mcp.NewServer(serverImpl("http-server"), nil)
	server.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "Echo input",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "http-echo-ok"}},
		}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		if auth := req.Header.Get("Authorization"); auth != "" {
			receivedAuthHeader.Store(auth)
		}
		return server
	}, nil)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	session, err := DialHTTP(ctx, HTTPOptions{
		Endpoint: ts.URL,
		Headers: map[string]string{
			"Authorization": "Bearer test-token-xyz",
		},
		StartupTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialHTTP: %v", err)
	}
	defer session.Close()

	// Verify header was received on server
	if val, ok := receivedAuthHeader.Load().(string); !ok || val != "Bearer test-token-xyz" {
		t.Fatalf("server did not receive expected Authorization header, got %v", receivedAuthHeader.Load())
	}

	// ListTools
	tools, err := session.ListAllTools(ctx)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	// CallTool
	callRes, err := session.CallTool(ctx, "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(callRes.Content) == 0 {
		t.Fatalf("expected content in CallTool response")
	}
	tc, ok := callRes.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "http-echo-ok" {
		t.Fatalf("unexpected CallTool content: %+v", callRes.Content[0])
	}

	// Close idempotency
	if err := session.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !session.IsClosed() {
		t.Fatalf("session should report IsClosed == true")
	}

	// Calls after close fail with ErrSessionClosed
	_, err = session.CallTool(ctx, "echo", nil)
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed after close, got %v", err)
	}
}

// 5. Generic IOTransport: in-memory pipes, CallTool with raw big int JSON args
func TestIOTransport_EndToEnd_RawArgs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var receivedRawArgs atomic.Value

	server := mcp.NewServer(serverImpl("io-server"), nil)
	server.AddTool(&mcp.Tool{
		Name:        "raw_args_inspector",
		Description: "Inspects raw JSON arguments preserving structure and large integers",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Capture raw JSON arguments directly
		var raw map[string]any
		if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
			return nil, err
		}
		receivedRawArgs.Store(string(req.Params.Arguments))
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "raw-ok"}},
		}, nil
	})

	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	session, err := DialIO(ctx, IOOptions{
		Reader:         clientTransport.Reader,
		Writer:         clientTransport.Writer,
		StartupTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}
	defer session.Close()

	// ListAllTools
	tools, err := session.ListAllTools(ctx)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "raw_args_inspector" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	// CallTool with a large integer (9007199254740993 > 2^53, would lose precision if parsed as float64)
	rawInput := `{"big_int":9007199254740993,"description":"precise"}`
	res, err := session.CallTool(ctx, "raw_args_inspector", json.RawMessage(rawInput))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected content, got empty")
	}

	// Verify server received the exact raw integer digits without float64 alteration
	received, ok := receivedRawArgs.Load().(string)
	if !ok {
		t.Fatalf("server did not record raw arguments")
	}
	if !bytes.Contains([]byte(received), []byte("9007199254740993")) {
		t.Fatalf("expected 9007199254740993 in server raw args, got: %s", received)
	}
}

// 6. Pagination across multiple pages
func TestPagination_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("paged-server"), nil)
	// Add 5 tools
	for i := 1; i <= 5; i++ {
		server.AddTool(&mcp.Tool{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: fmt.Sprintf("Paged tool item %d description", i),
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	}

	// Intercept tools/list to paginate with PageSize=2
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				listReq, ok := req.(*mcp.ListToolsRequest)
				if !ok {
					return next(ctx, method, req)
				}
				cursor := ""
				if listReq.Params != nil {
					cursor = listReq.Params.Cursor
				}
				switch cursor {
				case "":
					return &mcp.ListToolsResult{
						NextCursor: "cur_page_2",
						Tools: []*mcp.Tool{
							{Name: "tool_1", Description: "Tool 1 description", InputSchema: map[string]any{"type": "object"}},
							{Name: "tool_2", Description: "Tool 2 description", InputSchema: map[string]any{"type": "object"}},
						},
					}, nil
				case "cur_page_2":
					return &mcp.ListToolsResult{
						NextCursor: "cur_page_3",
						Tools: []*mcp.Tool{
							{Name: "tool_3", Description: "Tool 3 description", InputSchema: map[string]any{"type": "object"}},
							{Name: "tool_4", Description: "Tool 4 description", InputSchema: map[string]any{"type": "object"}},
						},
					}, nil
				case "cur_page_3":
					return &mcp.ListToolsResult{
						NextCursor: "",
						Tools: []*mcp.Tool{
							{Name: "tool_5", Description: "Tool 5 description", InputSchema: map[string]any{"type": "object"}},
						},
					}, nil
				default:
					return nil, fmt.Errorf("unknown cursor %q", cursor)
				}
			}
			return next(ctx, method, req)
		}
	})

	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	session, err := DialIO(ctx, IOOptions{Reader: clientTransport.Reader, Writer: clientTransport.Writer})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}
	defer session.Close()

	tools, err := session.ListAllTools(ctx)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools across 3 pages, got %d", len(tools))
	}
	for i, tool := range tools {
		expectedName := fmt.Sprintf("tool_%d", i+1)
		if tool.Name != expectedName {
			t.Fatalf("tool %d: expected %s, got %s", i, expectedName, tool.Name)
		}
	}
}

// 7. Duplicate cursor detection
func TestPagination_DuplicateCursor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("dup-cursor-server"), nil)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				listReq := req.(*mcp.ListToolsRequest)
				cursor := ""
				if listReq.Params != nil {
					cursor = listReq.Params.Cursor
				}
				if cursor == "" {
					return &mcp.ListToolsResult{
						NextCursor: "loop_cursor",
						Tools: []*mcp.Tool{{
							Name:        "t1",
							Description: "Tool 1 description",
							InputSchema: map[string]any{"type": "object"},
						}},
					}, nil
				}
				if cursor == "loop_cursor" {
					// Server erroneously returns loop_cursor again
					return &mcp.ListToolsResult{
						NextCursor: "loop_cursor",
						Tools: []*mcp.Tool{{
							Name:        "t2",
							Description: "Tool 2 description",
							InputSchema: map[string]any{"type": "object"},
						}},
					}, nil
				}
			}
			return next(ctx, method, req)
		}
	})

	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	session, err := DialIO(ctx, IOOptions{Reader: clientTransport.Reader, Writer: clientTransport.Writer})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}
	defer session.Close()

	_, err = session.ListAllTools(ctx)
	if err == nil {
		t.Fatalf("expected error on duplicate cursor, got nil")
	}
	if !errors.Is(err, ErrDuplicateCursor) {
		t.Fatalf("expected ErrDuplicateCursor, got %v", err)
	}
}

// 8. Duplicate tool name detection
func TestPagination_DuplicateToolName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("dup-name-server"), nil)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				return &mcp.ListToolsResult{
					Tools: []*mcp.Tool{
						{
							Name:        "duplicate_tool",
							Description: "Duplicate tool description",
							InputSchema: map[string]any{"type": "object"},
						},
						{
							Name:        "duplicate_tool",
							Description: "Duplicate tool description",
							InputSchema: map[string]any{"type": "object"},
						},
					},
				}, nil
			}
			return next(ctx, method, req)
		}
	})

	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	session, err := DialIO(ctx, IOOptions{Reader: clientTransport.Reader, Writer: clientTransport.Writer})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}
	defer session.Close()

	_, err = session.ListAllTools(ctx)
	if err == nil {
		t.Fatalf("expected error on duplicate tool name, got nil")
	}
	if !errors.Is(err, ErrDuplicateToolName) {
		t.Fatalf("expected ErrDuplicateToolName, got %v", err)
	}
}

// 9. ToolListChanged dirty signal, callback queue, and re-fetch during discovery
func TestToolListChanged_DirtySignalAndQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("dynamic-server"), nil)
	server.AddTool(&mcp.Tool{
		Name:        "initial_tool",
		Description: "Initial dynamic tool description",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	callbackTriggered := make(chan struct{}, 10)
	session, err := DialIO(ctx, IOOptions{
		Reader: clientTransport.Reader,
		Writer: clientTransport.Writer,
		OnToolListChanged: func() {
			callbackTriggered <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}
	defer session.Close()

	if session.IsDirty() {
		t.Fatalf("session should not be dirty initially")
	}

	// Server dynamically adds a tool, firing notifications/tools/list_changed
	server.AddTool(&mcp.Tool{
		Name:        "added_tool",
		Description: "Added dynamic tool description",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	// Verify callback fired via queue
	select {
	case <-callbackTriggered:
		// callback received
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for OnToolListChanged callback")
	}

	// Verify dirty signal is set
	if !session.IsDirty() {
		t.Fatalf("session should be dirty after tool list changed notification")
	}
	if session.DirtyCount() == 0 {
		t.Fatalf("DirtyCount should be > 0")
	}

	// ListAllTools should discover both tools and clear dirty
	tools, err := session.ListAllTools(ctx)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if session.IsDirty() {
		t.Fatalf("session dirty flag should be cleared after ListAllTools completes")
	}
}

// 10. Verify that a change arriving during discovery triggers at most one re-fetch, merging subsequent changes to next round
func TestToolListChanged_ReFetchDuringDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("mid-flight-server"), nil)
	server.AddTool(&mcp.Tool{
		Name:        "base_tool",
		Description: "Base dynamic tool description",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	var listCalls atomic.Int32
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				callNum := listCalls.Add(1)
				if callNum == 1 {
					// During the first discovery call, dynamically add another tool
					server.AddTool(&mcp.Tool{
						Name:        "dynamic_midflight_1",
						Description: "Dynamic midflight tool 1 description",
						InputSchema: map[string]any{"type": "object"},
					}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					})
					// Give notification a moment to arrive on client
					time.Sleep(50 * time.Millisecond)
				} else if callNum == 2 {
					// During the single retry, trigger another notification
					server.AddTool(&mcp.Tool{
						Name:        "dynamic_midflight_2",
						Description: "Dynamic midflight tool 2 description",
						InputSchema: map[string]any{"type": "object"},
					}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					})
					time.Sleep(50 * time.Millisecond)
				}
			}
			return next(ctx, method, req)
		}
	})

	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	session, err := DialIO(ctx, IOOptions{Reader: clientTransport.Reader, Writer: clientTransport.Writer})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}
	defer session.Close()

	tools, err := session.ListAllTools(ctx)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}

	// First pass triggered 1 call, saw dirty, retried once (call 2). Total calls == 2!
	if calls := listCalls.Load(); calls != 2 {
		t.Fatalf("expected exactly 2 list calls (1 initial + 1 retry), got %d", calls)
	}

	if len(tools) < 2 {
		t.Fatalf("expected at least 2 tools from retry, got %d", len(tools))
	}

	// Dirty flag from call 2's notification must remain set for the next round ("后续合并到下轮")
	if !session.IsDirty() {
		t.Fatalf("expected dirty flag to remain set for subsequent rounds after second change")
	}
}

// 11. Context and lifetime separation
func TestContextLifetimeSeparation(t *testing.T) {
	t.Run("StartupTimeoutCancellationDoesNotBreakSession", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		server := mcp.NewServer(serverImpl("startup-server"), nil)
		server.AddTool(&mcp.Tool{
			Name:        "ping",
			Description: "Ping pong fixture",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "pong"}},
			}, nil
		})

		clientTransport, serverTransport, pipeCleanup := newTestPipePair()
		defer pipeCleanup()

		ss, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatalf("server connect: %v", err)
		}
		defer ss.Close()

		// Dial with a short 200ms startup timeout
		session, err := DialIO(ctx, IOOptions{
			Reader:         clientTransport.Reader,
			Writer:         clientTransport.Writer,
			StartupTimeout: 200 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("DialIO: %v", err)
		}
		defer session.Close()

		// Wait longer than the startup timeout
		time.Sleep(300 * time.Millisecond)

		// Session must still be healthy and usable
		res, err := session.CallTool(ctx, "ping", nil)
		if err != nil {
			t.Fatalf("CallTool failed after startup timeout expired: %v", err)
		}
		tc, ok := res.Content[0].(*mcp.TextContent)
		if !ok || tc.Text != "pong" {
			t.Fatalf("unexpected result: %+v", res.Content[0])
		}
	})

	t.Run("StartupTimeoutExpirationFailsConnect", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Server absorbs stdin but hangs and never replies on stdout
		c1, c2 := net.Pipe()
		defer c2.Close()
		defer c1.Close()
		go io.Copy(io.Discard, c2)

		start := time.Now()
		_, err := DialIO(ctx, IOOptions{
			Reader:         c1,
			Writer:         c1,
			StartupTimeout: 100 * time.Millisecond,
		})
		duration := time.Since(start)

		if err == nil {
			t.Fatalf("expected error on startup timeout, got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
		if duration > 2*time.Second {
			t.Fatalf("startup timeout took too long: %v", duration)
		}
	})

	t.Run("LifetimeContextCancellationClosesSession", func(t *testing.T) {
		lifetimeCtx, cancelLifetime := context.WithCancel(context.Background())

		server := mcp.NewServer(serverImpl("lifetime-server"), nil)
		clientTransport, serverTransport, pipeCleanup := newTestPipePair()
		defer pipeCleanup()

		ss, err := server.Connect(context.Background(), serverTransport, nil)
		if err != nil {
			t.Fatalf("server connect: %v", err)
		}
		defer ss.Close()

		session, err := DialIO(lifetimeCtx, IOOptions{
			Reader: clientTransport.Reader,
			Writer: clientTransport.Writer,
		})
		if err != nil {
			t.Fatalf("DialIO: %v", err)
		}

		if session.IsClosed() {
			t.Fatalf("session should not be closed initially")
		}

		// Cancel lifetime context
		cancelLifetime()

		// Wait briefly for background watcher to close session
		deadline := time.Now().Add(2 * time.Second)
		for !session.IsClosed() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}

		if !session.IsClosed() {
			t.Fatalf("session did not close after lifetime context was cancelled")
		}
	})
}

// 12. Close idempotency under concurrency
func TestClose_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("close-server"), nil)
	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	session, err := DialIO(ctx, IOOptions{
		Reader: clientTransport.Reader,
		Writer: clientTransport.Writer,
	})
	if err != nil {
		t.Fatalf("DialIO: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = session.Close()
		}()
	}
	wg.Wait()

	if !session.IsClosed() {
		t.Fatalf("session should be closed")
	}
}

// 13. Dial using unified Options
func TestDial_UnifiedOptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("unified-server"), nil)
	clientTransport, serverTransport, pipeCleanup := newTestPipePair()
	defer pipeCleanup()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	session, err := Dial(ctx, Options{
		Type:   TransportTypeIO,
		Reader: clientTransport.Reader,
		Writer: clientTransport.Writer,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer session.Close()

	if session.IsClosed() {
		t.Fatalf("session should be open")
	}
}
