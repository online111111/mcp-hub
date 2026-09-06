package testserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func clientImpl(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:    name,
		Version: "1.0.0",
	}
}

// 1. Echo Raw: verifies raw JSON retention, large integer preservation, and structured output.
func TestEchoRawFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts := New()
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	// 1a. Call with []byte arguments: verifies that base64 wire-encoded bytes are
	// decoded back to pre-wire raw JSON containing 9007199254740993 without precision loss.
	byteArgs := []byte(`{"big_int":9007199254740993,"title":"preserve-exact-json"}`)
	res1, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolEchoRaw,
		Arguments: byteArgs,
	})
	if err != nil {
		t.Fatalf("CallTool echo_raw with []byte failed: %v", err)
	}

	if ts.EchoRaw.CallCount() != 1 {
		t.Fatalf("expected 1 call, got %d", ts.EchoRaw.CallCount())
	}

	lastArgs1 := ts.EchoRaw.LastRawArgs()
	if !bytes.Contains(lastArgs1, []byte("9007199254740993")) {
		t.Fatalf("raw args lost big int with []byte: %s", string(lastArgs1))
	}

	if len(res1.Content) == 0 {
		t.Fatalf("expected content, got empty")
	}
	tc1, ok := res1.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res1.Content[0])
	}
	if !bytes.Contains([]byte(tc1.Text), []byte("9007199254740993")) {
		t.Fatalf("text content does not contain big int: %s", tc1.Text)
	}

	// Verify structured content contains the decoded JSON
	scMap1, ok := res1.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T: %+v", res1.StructuredContent, res1.StructuredContent)
	}
	if num, ok := scMap1["big_int"].(json.Number); ok {
		if num.String() != "9007199254740993" {
			t.Fatalf("expected 9007199254740993, got %s", num.String())
		}
	} else if str, ok := scMap1["big_int"].(string); ok {
		if str != "9007199254740993" {
			t.Fatalf("expected 9007199254740993, got %s", str)
		}
	}

	// 1b. Call with json.RawMessage arguments: verifies direct raw JSON works identically
	rawMsg := json.RawMessage(`{"big_int":9007199254740993,"title":"preserve-exact-json-raw"}`)
	res2, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolEchoRaw,
		Arguments: rawMsg,
	})
	if err != nil {
		t.Fatalf("CallTool echo_raw with json.RawMessage failed: %v", err)
	}
	lastArgs2 := ts.EchoRaw.LastRawArgs()
	if !bytes.Contains(lastArgs2, []byte("9007199254740993")) {
		t.Fatalf("raw args lost big int with json.RawMessage: %s", string(lastArgs2))
	}
	if len(res2.Content) == 0 {
		t.Fatalf("expected content in res2")
	}
}

// 2. Image Fixture: verifies deterministic PNG bytes and MIME type.
func TestImageFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts := New()
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	// Default image call
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolImage})
	if err != nil {
		t.Fatalf("CallTool image failed: %v", err)
	}
	if len(res.Content) < 2 {
		t.Fatalf("expected at least 2 content blocks, got %d", len(res.Content))
	}
	imgContent, ok := res.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("expected ImageContent at index 1, got %T", res.Content[1])
	}
	if imgContent.MIMEType != "image/png" {
		t.Fatalf("expected MIMEType image/png, got %s", imgContent.MIMEType)
	}
	if !bytes.Equal(imgContent.Data, DefaultPNG) {
		t.Fatalf("image data mismatch")
	}

	// Update image and verify custom payload
	customData := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	ts.Image.SetImage(customData, "image/webp")

	res2, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolImage})
	if err != nil {
		t.Fatalf("CallTool image custom failed: %v", err)
	}
	imgContent2, ok := res2.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("expected ImageContent, got %T", res2.Content[1])
	}
	if imgContent2.MIMEType != "image/webp" {
		t.Fatalf("expected MIMEType image/webp, got %s", imgContent2.MIMEType)
	}
	if !bytes.Equal(imgContent2.Data, customData) {
		t.Fatalf("custom image data mismatch")
	}
}

