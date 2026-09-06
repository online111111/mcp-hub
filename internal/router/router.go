package router

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/catalog"
)

// Standard error categories for CallToolResult error reporting.
const (
	CategoryUnavailable     = "unavailable"
	CategoryBusy            = "busy"
	CategoryTimeout         = "timeout"
	CategoryDownstreamError = "downstream_error"
	CategoryInternalError   = "internal_error"
)

var (
	// ErrServerUnavailable indicates downstream server is not in Ready state or not admitting calls.
	ErrServerUnavailable = errors.New("server unavailable")
	// ErrServerBusy indicates downstream server has reached its maxConcurrency limit.
	ErrServerBusy = errors.New("server busy, concurrency limit reached")
	// ErrServerNotFound indicates the target server is not configured or disabled.
	ErrServerNotFound = errors.New("server not found")
)

// Session defines the downstream session capability needed by the router.
type Session interface {
	CallTool(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error)
}

// Lease represents an active concurrency lease on a downstream generation.
type Lease interface {
	Release()
	Session() Session
	ServerID() string
	GenerationID() uint64
	CallTimeout() time.Duration
	GenerationDone() <-chan struct{}
}

// LeaseProvider provides leases for downstream tool calls.
type LeaseProvider interface {
	AcquireLease(serverID string) (Lease, error)
}

// RouteLookup provides route resolution protected by RLock/RUnlock.
type RouteLookup interface {
	RLock()
	RUnlock()
	LookupRoute(publicName string) (catalog.RouteEntry, bool)
}

// CallRecord is an owned, sanitized summary of one routed tool invocation.
// It contains no parameters, results, sessions, or transport details.
type CallRecord struct {
	RequestID     string
	Time          time.Time
	Duration      time.Duration
	Tool          string
	ServerID      string
	Outcome       string
	ErrorCategory string
}

// CallRecorder receives a completed routed-call summary.
type CallRecorder func(CallRecord)

// Router routes incoming MCP tool calls to the appropriate downstream session.
// It enforces catalog routing, lease admission, call timeouts, and safe error mapping.
type Router struct {
	routes RouteLookup
	leases LeaseProvider

	recordMu sync.RWMutex
	record   CallRecorder
}

// NewRouter creates a new Router with the provided route lookup and lease provider.
func NewRouter(routes RouteLookup, leases LeaseProvider) *Router {
	return &Router{
		routes: routes,
		leases: leases,
	}
}

// SetCallRecorder installs an optional diagnostics recorder. The callback receives
// an owned, already-sanitized summary after each known-tool invocation.
func (r *Router) SetCallRecorder(record CallRecorder) {
	r.recordMu.Lock()
	defer r.recordMu.Unlock()
	r.record = record
}

func (r *Router) callRecorder() CallRecorder {
	r.recordMu.RLock()
	defer r.recordMu.RUnlock()
	return r.record
}

func (r *Router) recordCall(call CallRecord) {
	if record := r.callRecorder(); record != nil {
		record(call)
	}
}

// GenerateRequestID creates a unique, safe request identifier for error tracing.
func GenerateRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("req-%x", b)
}

