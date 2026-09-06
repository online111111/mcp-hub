package sdkprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Helper to create a basic server implementation descriptor
func serverImpl(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:    name,
		Version: "1.0.0",
	}
}

// Helper to create a basic client implementation descriptor
func clientImpl(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:    name,
		Version: "1.0.0",
	}
}

// 1. Downstream transports: Streamable HTTP, In-Memory (net.Pipe), and IOTransport (io.Pipe)
func TestDownstreamTransports(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1a. Streamable HTTP via httptest.Server
	t.Run("StreamableHTTP", func(t *testing.T) {
		server := mcp.NewServer(serverImpl("http-server"), nil)
		server.AddTool(&mcp.Tool{
			Name:        "echo",
			Description: "Echo input",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "http-ok"}},
			}, nil
		})

		httpHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
			return server
		}, nil)

		ts := httptest.NewServer(httpHandler)
		defer ts.Close()

		client := mcp.NewClient(clientImpl("http-client"), nil)
		clientTransport := &mcp.StreamableClientTransport{
			Endpoint: ts.URL,
		}

		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatalf("Streamable HTTP client Connect failed: %v", err)
		}
		defer session.Close()

		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "echo"})
		if err != nil {
			t.Fatalf("CallTool over HTTP failed: %v", err)
		}
		if len(res.Content) == 0 {
			t.Fatalf("Expected content in CallTool response, got empty")
		}
		tc, ok := res.Content[0].(*mcp.TextContent)
		if !ok || tc.Text != "http-ok" {
			t.Fatalf("Unexpected CallTool response: %+v", res.Content[0])
		}
	})

	// 1b. In-Memory Transport via mcp.NewInMemoryTransports()
	t.Run("InMemoryPipe", func(t *testing.T) {
		server := mcp.NewServer(serverImpl("inmem-server"), nil)
		server.AddTool(&mcp.Tool{
			Name:        "ping",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "pong"}},
			}, nil
		})

		ct, st := mcp.NewInMemoryTransports()

		serverSession, err := server.Connect(ctx, st, nil)
		if err != nil {
			t.Fatalf("Server in-memory Connect failed: %v", err)
		}
		defer serverSession.Close()

		client := mcp.NewClient(clientImpl("inmem-client"), nil)
		clientSession, err := client.Connect(ctx, ct, nil)
		if err != nil {
			t.Fatalf("Client in-memory Connect failed: %v", err)
		}
		defer clientSession.Close()

		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "ping"})
		if err != nil {
			t.Fatalf("CallTool over in-memory pipe failed: %v", err)
		}
		if len(res.Content) == 0 {
			t.Fatalf("Expected content, got 0")
		}
		tc := res.Content[0].(*mcp.TextContent)
		if tc.Text != "pong" {
			t.Fatalf("Expected pong, got %s", tc.Text)
		}
	})

	// 1c. IOTransport via io.Pipe (representing stdin/stdout)
	t.Run("IOTransport_StdinStdoutFixture", func(t *testing.T) {
		server := mcp.NewServer(serverImpl("io-server"), nil)
		server.AddTool(&mcp.Tool{
			Name:        "greet",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "hello-stdio"}},
			}, nil
		})

		// Server stdin <- Client stdout (c2s)
		c2sReader, c2sWriter := io.Pipe()
		// Client stdin <- Server stdout (s2c)
		s2cReader, s2cWriter := io.Pipe()

		serverTransport := &mcp.IOTransport{
			Reader: c2sReader,
			Writer: s2cWriter,
		}
		clientTransport := &mcp.IOTransport{
			Reader: s2cReader,
			Writer: c2sWriter,
		}

		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatalf("Server IOTransport Connect failed: %v", err)
		}
		defer serverSession.Close()

		client := mcp.NewClient(clientImpl("io-client"), nil)
		clientSession, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatalf("Client IOTransport Connect failed: %v", err)
		}
		defer clientSession.Close()

		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "greet"})
		if err != nil {
			t.Fatalf("CallTool over IOTransport failed: %v", err)
		}
		tc := res.Content[0].(*mcp.TextContent)
		if tc.Text != "hello-stdio" {
			t.Fatalf("Expected hello-stdio, got %s", tc.Text)
		}
	})
}

