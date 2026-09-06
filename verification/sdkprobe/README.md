# Go SDK Feasibility Probe Report

This probe verifies the feasibility and exact API semantics of `github.com/modelcontextprotocol/go-sdk` pinned at `v1.4.1` (using Go 1.26.3 on Windows amd64).

## Verification boundary and selected production design

The parent agent independently reran `go test -race -count=1 -timeout 90s ./...` on Windows amd64: PASS (`ok sdkprobe 3.652s`). These tests are feasibility probes, not a completed Hub.

Not verified here: Windows Job/worker process ownership, native subprocess stdio (IO fixtures use pipes), full stdio bridge lifecycle, actual Cursor/Claude Desktop versions, hot reload coordinators, security limits, and lost-response/SSE reconnect replay scenarios. The counter assertions cover only the exercised normal/error/cancel paths; they do not establish a general exactly-once delivery guarantee. Image tests verify bytes, not whether the synthetic byte fixture is a renderable PNG.

Production follows the RWMutex + SDK native registration design, not the alternative atomic snapshot interception test. Explicitly set `ClientOptions.Capabilities` to `&mcp.ClientCapabilities{}`: v1.4.1 defaults nil client capabilities to roots support, which the product does not implement. Pinning a version does not replace a release-time vulnerability check.

## 1. Executive Summary & Core Findings

| Capability Area | Feasibility | Exact Go SDK API / Mechanism |
|---|---|---|
| **Downstream Streamable HTTP** | **Supported** | `mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server, *mcp.StreamableHTTPOptions)` + `mcp.StreamableClientTransport{Endpoint: ...}` |
| **In-Memory Transport** | **Supported** | `mcp.NewInMemoryTransports()` (built on `net.Pipe()`) |
| **IO / Stdio Transport** | **Supported** | `mcp.IOTransport{Reader, Writer}` (or `mcp.StdioTransport{}`) |
| **Single-Hop Raw Tool Proxy** | **Supported** | `server.AddTool(tool, func(ctx, req *CallToolRequest) (*CallToolResult, error))` preserves raw `json.RawMessage` arguments and supports `Content`, `StructuredContent`, and `IsError`. |
| **Two-Hop Forwarding Chain** | **Supported** | Client -> Hub (`httptest.Server` Streamable HTTP) -> downstream `ClientSession` -> downstream Server. Transmits raw big int `9007199254740993`, `ImageContent` binary bytes, structured data, and `isError`. |
| **Two-Hop Cancellation** | **Supported** | Client context cancellation sends `notifications/cancelled` over HTTP to Hub; Hub's context cancellation sends `notifications/cancelled` to downstream server; downstream handler unblocks via `<-ctx.Done()`. |
| **Dynamic Tool Updates (`AddTool` & `RemoveTools`)** | **Supported** | `server.AddTool(...)` and `server.RemoveTools(names ...string)` (in `server.go:510`). Automatically notifies connected clients via `notifications/tools/list_changed` (10ms debounce). |
| **Multiple Clients Call Routing** | **Supported** | Multiple `ClientSession`s connect to the same `*Server`. `req.Session` points to each client's distinct `*ServerSession`. Under Streamable HTTP, `req.Session.ID()` contains the HTTP session ID. |
| **Production Publish RWMutex Concurrency** | **Supported** | Hub `publish sync.RWMutex`: `tools/list` middleware takes `RLock` and calls `next`; Publisher updates routes and SDK tools under `Lock`; tool calls release `RLock` before network invocation. |

---

## 2. Refutation of Erroneous Claims

### Claim: "The Go SDK lacks RemoveTools"
- **Status: FALSE.**
- **Code Inspection Fact**: `server.go` (line 510):
  ```go
  // RemoveTools removes the tools with the given names.
  // It is not an error to remove a nonexistent tool.
  func (s *Server) RemoveTools(names ...string) {
      s.changeAndNotify(notificationToolListChanged, func() bool { return s.tools.remove(names...) })
  }
  ```
  `RemoveTools` is a standard exported method of `*mcp.Server`. Calling it removes the tools and fires the debounced `notifications/tools/list_changed` notification to all active client sessions.

---

## 3. Test Suite Implementation & Structure

The probe test suite in `probe_test.go` clearly separates **Single-Hop Baseline Probes (Tests 1–6)** from the **Two-Hop Forwarding Chain (Test 7)** and the **Production RWMutex Concurrency Architecture (Test 8)**:

### Part A: Baseline Single-Hop Tests (Tests 1–6)
1. **`TestDownstreamTransports`**:
   - `StreamableHTTP`: Validates `httptest.Server` with `mcp.NewStreamableHTTPHandler` and `mcp.StreamableClientTransport`.
   - `InMemoryPipe`: Validates `mcp.NewInMemoryTransports()`.
   - `IOTransport_StdinStdoutFixture`: Validates `mcp.IOTransport` using dual `io.Pipe()` pairs modeling stdin/stdout streams.
2. **`TestRawServerAddToolProxy`**:
   - Single-server baseline showing that `server.AddTool` (raw untyped handler) accepts arbitrary JSON payloads in `req.Params.Arguments` (`json.RawMessage`).
3. **`TestDynamicToolUpdatesAndNotification`**:
   - Registers tool A, client connects with `ToolListChangedHandler`.
   - Server calls `server.AddTool(...)` for tool B: client handler triggers, `ListTools` returns both tools.
   - Server calls `server.RemoveTools("initial_tool")`: client handler triggers, `ListTools` returns only tool B.
4. **`TestMultipleClientsRouting`**:
   - `StreamableHTTP_DistinctSessionIDs`: 3 concurrent clients connected via Streamable HTTP. Verifies `req.Session.ID()` yields unique non-empty session IDs per client and routes calls concurrently without crosstalk.
   - `InMemory_DistinctSessionPointers`: 4 concurrent clients connected via in-memory pipes. Verifies each client maintains a unique `*mcp.ServerSession` reference without crosstalk.
5. **`TestCancellationViaContext`**:
   - Single-hop cancellation: client cancels context, JSON-RPC `notifications/cancelled` unblocks the server tool handler via `ctx.Done()`.
6. **`TestMiddlewareToolsListAtomicSnapshot`**:
   - Middleware intercepts `method == "tools/list"` and returns an atomic snapshot loaded from `atomic.Pointer[mcp.ListToolsResult]`, while normal tool calls execute via standard SDK handler execution.

---

### Part B: Real Two-Hop Forwarding Chain (Test 7)
**`TestTwoHopForwardingChain`**:
- **Topology**: Origin Client -> [Hop 1: Streamable HTTP over `httptest.Server`] -> Hub Server -> [Hop 2: `mcp.ClientSession` over In-Memory Transport] -> Downstream Server.
- **Raw Big Integer Argument Preservation (`9007199254740993`)**:
  - `9007199254740993` is `2^53 + 1`. If parsed into standard IEEE 754 float64, it loses precision and rounds to `9007199254740992`.
  - The test passes `json.RawMessage(`{"big_int": 9007199254740993, ...}`)` from the origin client through the Hub HTTP server into the downstream server.
  - The downstream server verifies the literal byte substring `"9007199254740993"` in `req.Params.Arguments`, proving zero truncation across both hops.
- **Image Content Preservation**:
  - Downstream server returns `&mcp.ImageContent{MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0xAA, 0xBB}}`.
  - Origin client receives the `*mcp.ImageContent` across HTTP and asserts byte-for-byte equality.
- **Structured Content & Text Content Preservation**:
  - Unstructured text and structured JSON maps are returned and verified at the origin client.
- **Error Flag Preservation (`isError: true`)**:
  - Downstream returns `IsError: true` with error text; Hub forwards result; origin client receives `res.IsError == true` without protocol errors.
- **Two-Hop Cancellation Propagation**:
  - Origin client cancels `callCtx`.
  - Cancellation propagates across Hop 1 (HTTP) to Hub, then across Hop 2 to Downstream Server.
  - Downstream handler observes `<-ctx.Done()` and unblocks. Origin client returns non-nil cancellation error.
- **Exactly-Once Side-Effect Assertion**:
  - Downstream server maintains atomic execution counters: normal call = 1, error call = 1, cancellation call = 1.

---

### Part C: Production Publish RWMutex Concurrency Design (Test 8)
**`TestProductionPublishRWMutexPattern`**:
- **Concurrency Architecture**:
  1. Hub maintains a `sync.RWMutex` (`hub.mu`) and route table `routes map[string]*mcp.ClientSession`.
  2. `tools/list` middleware wraps receiving handler:
     ```go
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
     ```
  3. Publisher updates both SDK tools (`s.AddTool`, `s.RemoveTools`) and `routes` map under `hub.mu.Lock()`:
     ```go
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
     ```
  4. Tool calls release lock before network:
     ```go
     func (h *ProductionHub) dispatchTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
         var target *mcp.ClientSession
         h.mu.RLock()
         target = h.routes[req.Params.Name]
         h.mu.RUnlock() // CRITICAL: Released BEFORE downstream network call!

         if target == nil {
             return nil, fmt.Errorf("tool route %q not found", req.Params.Name)
         }
         return target.CallTool(ctx, &mcp.CallToolParams{
             Name:      req.Params.Name,
             Arguments: req.Params.Arguments,
         })
     }
     ```