// RouteTool handles an incoming MCP tool call request according to Section 6.2 and 6.3.
// It is compatible with inbound.Publisher.RouterCallback.
func (r *Router) RouteTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil || req.Params.Name == "" {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: "invalid params: tool name is required",
		}
	}

	publicName := req.Params.Name
	reqID := GenerateRequestID()
	startedAt := time.Now()
	outcome := "error"
	errorCategory := CategoryInternalError
	serverID := ""
	defer func() {
		if ctx.Err() != nil && errorCategory == CategoryInternalError {
			errorCategory = "cancelled"
		}
		r.recordCall(CallRecord{
			RequestID:     reqID,
			Time:          startedAt,
			Duration:      time.Since(startedAt),
			Tool:          publicName,
			ServerID:      serverID,
			Outcome:       outcome,
			ErrorCategory: errorCategory,
		})
	}()

	// 1. Short read lock on Publisher to resolve route.
	if r.routes == nil {
		errorCategory = CategoryInternalError
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInternalError,
			Message: "router: route lookup not configured",
		}
	}

	r.routes.RLock()
	route, ok := r.routes.LookupRoute(publicName)
	r.routes.RUnlock()

	// Unknown or disabled tool -> JSON-RPC -32602.
	if !ok {
		errorCategory = CategoryUnavailable
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("unknown tool %q", publicName),
		}
	}
	serverID = route.ServerID

	// 2. Acquire generation lease (no wait queue, non-blocking).
	if r.leases == nil {
		errorCategory = CategoryInternalError
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("[%s] lease provider not configured (request_id: %s)", CategoryInternalError, reqID),
				},
			},
		}, nil
	}

	lease, err := r.leases.AcquireLease(route.ServerID)
	if err != nil {
		if errors.Is(err, ErrServerBusy) {
			errorCategory = CategoryBusy
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("[%s] server %q concurrency limit reached (request_id: %s)", CategoryBusy, route.ServerID, reqID),
					},
				},
			}, nil
		}

		errorCategory = CategoryUnavailable
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("[%s] server %q is unavailable (request_id: %s)", CategoryUnavailable, route.ServerID, reqID),
				},
			},
		}, nil
	}
	defer lease.Release()

	// 3. Child context canceled by the client, the call timeout, or generation shutdown.
	generationCtx, generationCancel := context.WithCancel(ctx)
	defer generationCancel()
	if generationDone := lease.GenerationDone(); generationDone != nil {
		go func() {
			select {
			case <-generationDone:
				generationCancel()
			case <-generationCtx.Done():
			}
		}()
	}

	callCtx := context.Context(generationCtx)
	callTimeout := lease.CallTimeout()
	if callTimeout > 0 {
		var timeoutCancel context.CancelFunc
		callCtx, timeoutCancel = context.WithTimeout(generationCtx, callTimeout)
		defer timeoutCancel()
	}

	// 4. Downstream invocation with original tool name and raw JSON arguments.
	// Preserves large numbers by passing json.RawMessage unmodified.
	downstreamSession := lease.Session()
	if downstreamSession == nil {
		errorCategory = CategoryUnavailable
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("[%s] server %q session is nil (request_id: %s)", CategoryUnavailable, route.ServerID, reqID),
				},
			},
		}, nil
	}

	res, err := downstreamSession.CallTool(callCtx, route.OriginalName, req.Params.Arguments)

	// 5. Client context cancellation: propagate context error directly.
	if ctx.Err() != nil {
		errorCategory = "cancelled"
		return nil, ctx.Err()
	}

	// 6. Call timeout or generation shutdown.
	if callCtx.Err() != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			errorCategory = CategoryTimeout
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("[%s] tool call timed out after %v (request_id: %s)", CategoryTimeout, callTimeout, reqID),
					},
				},
			}, nil
		}
		errorCategory = CategoryUnavailable
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("[%s] server %q generation stopped (request_id: %s)", CategoryUnavailable, route.ServerID, reqID),
				},
			},
		}, nil
	}

	// 7. Downstream protocol / transport error: sanitized, do not leak command or credentials.
	if err != nil {
		errorCategory = CategoryDownstreamError
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("[%s] downstream tool execution failed (request_id: %s)", CategoryDownstreamError, reqID),
				},
			},
		}, nil
	}

	// 8. Downstream success / application error (res.IsError preserved).
	if res == nil {
		outcome = "success"
		errorCategory = ""
		return &mcp.CallToolResult{}, nil
	}
	if res.IsError {
		outcome = "tool_error"
		errorCategory = CategoryDownstreamError
	} else {
		outcome = "success"
		errorCategory = ""
	}
	return res, nil
}