// 2. Raw Server.AddTool proxy preserving JSON args, structured result, content, and isError
func TestRawServerAddToolProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("proxy-server"), nil)

	var recordedRawArgs json.RawMessage
	var recordedSession *mcp.ServerSession

	// Raw AddTool accepts untyped json.RawMessage in req.Params.Arguments
	server.AddTool(&mcp.Tool{
		Name:        "proxy_tool",
		Description: "Raw proxy tool testing args & result preservation",
		InputSchema: map[string]any{
			"type": "object",
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		recordedRawArgs = req.Params.Arguments
		recordedSession = req.Session

		var parsed map[string]any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &parsed); err != nil {
				return nil, fmt.Errorf("unmarshal raw args: %w", err)
			}
		}

		// Check if caller simulated error
		if val, ok := parsed["simulate_error"].(bool); ok && val {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "simulated error occurred in proxy"},
				},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "unstructured log/summary"},
			},
			StructuredContent: map[string]any{
				"echo":      parsed,
				"status":    "custom_success",
				"record_id": float64(12345),
			},
			IsError: false,
		}, nil
	})

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("proxy-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 2a. Call with complex structured payload and verify preservation
	testArgs := map[string]any{
		"model":  "deepseek-r1",
		"nested": map[string]any{"depth": float64(3), "enabled": true},
		"tags":   []any{"prod", "v2"},
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "proxy_tool",
		Arguments: testArgs,
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if res.IsError {
		t.Errorf("Expected IsError=false, got true")
	}

	// Verify server recorded raw json without loss
	var unmarshaledRecorded map[string]any
	if err := json.Unmarshal(recordedRawArgs, &unmarshaledRecorded); err != nil {
		t.Fatalf("Recorded raw args invalid json: %v", err)
	}
	if unmarshaledRecorded["model"] != "deepseek-r1" {
		t.Errorf("Expected deepseek-r1, got %v", unmarshaledRecorded["model"])
	}

	// Verify Content preserved
	if len(res.Content) != 1 {
		t.Fatalf("Expected 1 Content item, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "unstructured log/summary" {
		t.Errorf("Content mismatch: %+v", res.Content[0])
	}

	// Verify StructuredContent preserved
	scMap, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent was not map[string]any, got %T: %+v", res.StructuredContent, res.StructuredContent)
	}
	if scMap["status"] != "custom_success" || scMap["record_id"] != float64(12345) {
		t.Errorf("StructuredContent fields mismatch: %+v", scMap)
	}

	// 2b. Verify IsError: true preservation
	errRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "proxy_tool",
		Arguments: map[string]any{
			"simulate_error": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool returned protocol error when IsError=true was expected: %v", err)
	}
	if !errRes.IsError {
		t.Fatalf("Expected errRes.IsError == true, got false")
	}
	if len(errRes.Content) == 0 {
		t.Fatalf("Expected error content message, got none")
	}
	errTc := errRes.Content[0].(*mcp.TextContent)
	if errTc.Text != "simulated error occurred in proxy" {
		t.Errorf("Unexpected error text: %s", errTc.Text)
	}
	// For in-memory (ioConn) transport, req.Session is non-nil while req.Session.ID() is "" by SDK design
	if recordedSession == nil {
		t.Errorf("Expected recordedSession to be non-nil")
	}
}

// 3. Server AddTool / RemoveTools dynamic updates and ToolListChangedHandler
func TestDynamicToolUpdatesAndNotification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("dynamic-server"), nil)
	server.AddTool(&mcp.Tool{
		Name:        "initial_tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	listChangedCh := make(chan struct{}, 10)
	client := mcp.NewClient(clientImpl("dynamic-client"), &mcp.ClientOptions{
		ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
			listChangedCh <- struct{}{}
		},
	})

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Initial check
	initialList, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("initial ListTools: %v", err)
	}
	if len(initialList.Tools) != 1 || initialList.Tools[0].Name != "initial_tool" {
		t.Fatalf("Expected initial_tool only, got %+v", initialList.Tools)
	}

	// 3a. Dynamically add a second tool
	server.AddTool(&mcp.Tool{
		Name:        "second_tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	// Wait for ToolListChanged notification
	select {
	case <-listChangedCh:
		// notification received
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ToolListChanged notification on AddTool")
	}

	updatedList, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("updated ListTools: %v", err)
	}
	if len(updatedList.Tools) != 2 {
		t.Fatalf("Expected 2 tools after AddTool, got %d", len(updatedList.Tools))
	}

	// 3b. Dynamically remove initial_tool via server.RemoveTools
	server.RemoveTools("initial_tool")

	// Wait for ToolListChanged notification
	select {
	case <-listChangedCh:
		// notification received
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ToolListChanged notification on RemoveTools")
	}

	afterRemoveList, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("after remove ListTools: %v", err)
	}
	if len(afterRemoveList.Tools) != 1 || afterRemoveList.Tools[0].Name != "second_tool" {
		t.Fatalf("Expected only second_tool left, got %+v", afterRemoveList.Tools)
	}
}

