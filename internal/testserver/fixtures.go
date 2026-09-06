package testserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Standard fixture tool names specified in Agent实施手册 T03.
const (
	ToolEchoRaw = "echo_raw"
	ToolImage   = "image"
	ToolFail    = "fail_tool"
	ToolBlock   = "block"
	ToolCounter = "counter"
	ToolCrash   = "crash"
)

// DefaultPNG is a deterministic 1x1 RGBA PNG byte sequence.
var DefaultPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG magic
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, // 8-bit RGBA
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, // IDAT chunk
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, // IEND chunk
	0x42, 0x60, 0x82,
}

// normalizeRawArgs decodes base64-encoded strings if the wire transport marshaled
// raw []byte arguments into a JSON base64 string, or returns literal JSON arguments as-is.
func normalizeRawArgs(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	// If raw is wrapped in JSON quotes (e.g. "eyJiaWdf..."), unmarshal the JSON string first.
	// This occurs when callers pass []byte into CallToolParams.Arguments (type any),
	// which json.Marshal serializes as a base64 string on the wire.
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			if decoded, err := base64.StdEncoding.DecodeString(str); err == nil && len(decoded) > 0 {
				return decoded
			}
			return []byte(str)
		}
	}
	return raw
}

// EchoRawController manages the echo_raw fixture.
// It captures raw JSON arguments without float64 precision loss (e.g. big integers)
// and returns both plain text and structured output.
type EchoRawController struct {
	mu        sync.RWMutex
	lastArgs  []byte
	callCount atomic.Int64
}

// NewEchoRawController creates a new EchoRawController.
func NewEchoRawController() *EchoRawController {
	return &EchoRawController{}
}

// Tool returns the tool specification for echo_raw.
func (c *EchoRawController) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ToolEchoRaw,
		Description: "Echo raw JSON arguments preserving structure and large integers",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Handler handles echo_raw calls.
func (c *EchoRawController) Handler(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.callCount.Add(1)
	raw := normalizeRawArgs([]byte(req.Params.Arguments))

	c.mu.Lock()
	c.lastArgs = append([]byte(nil), raw...)
	c.mu.Unlock()

	var structured any
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err == nil {
			structured = v
		}
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(raw)},
		},
	}
	if structured != nil {
		result.StructuredContent = structured
	}
	return result, nil
}

// LastRawArgs returns a copy of the most recently received raw arguments.
func (c *EchoRawController) LastRawArgs() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]byte(nil), c.lastArgs...)
}

// CallCount returns the total number of calls to echo_raw.
func (c *EchoRawController) CallCount() int64 {
	return c.callCount.Load()
}

// Reset clears the recorded raw arguments and resets call count.
func (c *EchoRawController) Reset() {
	c.mu.Lock()
	c.lastArgs = nil
	c.callCount.Store(0)
	c.mu.Unlock()
}

// ImageController manages the image fixture.
// It returns deterministic image bytes and MIME type.
type ImageController struct {
	mu        sync.RWMutex
	data      []byte
	mimeType  string
	callCount atomic.Int64
}

// NewImageController creates a new ImageController.
func NewImageController() *ImageController {
	return &ImageController{
		data:     append([]byte(nil), DefaultPNG...),
		mimeType: "image/png",
	}
}

// Tool returns the tool specification for image.
func (c *ImageController) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ToolImage,
		Description: "Return deterministic image bytes and MIME type",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Handler handles image calls.
func (c *ImageController) Handler(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.callCount.Add(1)
	c.mu.RLock()
	dataCopy := append([]byte(nil), c.data...)
	mime := c.mimeType
	c.mu.RUnlock()

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "image generated"},
			&mcp.ImageContent{
				Data:     dataCopy,
				MIMEType: mime,
			},
		},
	}, nil
}

// SetImage updates the image data and MIME type returned by the fixture.
func (c *ImageController) SetImage(data []byte, mimeType string) {
	c.mu.Lock()
	c.data = append([]byte(nil), data...)
	c.mimeType = mimeType
	c.mu.Unlock()
}

// Data returns a copy of the currently configured image bytes.
func (c *ImageController) Data() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]byte(nil), c.data...)
}