// 3. Fail Tool: verifies IsError=true and configurable error modes.
func TestFailToolFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts := New()
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	// Default fail_tool returns IsError = true
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolFail})
	if err != nil {
		t.Fatalf("CallTool fail_tool unexpected RPC error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true, got false")
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected content in fail result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "simulated tool failure" {
		t.Fatalf("unexpected fail text: %+v", res.Content[0])
	}

	// Configured to return Go error
	ts.FailTool.SetReturnGoError(true)
	ts.FailTool.SetErrorMessage("fatal downstream fault")
	_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolFail})
	if err == nil {
		t.Fatalf("expected RPC error when ReturnGoError is true, got nil")
	}
}

// 4. Block / Cancel: verifies enter notification and context cancellation unblocking.
func TestBlockCancelFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts := New()
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	// 4a. Cancellation path
	callCtx, callCancel := context.WithCancel(ctx)
	callErrCh := make(chan error, 1)

	go func() {
		_, err := clientSession.CallTool(callCtx, &mcp.CallToolParams{Name: ToolBlock})
		callErrCh <- err
	}()

	// Wait for handler to be entered
	if err := ts.Block.WaitForEnter(2 * time.Second); err != nil {
		t.Fatalf("failed waiting for block handler: %v", err)
	}

	// Cancel context while blocked
	callCancel()

	select {
	case err := <-callErrCh:
		if err == nil {
			t.Fatalf("expected error after context cancellation, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("CallTool did not unblock after cancellation")
	}

	if ts.Block.EnterCount() != 1 {
		t.Fatalf("expected EnterCount=1, got %d", ts.Block.EnterCount())
	}

	// 4b. Explicit unblock path
	ts.Block.Reset()
	go func() {
		_, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolBlock})
		callErrCh <- err
	}()

	if err := ts.Block.WaitForEnter(2 * time.Second); err != nil {
		t.Fatalf("failed waiting for block handler: %v", err)
	}

	// Explicitly unblock
	ts.Block.Unblock()

	select {
	case err := <-callErrCh:
		if err != nil {
			t.Fatalf("expected success after unblock, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("CallTool did not unblock after Unblock()")
	}
}

// 5. Counter: verifies atomic increment for replay detection.
func TestCounterFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts := New()
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	for i := 1; i <= 3; i++ {
		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolCounter})
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		if ts.Counter.Value() != int64(i) {
			t.Fatalf("expected counter %d, got %d", i, ts.Counter.Value())
		}
		tc, ok := res.Content[0].(*mcp.TextContent)
		if !ok || tc.Text != fmt.Sprintf("count: %d", i) {
			t.Fatalf("unexpected content at call %d: %+v", i, res.Content[0])
		}
	}

	ts.Counter.Reset()
	if ts.Counter.Value() != 0 {
		t.Fatalf("expected counter 0 after reset, got %d", ts.Counter.Value())
	}
}