// 4. Multiple clients call routing & concurrency
func TestMultipleClientsRouting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	t.Run("StreamableHTTP_DistinctSessionIDs", func(t *testing.T) {
		server := mcp.NewServer(serverImpl("multi-http-server"), nil)
		var sessionCallCounts sync.Map

		server.AddTool(&mcp.Tool{
			Name:        "whoami",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sessID := req.Session.ID()
			val, _ := sessionCallCounts.LoadOrStore(sessID, new(atomic.Int64))
			val.(*atomic.Int64).Add(1)

			var args map[string]any
			if len(req.Params.Arguments) > 0 {
				_ = json.Unmarshal(req.Params.Arguments, &args)
			}

			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"session_id": sessID,
					"client_tag": args["tag"],
				},
			}, nil
		})

		ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
			return server
		}, nil))
		defer ts.Close()

		const numClients = 3
		var clientSessions []*mcp.ClientSession

		for i := 0; i < numClients; i++ {
			client := mcp.NewClient(clientImpl(fmt.Sprintf("http-client-%d", i)), nil)
			cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
			if err != nil {
				t.Fatalf("client %d connect failed: %v", i, err)
			}
			clientSessions = append(clientSessions, cs)
		}

		defer func() {
			for _, cs := range clientSessions {
				_ = cs.Close()
			}
		}()

		var wg sync.WaitGroup
		errCh := make(chan error, numClients*10)

		for i := 0; i < numClients; i++ {
			tag := fmt.Sprintf("http-tag-%d", i)
			cs := clientSessions[i]
			wg.Add(1)
			go func(c *mcp.ClientSession, expectedTag string) {
				defer wg.Done()
				for k := 0; k < 4; k++ {
					res, err := c.CallTool(ctx, &mcp.CallToolParams{
						Name:      "whoami",
						Arguments: map[string]any{"tag": expectedTag},
					})
					if err != nil {
						errCh <- fmt.Errorf("http call error: %w", err)
						return
					}
					sc, ok := res.StructuredContent.(map[string]any)
					if !ok {
						errCh <- fmt.Errorf("unexpected StructuredContent type")
						return
					}
					if sc["client_tag"] != expectedTag {
						errCh <- fmt.Errorf("tag mismatch: expected %s, got %v", expectedTag, sc["client_tag"])
						return
					}
					if sc["session_id"] == "" {
						errCh <- fmt.Errorf("expected non-empty session_id over streamable HTTP")
						return
					}
				}
			}(cs, tag)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatal(err)
		}

		countSessions := 0
		sessionCallCounts.Range(func(key, value any) bool {
			countSessions++
			calls := value.(*atomic.Int64).Load()
			if calls != 4 {
				t.Errorf("Session %v expected 4 calls, got %d", key, calls)
			}
			return true
		})

		if countSessions != numClients {
			t.Fatalf("Expected %d distinct session IDs over HTTP, got %d", numClients, countSessions)
		}
	})

	t.Run("InMemory_DistinctSessionPointers", func(t *testing.T) {
		server := mcp.NewServer(serverImpl("multi-inmem-server"), nil)
		var sessionCallCounts sync.Map

		server.AddTool(&mcp.Tool{
			Name:        "whoami",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sessPtr := req.Session
			val, _ := sessionCallCounts.LoadOrStore(sessPtr, new(atomic.Int64))
			val.(*atomic.Int64).Add(1)

			var args map[string]any
			if len(req.Params.Arguments) > 0 {
				_ = json.Unmarshal(req.Params.Arguments, &args)
			}

			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"client_tag": args["tag"],
				},
			}, nil
		})

		const numClients = 4
		var clientSessions []*mcp.ClientSession
		var serverSessions []*mcp.ServerSession

		for i := 0; i < numClients; i++ {
			ct, st := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			if err != nil {
				t.Fatalf("client %d server connect failed: %v", i, err)
			}
			serverSessions = append(serverSessions, ss)

			client := mcp.NewClient(clientImpl(fmt.Sprintf("client-%d", i)), nil)
			cs, err := client.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatalf("client %d client connect failed: %v", i, err)
			}
			clientSessions = append(clientSessions, cs)
		}

		defer func() {
			for _, cs := range clientSessions {
				_ = cs.Close()
			}
			for _, ss := range serverSessions {
				_ = ss.Close()
			}
		}()

		var wg sync.WaitGroup
		errCh := make(chan error, numClients*10)

		for i := 0; i < numClients; i++ {
			tag := fmt.Sprintf("tag-%d", i)
			cs := clientSessions[i]
			wg.Add(1)
			go func(c *mcp.ClientSession, expectedTag string) {
				defer wg.Done()
				for k := 0; k < 5; k++ {
					res, err := c.CallTool(ctx, &mcp.CallToolParams{
						Name:      "whoami",
						Arguments: map[string]any{"tag": expectedTag},
					})
					if err != nil {
						errCh <- fmt.Errorf("call error: %w", err)
						return
					}
					sc, ok := res.StructuredContent.(map[string]any)
					if !ok {
						errCh <- fmt.Errorf("unexpected StructuredContent type")
						return
					}
					if sc["client_tag"] != expectedTag {
						errCh <- fmt.Errorf("tag mismatch: expected %s, got %v", expectedTag, sc["client_tag"])
						return
					}
				}
			}(cs, tag)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatal(err)
		}

		countSessions := 0
		sessionCallCounts.Range(func(key, value any) bool {
			countSessions++
			calls := value.(*atomic.Int64).Load()
			if calls != 5 {
				t.Errorf("Session %v expected 5 calls, got %d", key, calls)
			}
			return true
		})

		if countSessions != numClients {
			t.Fatalf("Expected %d distinct session pointers in memory, got %d", numClients, countSessions)
		}
	})
}

