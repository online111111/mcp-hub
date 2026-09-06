package router

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/catalog"
)

// fakeRouteLookup implements RouteLookup for testing.
type fakeRouteLookup struct {
	mu     sync.RWMutex
	routes map[string]catalog.RouteEntry
}

func (f *fakeRouteLookup) RLock() {
	f.mu.RLock()
}

func (f *fakeRouteLookup) RUnlock() {
	f.mu.RUnlock()
}

func (f *fakeRouteLookup) LookupRoute(publicName string) (catalog.RouteEntry, bool) {
	r, ok := f.routes[publicName]
	return r, ok
}

// fakeSession implements Session for testing.
type fakeSession struct {
	callCount atomic.Int64
	lastTool  string
	lastArgs  json.RawMessage
	callFn    func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error)
}

func (s *fakeSession) CallTool(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
	s.callCount.Add(1)
	s.lastTool = name
	s.lastArgs = args
	if s.callFn != nil {
		return s.callFn(ctx, name, args)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "ok"},
		},
	}, nil
}

// fakeLease implements Lease for testing.
type fakeLease struct {
	session        Session
	serverID       string
	genID          uint64
	timeout        time.Duration
	releaseCount   atomic.Int64
	generationDone <-chan struct{}
}

func (l *fakeLease) Release() {
	l.releaseCount.Add(1)
}

func (l *fakeLease) Session() Session {
	return l.session
}

func (l *fakeLease) ServerID() string {
	return l.serverID
}

func (l *fakeLease) GenerationID() uint64 {
	return l.genID
}

func (l *fakeLease) CallTimeout() time.Duration {
	return l.timeout
}

func (l *fakeLease) GenerationDone() <-chan struct{} {
	return l.generationDone
}

// fakeLeaseProvider implements LeaseProvider for testing.
type fakeLeaseProvider struct {
	leaseFn func(serverID string) (Lease, error)
}

func (p *fakeLeaseProvider) AcquireLease(serverID string) (Lease, error) {
	if p.leaseFn != nil {
		return p.leaseFn(serverID)
	}
	return nil, ErrServerUnavailable
}

func TestRouter_UnknownOrMissingTool(t *testing.T) {
	rl := &fakeRouteLookup{routes: map[string]catalog.RouteEntry{}}
	lp := &fakeLeaseProvider{}
	r := NewRouter(rl, lp)

	// Nil request
	_, err := r.RouteTool(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on nil request")
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("expected CodeInvalidParams (-32602), got %v", err)
	}

	// Unknown tool
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "unknown__tool",
		},
	}
	_, err = r.RouteTool(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on unknown tool")
	}
	if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("expected CodeInvalidParams (-32602), got %v", err)
	}
}

func TestRouter_ServerBusyAndUnavailable(t *testing.T) {
	rl := &fakeRouteLookup{
		routes: map[string]catalog.RouteEntry{
			"srv__tool": {
				PublicName:   "srv__tool",
				ServerID:     "srv",
				OriginalName: "tool",
			},
		},
	}

	// 1. Server Busy (concurrency limit)
	lpBusy := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return nil, ErrServerBusy
		},
	}
	rBusy := NewRouter(rl, lpBusy)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__tool"},
	}

	res, err := rBusy.RouteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true for busy server")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "[busy]") || !strings.Contains(tc.Text, "request_id:") {
		t.Fatalf("unexpected busy text: %v", tc)
	}

	// 2. Server Unavailable
	lpUnavail := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return nil, ErrServerUnavailable
		},
	}
	rUnavail := NewRouter(rl, lpUnavail)
	res, err = rUnavail.RouteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true for unavailable server")
	}
	tc, ok = res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "[unavailable]") || !strings.Contains(tc.Text, "request_id:") {
		t.Fatalf("unexpected unavailable text: %v", tc)
	}
}

func TestRouter_SuccessAndArgPreservation(t *testing.T) {
	rawArgs := json.RawMessage(`{"bigInt":9007199254740993,"token":"secret"}`)
	rl := &fakeRouteLookup{
		routes: map[string]catalog.RouteEntry{
			"srv__calculate": {
				PublicName:   "srv__calculate",
				ServerID:     "srv",
				OriginalName: "calculate",
			},
		},
	}

	sess := &fakeSession{}
	lease := &fakeLease{
		session:  sess,
		serverID: "srv",
		genID:    1,
		timeout:  5 * time.Second,
	}
	lp := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return lease, nil
		},
	}

	r := NewRouter(rl, lp)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "srv__calculate",
			Arguments: rawArgs,
		},
	}

	res, err := r.RouteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected isError=false")
	}

	// Verify original tool name called
	if sess.lastTool != "calculate" {
		t.Fatalf("expected tool 'calculate', got %q", sess.lastTool)
	}

	// Verify raw arguments preserved byte-for-byte (including large integer)
	if string(sess.lastArgs) != string(rawArgs) {
		t.Fatalf("expected args %s, got %s", string(rawArgs), string(sess.lastArgs))
	}

	// Verify lease released exactly once
	if lease.releaseCount.Load() != 1 {
		t.Fatalf("expected releaseCount 1, got %d", lease.releaseCount.Load())
	}
}