// MIMEType returns the currently configured MIME type.
func (c *ImageController) MIMEType() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mimeType
}

// CallCount returns the total number of calls to image.
func (c *ImageController) CallCount() int64 {
	return c.callCount.Load()
}

// Reset restores default PNG bytes and MIME type, and resets call count.
func (c *ImageController) Reset() {
	c.mu.Lock()
	c.data = append([]byte(nil), DefaultPNG...)
	c.mimeType = "image/png"
	c.callCount.Store(0)
	c.mu.Unlock()
}

// FailToolController manages the fail_tool fixture.
// By default it returns CallToolResult with IsError=true.
// It can also be configured to return a raw Go error.
type FailToolController struct {
	mu          sync.RWMutex
	message     string
	returnError bool
	callCount   atomic.Int64
}

// NewFailToolController creates a new FailToolController.
func NewFailToolController() *FailToolController {
	return &FailToolController{
		message: "simulated tool failure",
	}
}

// Tool returns the tool specification for fail_tool.
func (c *FailToolController) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ToolFail,
		Description: "Simulate tool execution failure returning isError=true",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Handler handles fail_tool calls.
func (c *FailToolController) Handler(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.callCount.Add(1)
	c.mu.RLock()
	msg := c.message
	retErr := c.returnError
	c.mu.RUnlock()

	if retErr {
		return nil, errors.New(msg)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}, nil
}

// SetErrorMessage configures the error message string.
func (c *FailToolController) SetErrorMessage(msg string) {
	c.mu.Lock()
	c.message = msg
	c.mu.Unlock()
}

// SetReturnGoError sets whether to return a Go error instead of IsError=true.
func (c *FailToolController) SetReturnGoError(val bool) {
	c.mu.Lock()
	c.returnError = val
	c.mu.Unlock()
}

// CallCount returns the total number of calls to fail_tool.
func (c *FailToolController) CallCount() int64 {
	return c.callCount.Load()
}

// Reset restores default failure message and resets call count.
func (c *FailToolController) Reset() {
	c.mu.Lock()
	c.message = "simulated tool failure"
	c.returnError = false
	c.callCount.Store(0)
	c.mu.Unlock()
}

// BlockController manages the block fixture.
// It signals an enter notification and waits until context cancellation or explicit unblocking.
type BlockController struct {
	mu         sync.Mutex
	enteredCh  chan struct{}
	unblockCh  chan struct{}
	enterCount atomic.Int64
}

// NewBlockController creates a new BlockController.
func NewBlockController() *BlockController {
	return &BlockController{
		enteredCh: make(chan struct{}, 64),
		unblockCh: make(chan struct{}),
	}
}

// Tool returns the tool specification for block.
func (c *BlockController) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ToolBlock,
		Description: "Blocks until context is cancelled or explicitly unblocked",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Handler handles block calls.
func (c *BlockController) Handler(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.enterCount.Add(1)

	// Notify test watcher that handler has been entered
	select {
	case c.enteredCh <- struct{}{}:
	default:
	}

	c.mu.Lock()
	unblock := c.unblockCh
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-unblock:
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "unblocked"},
			},
		}, nil
	}
}

// WaitForEnter waits until the block handler has been entered or timeout expires.
func (c *BlockController) WaitForEnter(timeout time.Duration) error {
	select {
	case <-c.enteredCh:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout waiting for block handler enter notification")
	}
}

// Unblock releases any blocked calls.
func (c *BlockController) Unblock() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.unblockCh:
		// already unblocked
	default:
		close(c.unblockCh)
	}
}

// EnterCount returns the total number of times the block handler was entered.
func (c *BlockController) EnterCount() int64 {
	return c.enterCount.Load()
}

// Reset resets the enter channel, unblock channel, and enter count.
func (c *BlockController) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enteredCh = make(chan struct{}, 64)
	c.unblockCh = make(chan struct{})
	c.enterCount.Store(0)
}

// CounterController manages the counter fixture.
// It atomically increments a counter on each call to detect replays.
type CounterController struct {
	count atomic.Int64
}

// NewCounterController creates a new CounterController.
func NewCounterController() *CounterController {
	return &CounterController{}
}