// 5. Cancellation via context propagation
func TestCancellationViaContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("cancel-server"), nil)

	handlerStarted := make(chan struct{})
	handlerCancelled := make(chan struct{})

	server.AddTool(&mcp.Tool{
		Name:        "slow_operation",
		InputSchema: map[string]any{"type": "object"},
	}, func(callCtx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		close(handlerStarted)
		select {
		case <-callCtx.Done():
			close(handlerCancelled)
			return nil, callCtx.Err()
		case <-time.After(5 * time.Second):
			return &mcp.CallToolResult{}, nil
		}
	})

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("cancel-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	callCtx, callCancel := context.WithCancel(ctx)
	callDone := make(chan error, 1)

	go func() {
		_, err := cs.CallTool(callCtx, &mcp.CallToolParams{Name: "slow_operation"})
		callDone <- err
	}()

	// Wait until handler is running
	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start in time")
	}

	// Cancel caller context
	callCancel()

	// Verify server handler observed context cancellation
	select {
	case <-handlerCancelled:
		// cancellation was successfully observed on the server
	case <-time.After(3 * time.Second):
		t.Fatal("server tool handler did not receive cancellation via context.Done()")
	}

	// Verify client call completed with error
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("expected non-nil error on cancelled CallTool")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client CallTool did not unblock after cancellation")
	}
}