// 6. Paged Tools: verifies multi-page pagination, repeat-cursor fault, and dynamic catalog switching.
func TestPagedToolsFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts := New(WithPageSize(2))
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	// 6a. Multi-page traversal
	var allTools []*mcp.Tool
	cursor := ""
	pageCount := 0

	for {
		pageCount++
		params := &mcp.ListToolsParams{Cursor: cursor}
		res, err := clientSession.ListTools(ctx, params)
		if err != nil {
			t.Fatalf("ListTools page %d failed: %v", pageCount, err)
		}
		allTools = append(allTools, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}

	if pageCount != 3 {
		t.Fatalf("expected 3 pages for 5 tools with pageSize 2, got %d", pageCount)
	}
	if len(allTools) != 5 {
		t.Fatalf("expected 5 tools total, got %d", len(allTools))
	}

	// 6b. Repeat cursor fault mode
	ts.Paged.SetRepeatCursorMode(true)
	resFault1, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools fault call 1 failed: %v", err)
	}
	if resFault1.NextCursor != "repeat_cursor_fault" {
		t.Fatalf("expected repeat_cursor_fault cursor, got %q", resFault1.NextCursor)
	}

	resFault2, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{Cursor: resFault1.NextCursor})
	if err != nil {
		t.Fatalf("ListTools fault call 2 failed: %v", err)
	}
	if resFault2.NextCursor != "repeat_cursor_fault" {
		t.Fatalf("expected repeat_cursor_fault repeated, got %q", resFault2.NextCursor)
	}
	ts.Paged.SetRepeatCursorMode(false)

	// 6c. Dynamic catalog switching and notification
	notifyCh := make(chan struct{}, 1)
	clientSession2, cleanup2, err := ts.ConnectClient(ctx, &mcp.ClientOptions{
		ToolListChangedHandler: func(_ context.Context, _ *mcp.ToolListChangedRequest) {
			select {
			case notifyCh <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("ConnectClient 2 failed: %v", err)
	}
	defer cleanup2()

	newTools := []*mcp.Tool{
		{
			Name:        "custom_tool_alpha",
			Description: "Custom tool alpha description",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "custom_tool_beta",
			Description: "Custom tool beta description",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	ts.Paged.SwitchCatalog(newTools)

	select {
	case <-notifyCh:
		// Succeeded: ToolListChangedHandler received notification!
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for ToolListChangedHandler notification after SwitchCatalog")
	}

	resAfterSwitch, err := clientSession2.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools after switch failed: %v", err)
	}
	if len(resAfterSwitch.Tools) != 2 {
		t.Fatalf("expected 2 tools after switch, got %d", len(resAfterSwitch.Tools))
	}
	if resAfterSwitch.Tools[0].Name != "custom_tool_alpha" || resAfterSwitch.Tools[1].Name != "custom_tool_beta" {
		t.Fatalf("unexpected tools after switch: %+v", resAfterSwitch.Tools)
	}

	// 6d. Invalid cursor error handling
	_, err = clientSession2.ListTools(ctx, &mcp.ListToolsParams{Cursor: "invalid_cursor_xyz"})
	if err == nil {
		t.Fatalf("expected error for invalid cursor, got nil")
	}
}

// 7. Crash: verifies safe in-memory session termination on TriggerCrash and crash tool invocation.
func TestCrashFixture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 7a. Direct TriggerCrash: verifies subsequent calls fail immediately
	t.Run("TriggerCrash_Direct", func(t *testing.T) {
		ts := New()
		defer ts.Close()

		clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
		if err != nil {
			t.Fatalf("ConnectClient failed: %v", err)
		}
		defer cleanup()

		ts.Crash.TriggerCrash()
		if !ts.Crash.HasCrashed() {
			t.Fatalf("expected HasCrashed=true after TriggerCrash")
		}

		// Subsequent calls must fail immediately
		_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolCounter})
		if err == nil {
			t.Fatalf("expected CallTool error after TriggerCrash, got nil")
		}
	})

	// 7b. Invoking crash tool via RPC
	t.Run("CrashTool_Invocation", func(t *testing.T) {
		ts := New()
		defer ts.Close()

		clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
		if err != nil {
			t.Fatalf("ConnectClient failed: %v", err)
		}
		defer cleanup()

		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolCrash})
		if err != nil {
			// Connection closed during call is acceptable
		} else if !res.IsError {
			t.Fatalf("expected IsError=true or error from crash tool, got success")
		}

		if !ts.Crash.HasCrashed() {
			t.Fatalf("expected HasCrashed=true")
		}

		// Subsequent calls on the crashed session must fail
		_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolCounter})
		if err == nil {
			t.Fatalf("expected call on crashed session to fail, got nil")
		}
	})
}