- **Proven Properties**:
  - **Non-blocking Network Calls**: While a slow downstream tool call is blocked in flight, `Publish(...)` acquires `Lock` and completes immediately; `tools/list` acquires `RLock` and completes immediately. Long network calls never hold the publish lock.
  - **Publish Atomicity**: `tools/list` holding `RLock` never observes half-applied publisher tool batches.
  - **Race-free Concurrency**: Concurrently running goroutines calling `tools/list`, `CallTool`, and `Publish`/unpublish pass `go test -race` with 0 data races.

---

## 4. Execution Commands & Race Detector Outcomes

### Environment
- Go version: `go version go1.26.3 windows/amd64`
- Pinned SDK: `github.com/modelcontextprotocol/go-sdk v1.4.1`
- GOPROXY: `https://goproxy.cn,direct`

### Commands Run

```powershell
$env:GOPROXY='https://goproxy.cn,direct'; go test -race -v ./...
```

### Full Output

```text
=== RUN   TestDownstreamTransports
=== RUN   TestDownstreamTransports/StreamableHTTP
=== RUN   TestDownstreamTransports/InMemoryPipe
=== RUN   TestDownstreamTransports/IOTransport_StdinStdoutFixture
--- PASS: TestDownstreamTransports (0.01s)
    --- PASS: TestDownstreamTransports/StreamableHTTP (0.01s)
    --- PASS: TestDownstreamTransports/InMemoryPipe (0.00s)
    --- PASS: TestDownstreamTransports/IOTransport_StdinStdoutFixture (0.00s)
=== RUN   TestRawServerAddToolProxy
--- PASS: TestRawServerAddToolProxy (0.00s)
=== RUN   TestDynamicToolUpdatesAndNotification
--- PASS: TestDynamicToolUpdatesAndNotification (0.02s)
=== RUN   TestMultipleClientsRouting
=== RUN   TestMultipleClientsRouting/StreamableHTTP_DistinctSessionIDs
=== RUN   TestMultipleClientsRouting/InMemory_DistinctSessionPointers
--- PASS: TestMultipleClientsRouting (0.01s)
    --- PASS: TestMultipleClientsRouting/StreamableHTTP_DistinctSessionIDs (0.01s)
    --- PASS: TestMultipleClientsRouting/InMemory_DistinctSessionPointers (0.00s)
=== RUN   TestCancellationViaContext
--- PASS: TestCancellationViaContext (0.00s)
=== RUN   TestMiddlewareToolsListAtomicSnapshot
--- PASS: TestMiddlewareToolsListAtomicSnapshot (0.00s)
=== RUN   TestTwoHopForwardingChain
=== RUN   TestTwoHopForwardingChain/NormalCall_BigInt_Image_Structured
=== RUN   TestTwoHopForwardingChain/ErrorCall_IsErrorPreservation
=== RUN   TestTwoHopForwardingChain/TwoHop_Cancellation
--- PASS: TestTwoHopForwardingChain (0.00s)
    --- PASS: TestTwoHopForwardingChain/NormalCall_BigInt_Image_Structured (0.00s)
    --- PASS: TestTwoHopForwardingChain/ErrorCall_IsErrorPreservation (0.00s)
    --- PASS: TestTwoHopForwardingChain/TwoHop_Cancellation (0.00s)
=== RUN   TestProductionPublishRWMutexPattern
--- PASS: TestProductionPublishRWMutexPattern (0.08s)
PASS
ok  	sdkprobe	1.763s
```

---

## 5. Architectural Nuances & Recommendations

1. **Big Integer Safety**:
   - Intermediate hub layers MUST treat `req.Params.Arguments` strictly as `json.RawMessage` and pass it directly to `CallToolParams{Arguments: req.Params.Arguments}`. Never unmarshal arguments into `map[string]any` without `UseNumber()` in the forwarding path, as numbers above `2^53 - 1` (`9007199254740991`) lose precision.

2. **Lock Granularity in Tool Dispatch**:
   - Always copy the downstream target pointer under `RLock` and call `RUnlock()` immediately before making the outbound `CallTool` call. Holding any mutex across network I/O creates cascading head-of-line blocking for tool publishing and list queries.

3. **Notification Debouncing**:
   - `server.changeAndNotify` introduces a built-in 10ms debounce delay (`notificationDelay = 10 * time.Millisecond`). Dynamic additions and removals trigger notifications asynchronously.