// 6. Middleware intercepting tools/list from atomic snapshot while registered AddTool remains SDK-based
func TestMiddlewareToolsListAtomicSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("mw-server"), &mcp.ServerOptions{
		// Explicitly advertise tools capability even if snapshot is dynamic
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{ListChanged: true},
		},
	})

	// Register a real SDK tool for tool execution
	server.AddTool(&mcp.Tool{
		Name:        "native_sdk_tool",
		Description: "A native tool handled by SDK",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "sdk-native-executed"}},
		}, nil
	})

	// Prepare an atomic snapshot for tools/list
	var toolSnapshot atomic.Pointer[mcp.ListToolsResult]
	initialSnapshot := &mcp.ListToolsResult{
		Tools: []*mcp.Tool{
			{
				Name:        "virtual_snapshot_tool",
				Description: "Returned from atomic snapshot via middleware",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}
	toolSnapshot.Store(initialSnapshot)

	// Install receiving middleware to intercept "tools/list"
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				snap := toolSnapshot.Load()
				return snap, nil
			}
			// For all other methods (including tools/call, ping, etc.), fall through to SDK
			return next(ctx, method, req)
		}
	})

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("mw-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 6a. Verify tools/list returns the atomic snapshot instead of server's native_sdk_tool
	listRes, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(listRes.Tools) != 1 || listRes.Tools[0].Name != "virtual_snapshot_tool" {
		t.Fatalf("Expected virtual_snapshot_tool from snapshot, got %+v", listRes.Tools)
	}

	// 6b. Update atomic snapshot dynamically and verify new snapshot is returned on next list call
	toolSnapshot.Store(&mcp.ListToolsResult{
		Tools: []*mcp.Tool{
			{
				Name:        "virtual_snapshot_tool_v2",
				Description: "Updated snapshot version",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	})

	listRes2, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools v2 failed: %v", err)
	}
	if len(listRes2.Tools) != 1 || listRes2.Tools[0].Name != "virtual_snapshot_tool_v2" {
		t.Fatalf("Expected virtual_snapshot_tool_v2, got %+v", listRes2.Tools)
	}

	// 6c. Verify normal registered AddTool calls still execute via standard SDK handler
	callRes, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "native_sdk_tool"})
	if err != nil {
		t.Fatalf("CallTool native_sdk_tool failed: %v", err)
	}
	if len(callRes.Content) == 0 {
		t.Fatalf("Expected content in callRes, got empty")
	}
	tc := callRes.Content[0].(*mcp.TextContent)
	if tc.Text != "sdk-native-executed" {
		t.Fatalf("Expected sdk-native-executed, got %s", tc.Text)
	}
}