// Tool returns the tool specification for counter.
func (c *CounterController) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ToolCounter,
		Description: "Increments atomic counter on each call to verify non-replay",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Handler handles counter calls.
func (c *CounterController) Handler(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	val := c.count.Add(1)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("count: %d", val)},
		},
		StructuredContent: map[string]any{
			"count": val,
		},
	}, nil
}

// Value returns the current counter value.
func (c *CounterController) Value() int64 {
	return c.count.Load()
}

// Add adds delta to the counter.
func (c *CounterController) Add(delta int64) int64 {
	return c.count.Add(delta)
}

// Reset resets the counter value to zero.
func (c *CounterController) Reset() {
	c.count.Store(0)
}

// PagedController manages multi-page tool listing, dynamic catalog switching,
// and repeat-cursor fault simulation.
type PagedController struct {
	mu                sync.RWMutex
	serverRef         *Server
	tools             []*mcp.Tool
	pageSize          int
	interceptEnabled  bool
	repeatCursorMode  bool
	repeatCursorToken string
	listCalls         atomic.Int64
}

// NewPagedController creates a new PagedController with default 5 paged tools.
func NewPagedController(pageSize int) *PagedController {
	if pageSize <= 0 {
		pageSize = 2
	}
	return &PagedController{
		tools:             defaultPagedTools(),
		pageSize:          pageSize,
		interceptEnabled:  true,
		repeatCursorToken: "repeat_cursor_fault",
	}
}

func defaultPagedTools() []*mcp.Tool {
	var tools []*mcp.Tool
	for i := 1; i <= 5; i++ {
		tools = append(tools, &mcp.Tool{
			Name:        fmt.Sprintf("paged_tool_%d", i),
			Description: fmt.Sprintf("Paged tool fixture item %d", i),
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
	}
	return tools
}

func defaultPagedToolHandler(name string) mcp.ToolHandler {
	return func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("%s executed successfully", name)},
			},
		}, nil
	}
}

// Tools returns a slice of currently configured paged tools.
func (c *PagedController) Tools() []*mcp.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	res := make([]*mcp.Tool, len(c.tools))
	copy(res, c.tools)
	return res
}

// SetTools updates the tools slice for pagination.
func (c *PagedController) SetTools(tools []*mcp.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = make([]*mcp.Tool, len(tools))
	copy(c.tools, tools)
}

// SwitchCatalog switches the active catalog tools dynamically and updates the SDK server,
// which automatically broadcasts notifications/tools/list_changed to active sessions.
func (c *PagedController) SwitchCatalog(newTools []*mcp.Tool) {
	c.mu.Lock()
	oldTools := c.tools
	c.tools = make([]*mcp.Tool, len(newTools))
	copy(c.tools, newTools)
	server := c.serverRef
	c.mu.Unlock()

	if server != nil && server.mcpServer != nil {
		var oldNames []string
		for _, t := range oldTools {
			oldNames = append(oldNames, t.Name)
		}
		if len(oldNames) > 0 {
			server.mcpServer.RemoveTools(oldNames...)
		}
		for _, t := range newTools {
			server.mcpServer.AddTool(t, defaultPagedToolHandler(t.Name))
		}
	}
}

// SetRepeatCursorMode enables or disables repeat-cursor fault simulation.
// When enabled, tools/list repeatedly returns repeatCursorToken to test client loop detection.
func (c *PagedController) SetRepeatCursorMode(enabled bool) {
	c.mu.Lock()
	c.repeatCursorMode = enabled
	c.mu.Unlock()
}

// RepeatCursorMode reports whether repeat-cursor fault simulation is active.
func (c *PagedController) RepeatCursorMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.repeatCursorMode
}

// SetRepeatCursorToken sets the token returned during repeat-cursor fault simulation.
func (c *PagedController) SetRepeatCursorToken(token string) {
	c.mu.Lock()
	c.repeatCursorToken = token
	c.mu.Unlock()
}

// SetInterceptEnabled controls whether middleware intercepts tools/list.
func (c *PagedController) SetInterceptEnabled(enabled bool) {
	c.mu.Lock()
	c.interceptEnabled = enabled
	c.mu.Unlock()
}

// IsInterceptEnabled reports whether tools/list interception is enabled.
func (c *PagedController) IsInterceptEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.interceptEnabled
}