func TestRouter_CallTimeout(t *testing.T) {
	rl := &fakeRouteLookup{
		routes: map[string]catalog.RouteEntry{
			"srv__slow": {
				PublicName:   "srv__slow",
				ServerID:     "srv",
				OriginalName: "slow",
			},
		},
	}

	sess := &fakeSession{
		callFn: func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Second):
				return &mcp.CallToolResult{}, nil
			}
		},
	}
	lease := &fakeLease{
		session:  sess,
		serverID: "srv",
		genID:    1,
		timeout:  50 * time.Millisecond,
	}
	lp := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return lease, nil
		},
	}

	r := NewRouter(rl, lp)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__slow"},
	}

	res, err := r.RouteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true on timeout")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "[timeout]") || !strings.Contains(tc.Text, "request_id:") {
		t.Fatalf("unexpected timeout text: %v", tc)
	}

	// Must be called exactly once: timeout / cancellation must not retry!
	if sess.callCount.Load() != 1 {
		t.Fatalf("expected 1 downstream call, got %d", sess.callCount.Load())
	}
}

func TestRouter_ClientCancellation(t *testing.T) {
	rl := &fakeRouteLookup{
		routes: map[string]catalog.RouteEntry{
			"srv__tool": {
				PublicName:   "srv__tool",
				ServerID:     "srv",
				OriginalName: "tool",
			},
		},
	}

	sess := &fakeSession{
		callFn: func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	lease := &fakeLease{
		session:  sess,
		serverID: "srv",
		genID:    1,
		timeout:  5 * time.Second,
	}
	lp := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return lease, nil
		},
	}

	r := NewRouter(rl, lp)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__tool"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before / during

	_, err := r.RouteTool(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRouter_GenerationShutdownCancelsCall(t *testing.T) {
	rl := &fakeRouteLookup{routes: map[string]catalog.RouteEntry{
		"srv__tool": {PublicName: "srv__tool", ServerID: "srv", OriginalName: "tool"},
	}}
	started := make(chan struct{})
	sess := &fakeSession{callFn: func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	generationDone := make(chan struct{})
	lease := &fakeLease{session: sess, serverID: "srv", genID: 1, timeout: 5 * time.Second, generationDone: generationDone}
	r := NewRouter(rl, &fakeLeaseProvider{leaseFn: func(string) (Lease, error) { return lease, nil }})

	resultCh := make(chan *mcp.CallToolResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := r.RouteTool(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "srv__tool"}})
		resultCh <- res
		errCh <- err
	}()
	<-started
	close(generationDone)

	select {
	case res := <-resultCh:
		err := <-errCh
		if err != nil {
			t.Fatalf("unexpected protocol error: %v", err)
		}
		if res == nil || !res.IsError {
			t.Fatalf("expected unavailable CallToolResult, got %+v", res)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(text, "[unavailable]") {
			t.Fatalf("unexpected result text: %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("generation shutdown did not cancel downstream call")
	}
}

func TestRouter_DownstreamErrorSanitization(t *testing.T) {
	rl := &fakeRouteLookup{
		routes: map[string]catalog.RouteEntry{
			"srv__fail": {
				PublicName:   "srv__fail",
				ServerID:     "srv",
				OriginalName: "fail",
			},
		},
	}

	sess := &fakeSession{
		callFn: func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
			return nil, errors.New("sensitive connection error to internal db 10.0.0.1:5432 with password secret123")
		},
	}
	lease := &fakeLease{
		session:  sess,
		serverID: "srv",
		genID:    1,
		timeout:  5 * time.Second,
	}
	lp := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return lease, nil
		},
	}

	r := NewRouter(rl, lp)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__fail"},
	}

	res, err := r.RouteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true on downstream error")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "[downstream_error]") {
		t.Fatalf("unexpected text: %v", tc)
	}
	// Verify raw sensitive error message was NOT leaked
	if strings.Contains(tc.Text, "secret123") || strings.Contains(tc.Text, "10.0.0.1") {
		t.Fatalf("sensitive details leaked in error text: %s", tc.Text)
	}
}

func TestRouter_DownstreamToolFailurePreserved(t *testing.T) {
	rl := &fakeRouteLookup{
		routes: map[string]catalog.RouteEntry{
			"srv__app_error": {
				PublicName:   "srv__app_error",
				ServerID:     "srv",
				OriginalName: "app_error",
			},
		},
	}

	expectedResult := &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: "file not found: config.txt"},
		},
	}

	sess := &fakeSession{
		callFn: func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
			return expectedResult, nil
		},
	}
	lease := &fakeLease{
		session:  sess,
		serverID: "srv",
		genID:    1,
		timeout:  5 * time.Second,
	}
	lp := &fakeLeaseProvider{
		leaseFn: func(serverID string) (Lease, error) {
			return lease, nil
		},
	}

	r := NewRouter(rl, lp)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__app_error"},
	}

	res, err := r.RouteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "file not found: config.txt" {
		t.Fatalf("expected preserved application error text, got %v", res.Content)
	}
}