// 7. Real Two-Hop Forwarding Chain: Client -> Hub (Streamable HTTP) -> downstream ClientSession -> Downstream Server
func TestTwoHopForwardingChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Downstream side-effect counters
	var (
		normalSideEffects atomic.Int32
		errorSideEffects  atomic.Int32
		cancelSideEffects atomic.Int32
	)

	// Step 1: Set up Downstream Server
	downstreamServer := mcp.NewServer(serverImpl("downstream-server"), nil)

	// Complex tool on downstream server
	downstreamServer.AddTool(&mcp.Tool{
		Name:        "downstream_complex_tool",
		Description: "Downstream tool verifying big int, image, structured data, and error",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Verify raw JSON argument contains big integer 9007199254740993 literally without float64 precision loss
		if !bytes.Contains(req.Params.Arguments, []byte("9007199254740993")) {
			return nil, fmt.Errorf("big int 9007199254740993 not found in raw arguments: %s", string(req.Params.Arguments))
		}

		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			dec.UseNumber()
			_ = dec.Decode(&args)
		}

		if val, ok := args["simulate_error"].(bool); ok && val {
			errorSideEffects.Add(1)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "downstream error occurred"},
				},
				IsError: true,
			}, nil
		}

		normalSideEffects.Add(1)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "downstream normal text output"},
				&mcp.ImageContent{
					Data:     []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0xAA, 0xBB},
					MIMEType: "image/png",
				},
			},
			StructuredContent: map[string]any{
				"big_int_verified": "9007199254740993",
				"status":           "downstream_success",
				"level":            "production",
			},
			IsError: false,
		}, nil
	})

	// Slow tool on downstream server for cancellation test
	downstreamStarted := make(chan struct{})
	downstreamCancelled := make(chan struct{})
	downstreamServer.AddTool(&mcp.Tool{
		Name:        "downstream_slow_tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cancelSideEffects.Add(1)
		close(downstreamStarted)
		select {
		case <-ctx.Done():
			close(downstreamCancelled)
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return &mcp.CallToolResult{}, nil
		}
	})

	// Connect Hub to Downstream Server via In-Memory Transport (Hop 2)
	hubToDownstreamTransport, downstreamServerTransport := mcp.NewInMemoryTransports()
	downstreamSession, err := downstreamServer.Connect(ctx, downstreamServerTransport, nil)
	if err != nil {
		t.Fatalf("downstream server connect: %v", err)
	}
	defer downstreamSession.Close()

	hubDownstreamClient := mcp.NewClient(clientImpl("hub-downstream-client"), nil)
	hubDownstreamSession, err := hubDownstreamClient.Connect(ctx, hubToDownstreamTransport, nil)
	if err != nil {
		t.Fatalf("hub downstream client connect: %v", err)
	}
	defer hubDownstreamSession.Close()

	// Step 2: Set up Hub Server with raw forwarding proxy
	hubServer := mcp.NewServer(serverImpl("hub-server"), nil)

	forwardHandler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Pass raw arguments directly into downstream call without parsing or re-encoding
		return hubDownstreamSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      req.Params.Name,
			Arguments: req.Params.Arguments,
		})
	}

	hubServer.AddTool(&mcp.Tool{
		Name:        "downstream_complex_tool",
		InputSchema: map[string]any{"type": "object"},
	}, forwardHandler)

	hubServer.AddTool(&mcp.Tool{
		Name:        "downstream_slow_tool",
		InputSchema: map[string]any{"type": "object"},
	}, forwardHandler)

	// Expose Hub Server over Streamable HTTP (Hop 1)
	hubHTTPServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return hubServer
	}, nil))
	defer hubHTTPServer.Close()

	// Step 3: Connect Client to Hub Server over Streamable HTTP
	client := mcp.NewClient(clientImpl("origin-client"), nil)
	clientSession, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: hubHTTPServer.URL}, nil)
	if err != nil {
		t.Fatalf("origin client connect over HTTP: %v", err)
	}
	defer clientSession.Close()

	// 7a. Normal call: Big integer 9007199254740993, ImageContent, Structured data, and side-effect count
	t.Run("NormalCall_BigInt_Image_Structured", func(t *testing.T) {
		rawInput := json.RawMessage(`{"big_int": 9007199254740993, "model": "probe", "nested": {"key": "val"}}`)
		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "downstream_complex_tool",
			Arguments: rawInput,
		})
		if err != nil {
			t.Fatalf("Two-hop CallTool failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("Expected IsError == false, got true")
		}

		// Assert side effect count exactly once
		if count := normalSideEffects.Load(); count != 1 {
			t.Fatalf("Expected normal side effect count == 1, got %d", count)
		}

		// Verify ImageContent bytes and MIMEType
		if len(res.Content) != 2 {
			t.Fatalf("Expected 2 Content elements, got %d", len(res.Content))
		}
		txt, ok := res.Content[0].(*mcp.TextContent)
		if !ok || txt.Text != "downstream normal text output" {
			t.Fatalf("Unexpected text content: %+v", res.Content[0])
		}
		img, ok := res.Content[1].(*mcp.ImageContent)
		if !ok {
			t.Fatalf("Expected *mcp.ImageContent, got %T: %+v", res.Content[1], res.Content[1])
		}
		if img.MIMEType != "image/png" {
			t.Errorf("Expected MIMEType image/png, got %s", img.MIMEType)
		}
		expectedImgBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0xAA, 0xBB}
		if !bytes.Equal(img.Data, expectedImgBytes) {
			t.Errorf("Image bytes mismatch: got %v, want %v", img.Data, expectedImgBytes)
		}

		// Verify StructuredContent
		sc, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("Expected map[string]any for StructuredContent, got %T", res.StructuredContent)
		}
		if sc["big_int_verified"] != "9007199254740993" || sc["status"] != "downstream_success" {
			t.Errorf("StructuredContent mismatch: %+v", sc)
		}
	})

	// 7b. Error call: isError preservation through both hops and side-effect count
	t.Run("ErrorCall_IsErrorPreservation", func(t *testing.T) {
		errInput := json.RawMessage(`{"simulate_error": true, "big_int": 9007199254740993}`)
		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "downstream_complex_tool",
			Arguments: errInput,
		})
		if err != nil {
			t.Fatalf("CallTool returned protocol error when IsError expected: %v", err)
		}
		if !res.IsError {
			t.Fatalf("Expected res.IsError == true, got false")
		}
		if len(res.Content) == 0 {
			t.Fatalf("Expected error content")
		}
		txt, ok := res.Content[0].(*mcp.TextContent)
		if !ok || txt.Text != "downstream error occurred" {
			t.Errorf("Unexpected error text: %+v", res.Content[0])
		}
		if count := errorSideEffects.Load(); count != 1 {
			t.Fatalf("Expected error side effect count == 1, got %d", count)
		}
	})

	// 7c. Two-hop cancellation: Client cancels -> Hub cancels -> Downstream observes ctx.Done()
	t.Run("TwoHop_Cancellation", func(t *testing.T) {
		callCtx, callCancel := context.WithCancel(ctx)
		callErrCh := make(chan error, 1)

		go func() {
			_, err := clientSession.CallTool(callCtx, &mcp.CallToolParams{
				Name: "downstream_slow_tool",
			})
			callErrCh <- err
		}()

		// Wait until downstream tool has actually begun executing
		select {
		case <-downstreamStarted:
		case <-time.After(3 * time.Second):
			t.Fatal("downstream slow tool did not start in time")
		}

		// Cancel at origin client
		callCancel()

		// Verify cancellation propagated through Hub into Downstream Server
		select {
		case <-downstreamCancelled:
			// Success: downstream observed cancellation via context
		case <-time.After(5 * time.Second):
			t.Fatal("cancellation did not propagate through both hops to downstream server")
		}

		// Assert downstream side-effect occurred exactly once
		if count := cancelSideEffects.Load(); count != 1 {
			t.Fatalf("Expected cancel side effect count == 1, got %d", count)
		}

		// Verify origin client call unblocked with error
		select {
		case err := <-callErrCh:
			if err == nil {
				t.Fatal("expected error from cancelled CallTool on origin client")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("origin client did not unblock after cancellation")
		}
	})
}