// 8. Exclusive: verifies local port binding and collision detection.
func TestExclusivePortFixture(t *testing.T) {
	listener, err := AcquireExclusivePort(0)
	if err != nil {
		t.Fatalf("AcquireExclusivePort failed: %v", err)
	}
	defer listener.Close()

	if listener.Port() <= 0 {
		t.Fatalf("invalid listener port: %d", listener.Port())
	}

	// Verify collision detection (secondary bind to same address should fail)
	if err := listener.CheckCollision(); err != nil {
		t.Fatalf("CheckCollision failed: %v", err)
	}

	// Close listener and verify port is released
	port := listener.Port()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Now binding to the same port should succeed
	ln2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to re-bind port %d after release: %v", port, err)
	}
	_ = ln2.Close()
}

// 9. Transports: tests HTTP, IOTransport, and custom tools.
func TestTransportsAndCustomTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 9a. Custom Tool
	ts := New(WithCustomTool(&mcp.Tool{
		Name:        "my_custom_tool",
		Description: "Custom test tool description",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "custom-ok"}},
		}, nil
	}))
	defer ts.Close()

	// 9b. Streamable HTTP
	httpURL, httpCleanup, err := ts.StartHTTP()
	if err != nil {
		t.Fatalf("StartHTTP failed: %v", err)
	}
	defer httpCleanup()

	httpClient := mcp.NewClient(clientImpl("http-client"), nil)
	httpSession, err := httpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpURL}, nil)
	if err != nil {
		t.Fatalf("HTTP client connect failed: %v", err)
	}
	defer httpSession.Close()

	res, err := httpSession.CallTool(ctx, &mcp.CallToolParams{Name: "my_custom_tool"})
	if err != nil {
		t.Fatalf("HTTP CallTool custom tool failed: %v", err)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "custom-ok" {
		t.Fatalf("unexpected HTTP tool result: %+v", res.Content[0])
	}

	// 9c. IO Transport (stdio pipe simulation)
	ioClientTransport, ioCleanup, err := ts.NewIOClientTransport(ctx)
	if err != nil {
		t.Fatalf("NewIOClientTransport failed: %v", err)
	}
	defer ioCleanup()

	ioClient := mcp.NewClient(clientImpl("io-client"), nil)
	ioSession, err := ioClient.Connect(ctx, ioClientTransport, nil)
	if err != nil {
		t.Fatalf("IO client connect failed: %v", err)
	}
	defer ioSession.Close()

	resIO, err := ioSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolCounter})
	if err != nil {
		t.Fatalf("IO CallTool counter failed: %v", err)
	}
	if len(resIO.Content) == 0 {
		t.Fatalf("expected content over IOTransport")
	}
}

// 10. Lifecycle, ResetAll, and Option flags
func TestServerLifecycleAndOptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 10a. WithoutDefaultFixtures
	emptyTS := New(WithoutDefaultFixtures())
	defer emptyTS.Close()

	emptyClient, emptyCleanup, err := emptyTS.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer emptyCleanup()

	// Calling undefined fixture should fail
	_, err = emptyClient.CallTool(ctx, &mcp.CallToolParams{Name: ToolEchoRaw})
	if err == nil {
		t.Fatalf("expected error calling unregistered fixture, got nil")
	}

	// 10b. Calling paged tool directly
	ts := New(WithName("custom-named-server"), WithVersion("2.1.0"))
	defer ts.Close()

	clientSession, cleanup, err := ts.ConnectClient(ctx, nil)
	if err != nil {
		t.Fatalf("ConnectClient failed: %v", err)
	}
	defer cleanup()

	pagedRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "paged_tool_1"})
	if err != nil {
		t.Fatalf("CallTool paged_tool_1 failed: %v", err)
	}
	if len(pagedRes.Content) == 0 {
		t.Fatalf("expected content in paged_tool_1 result")
	}

	// 10c. ResetAll
	_, _ = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolCounter})
	_, _ = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolImage})
	_, _ = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolFail})

	if ts.Counter.Value() == 0 || ts.Image.CallCount() == 0 || ts.FailTool.CallCount() == 0 {
		t.Fatalf("expected non-zero call counts before reset")
	}

	ts.ResetAll()

	if ts.Counter.Value() != 0 || ts.Image.CallCount() != 0 || ts.FailTool.CallCount() != 0 {
		t.Fatalf("expected zero call counts after ResetAll")
	}
}