// ListCalls returns the total number of tools/list calls intercepted.
func (c *PagedController) ListCalls() int64 {
	return c.listCalls.Load()
}

// HandleListTools handles tools/list pagination requests.
func (c *PagedController) HandleListTools(_ context.Context, req mcp.Request) (mcp.Result, error) {
	c.listCalls.Add(1)
	c.mu.RLock()
	defer c.mu.RUnlock()

	cursor := ""
	if listReq, ok := req.(*mcp.ListToolsRequest); ok && listReq.Params != nil {
		cursor = listReq.Params.Cursor
	}

	if c.repeatCursorMode {
		// Return duplicate cursor to test client loop detection
		var subset []*mcp.Tool
		if len(c.tools) > 0 {
			subset = []*mcp.Tool{c.tools[0]}
		}
		return &mcp.ListToolsResult{
			NextCursor: c.repeatCursorToken,
			Tools:      subset,
		}, nil
	}

	total := len(c.tools)
	offset := 0
	if cursor != "" {
		n, err := fmt.Sscanf(cursor, "cursor_offset_%d", &offset)
		if err != nil || n != 1 || offset < 0 || offset > total {
			return nil, fmt.Errorf("invalid cursor token %q", cursor)
		}
	}

	end := offset + c.pageSize
	if end > total {
		end = total
	}

	resTools := make([]*mcp.Tool, end-offset)
	copy(resTools, c.tools[offset:end])

	nextCursor := ""
	if end < total {
		nextCursor = fmt.Sprintf("cursor_offset_%d", end)
	}

	return &mcp.ListToolsResult{
		NextCursor: nextCursor,
		Tools:      resTools,
	}, nil
}

// Reset restores default tools and settings.
func (c *PagedController) Reset() {
	c.mu.Lock()
	c.tools = defaultPagedTools()
	c.repeatCursorMode = false
	c.repeatCursorToken = "repeat_cursor_fault"
	c.interceptEnabled = true
	c.listCalls.Store(0)
	c.mu.Unlock()
}

// CrashController manages crash simulation.
// In-process Go tests cannot call os.Exit() without terminating the test runner.
// The safe crash fixture terminates active server sessions and transports to trigger
// client-side disconnection (EOF/closed connection) during or before a tool call.
type CrashController struct {
	mu          sync.Mutex
	serverRef   *Server
	crashOnCall bool
	crashed     atomic.Bool
	callCount   atomic.Int64
}

// NewCrashController creates a new CrashController.
func NewCrashController() *CrashController {
	return &CrashController{
		crashOnCall: true,
	}
}

// Tool returns the tool specification for crash.
func (c *CrashController) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ToolCrash,
		Description: "Simulate downstream crash / abrupt connection drop",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Handler handles crash tool calls.
func (c *CrashController) Handler(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.callCount.Add(1)
	if c.CrashOnCall() {
		c.crashed.Store(true)
		if c.serverRef != nil {
			go c.serverRef.closeAllSessions()
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "simulated downstream crash: session terminating"},
			},
			IsError: true,
		}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "crash tool called without crash trigger"},
		},
	}, nil
}

// SetCrashOnCall configures whether invoking the crash tool immediately terminates connections.
func (c *CrashController) SetCrashOnCall(val bool) {
	c.mu.Lock()
	c.crashOnCall = val
	c.mu.Unlock()
}

// CrashOnCall reports whether crash is triggered on call.
func (c *CrashController) CrashOnCall() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.crashOnCall
}

// TriggerCrash abruptly marks the crash state and terminates all active server sessions.
// Subsequent requests to the server will immediately fail with an error.
func (c *CrashController) TriggerCrash() {
	c.crashed.Store(true)
	if c.serverRef != nil {
		c.serverRef.closeAllSessions()
	}
}

// HasCrashed reports whether a crash was triggered.
func (c *CrashController) HasCrashed() bool {
	return c.crashed.Load()
}

// CallCount returns the total number of calls to crash.
func (c *CrashController) CallCount() int64 {
	return c.callCount.Load()
}

// Reset resets crash state and call count.
func (c *CrashController) Reset() {
	c.crashed.Store(false)
	c.callCount.Store(0)
}