// 8. Chosen Production Concurrency Design:
// - publish sync.RWMutex
// - tools/list middleware takes RLock, calls next, releases RLock
// - Publisher modifies SDK tools (AddTool/RemoveTools) and routes under Lock
// - Tool calls acquire RLock, read route, and release lock BEFORE network call to downstream
type ProductionHub struct {
	mu     sync.RWMutex
	server *mcp.Server
	routes map[string]*mcp.ClientSession
}

func NewProductionHub(server *mcp.Server) *ProductionHub {
	hub := &ProductionHub{
		server: server,
		routes: make(map[string]*mcp.ClientSession),
	}

	// tools/list middleware takes RLock and calls next
	hub.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				hub.mu.RLock()
				defer hub.mu.RUnlock()
				return next(ctx, method, req)
			}
			return next(ctx, method, req)
		}
	})

	return hub
}

func (h *ProductionHub) Publish(toAdd []*mcp.Tool, target *mcp.ClientSession, toRemove []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, t := range toAdd {
		h.routes[t.Name] = target
		h.server.AddTool(t, h.dispatchTool)
	}
	for _, name := range toRemove {
		delete(h.routes, name)
		h.server.RemoveTools(name)
	}
}

func (h *ProductionHub) dispatchTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var target *mcp.ClientSession
	h.mu.RLock()
	target = h.routes[req.Params.Name]
	h.mu.RUnlock() // CRITICAL: Release lock before downstream network call!

	if target == nil {
		return nil, fmt.Errorf("tool route %q not found", req.Params.Name)
	}

	return target.CallTool(ctx, &mcp.CallToolParams{
		Name:      req.Params.Name,
		Arguments: req.Params.Arguments,
	})
}

func TestProductionPublishRWMutexPattern(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Mock downstream server
	downstreamServer := mcp.NewServer(serverImpl("downstream"), nil)
	toolBlockCh := make(chan struct{})
	downstreamServer.AddTool(&mcp.Tool{
		Name:        "slow_call",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-toolBlockCh
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "slow_ok"}},
		}, nil
	})
	downstreamServer.AddTool(&mcp.Tool{
		Name:        "fast_call",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "fast_ok"}},
		}, nil
	})

	c1, s1 := mcp.NewInMemoryTransports()
	dss, err := downstreamServer.Connect(ctx, s1, nil)
	if err != nil {
		t.Fatalf("downstream connect: %v", err)
	}
	defer dss.Close()

	hubDownstreamClient := mcp.NewClient(clientImpl("hub-ds-client"), nil)
	hubDownstreamSession, err := hubDownstreamClient.Connect(ctx, c1, nil)
	if err != nil {
		t.Fatalf("hub downstream connect: %v", err)
	}
	defer hubDownstreamSession.Close()

	// Initialize Production Hub
	hubServer := mcp.NewServer(serverImpl("hub-prod"), nil)
	hub := NewProductionHub(hubServer)

	// Publish initial tools
	hub.Publish([]*mcp.Tool{
		{Name: "slow_call", InputSchema: map[string]any{"type": "object"}},
		{Name: "fast_call", InputSchema: map[string]any{"type": "object"}},
	}, hubDownstreamSession, nil)

	// Connect Client
	c2, s2 := mcp.NewInMemoryTransports()
	hss, err := hubServer.Connect(ctx, s2, nil)
	if err != nil {
		t.Fatalf("hub server connect: %v", err)
	}
	defer hss.Close()

	client := mcp.NewClient(clientImpl("client-prod"), nil)
	cs, err := client.Connect(ctx, c2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 8a. Verify non-blocking network calls: In-flight tool call must NOT block Publish or tools/list!
	slowCallReturned := make(chan struct{})
	go func() {
		_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "slow_call"})
		close(slowCallReturned)
	}()

	// Give time for slow_call to enter downstream
	time.Sleep(50 * time.Millisecond)

	// While slow_call is blocked in downstream network call, Publish must acquire Lock and succeed immediately!
	publishDone := make(chan struct{})
	go func() {
		hub.Publish([]*mcp.Tool{
			{Name: "dynamic_added", InputSchema: map[string]any{"type": "object"}},
		}, hubDownstreamSession, nil)
		close(publishDone)
	}()

	select {
	case <-publishDone:
		// Succeeded! Proves Lock was NOT blocked by in-flight slow_call!
	case <-time.After(1 * time.Second):
		t.Fatal("Publish was blocked by in-flight tool call! Lock was held across network!")
	}

	// While slow_call is still blocked, tools/list must acquire RLock and succeed!
	listDone := make(chan struct{})
	go func() {
		listRes, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Errorf("ListTools failed: %v", err)
			return
		}
		if len(listRes.Tools) != 3 {
			t.Errorf("Expected 3 tools, got %d", len(listRes.Tools))
		}
		close(listDone)
	}()

	select {
	case <-listDone:
		// Succeeded! Proves tools/list was NOT blocked by in-flight slow_call!
	case <-time.After(1 * time.Second):
		t.Fatal("tools/list was blocked by in-flight tool call!")
	}

	// Unblock the slow tool call
	close(toolBlockCh)
	select {
	case <-slowCallReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("slow call did not complete after unblocking")
	}

	// 8b. Dynamic removal: Remove dynamic_added under Lock
	hub.Publish(nil, nil, []string{"dynamic_added"})
	listAfterRemove, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools after remove failed: %v", err)
	}
	if len(listAfterRemove.Tools) != 2 {
		t.Fatalf("Expected 2 tools after remove, got %d", len(listAfterRemove.Tools))
	}

	// 8c. Stress concurrency with -race: concurrent ListTools, Publish, and CallTool
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 20; k++ {
				_, _ = cs.ListTools(ctx, nil)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 20; k++ {
				_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "fast_call"})
			}
		}()

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			toolName := fmt.Sprintf("stress_tool_%d", idx)
			for k := 0; k < 10; k++ {
				hub.Publish([]*mcp.Tool{
					{Name: toolName, InputSchema: map[string]any{"type": "object"}},
				}, hubDownstreamSession, nil)
				hub.Publish(nil, nil, []string{toolName})
			}
		}(i)
	}
	wg.Wait()
}
