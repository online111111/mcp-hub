package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/catalog"
	"mcp-hub/internal/config"
)

// fakeSession implements Session for manager testing.
type fakeSession struct {
	serverID  string
	mu        sync.Mutex
	tools     []*mcp.Tool
	callCount atomic.Int64
	closed    atomic.Bool
	closeCh   chan struct{}
	closeOnce sync.Once
	callFn    func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error)
	onClose   func()
}

func newFakeSession(serverID string, tools []*mcp.Tool, onClose func()) *fakeSession {
	return &fakeSession{
		serverID: serverID,
		tools:    tools,
		closeCh:  make(chan struct{}),
		onClose:  onClose,
	}
}

func (s *fakeSession) SetTools(tools []*mcp.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = tools
}

func (s *fakeSession) CallTool(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
	if s.IsClosed() {
		return nil, errors.New("session closed")
	}
	s.callCount.Add(1)
	if s.callFn != nil {
		return s.callFn(ctx, name, args)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "result-" + name}},
	}, nil
}

func (s *fakeSession) ListAllTools(ctx context.Context) ([]*mcp.Tool, error) {
	if s.IsClosed() {
		return nil, errors.New("session closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools, nil
}

func (s *fakeSession) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.closeCh)
		if s.onClose != nil {
			s.onClose()
		}
	})
	return nil
}

func (s *fakeSession) IsClosed() bool {
	return s.closed.Load()
}

// fakeSessionFactory implements SessionFactory with concurrency and instance tracking.
type fakeSessionFactory struct {
	mu              sync.Mutex
	createHook      func(srv config.ResolvedServer) (Session, io.Closer, error)
	activeInstances atomic.Int32
	maxInstances    atomic.Int32
	currentDials    atomic.Int32
	maxDials        atomic.Int32
}

func (f *fakeSessionFactory) CreateSession(
	ctx context.Context,
	srv config.ResolvedServer,
	onToolListChanged func(),
) (Session, io.Closer, error) {
	curDials := f.currentDials.Add(1)
	for {
		max := f.maxDials.Load()
		if curDials <= max || f.maxDials.CompareAndSwap(max, curDials) {
			break
		}
	}
	defer f.currentDials.Add(-1)

	f.mu.Lock()
	hook := f.createHook
	f.mu.Unlock()

	if hook != nil {
		return hook(srv)
	}

	cur := f.activeInstances.Add(1)
	for {
		max := f.maxInstances.Load()
		if cur <= max || f.maxInstances.CompareAndSwap(max, cur) {
			break
		}
	}

	tools := []*mcp.Tool{
		{
			Name:        "tool1",
			Description: "test tool",
			InputSchema: map[string]any{"type": "object"},
		},
	}

	sess := newFakeSession(srv.ID, tools, func() {
		f.activeInstances.Add(-1)
	})
	return sess, nil, nil
}

func setupTestManager(factory SessionFactory) (*Manager, *catalog.Catalog) {
	cat := catalog.NewCatalog()
	opts := []Option{
		WithSessionFactory(factory),
		WithBackoffDelays([]time.Duration{10 * time.Millisecond, 20 * time.Millisecond}),
		WithReadyResetDuration(100 * time.Millisecond),
		WithDrainTimeout(200 * time.Millisecond),
	}
	mgr := NewManager(NewCatalogPublisher(cat), opts...)
	return mgr, cat
}

func TestManager_ServerFailureIsolation(t *testing.T) {
	factory := &fakeSessionFactory{
		createHook: func(srv config.ResolvedServer) (Session, io.Closer, error) {
			if srv.ID == "srv-fail" {
				return nil, nil, errors.New("connection refused to fail server")
			}
			tools := []*mcp.Tool{
				{
					Name:        "ping",
					Description: "ping tool",
					InputSchema: map[string]any{"type": "object"},
				},
			}
			return newFakeSession(srv.ID, tools, nil), nil, nil
		},
	}

	mgr, cat := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfg := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv-fail": {
				ID:             "srv-fail",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
				StartupTimeout: 500 * time.Millisecond,
			},
			"srv-ok": {
				ID:             "srv-ok",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
				StartupTimeout: 500 * time.Millisecond,
			},
		},
	}

	if err := mgr.Apply(ctx, cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Wait for srv-ok to become Ready
	time.Sleep(50 * time.Millisecond)

	status := mgr.Status()
	if status["srv-ok"].State != StateReady {
		t.Fatalf("expected srv-ok to be StateReady, got %s", status["srv-ok"].State)
	}
	if status["srv-fail"].State != StateBackoff && status["srv-fail"].State != StateUnavailable && status["srv-fail"].State != StateConnecting {
		t.Fatalf("expected srv-fail in failure/backoff state, got %s", status["srv-fail"].State)
	}

	// Verify tool call to srv-ok succeeds
	reqOK := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv-ok__ping"},
	}
	resOK, err := mgr.RouteTool(ctx, reqOK)
	if err != nil {
		t.Fatalf("RouteTool srv-ok failed: %v", err)
	}
	if resOK.IsError {
		t.Fatalf("expected srv-ok call to succeed, got error: %+v", resOK)
	}

	// Verify tool call to srv-fail returns unavailable
	cat.UpdateServerTools("srv-fail", []*mcp.Tool{
		{Name: "dummy", Description: "dummy", InputSchema: map[string]any{"type": "object"}},
	}, nil)

	reqFail := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv-fail__dummy"},
	}
	resFail, err := mgr.RouteTool(ctx, reqFail)
	if err != nil {
		t.Fatalf("RouteTool srv-fail unexpected err: %v", err)
	}
	if !resFail.IsError {
		t.Fatalf("expected srv-fail to return isError=true")
	}
	tc := resFail.Content[0].(*mcp.TextContent)
	if !strings.Contains(tc.Text, "[unavailable]") {
		t.Fatalf("expected unavailable category, got %s", tc.Text)
	}
}

func TestManager_StaleAsyncConnectionCannotOverwrite(t *testing.T) {
	slowDialStarted := make(chan struct{})
	allowSlowDialToFinish := make(chan struct{})

	factory := &fakeSessionFactory{}
	factory.createHook = func(srv config.ResolvedServer) (Session, io.Closer, error) {
		if srv.URL == "http://slow-rev1" {
			close(slowDialStarted)
			<-allowSlowDialToFinish
			tools := []*mcp.Tool{
				{Name: "slow_tool", Description: "old", InputSchema: map[string]any{"type": "object"}},
			}
			return newFakeSession(srv.ID, tools, nil), nil, nil
		}
		tools := []*mcp.Tool{
			{Name: "fast_tool", Description: "new", InputSchema: map[string]any{"type": "object"}},
		}
		return newFakeSession(srv.ID, tools, nil), nil, nil
	}

	mgr, cat := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	// 1. Apply Rev 1 with slow URL
	cfgRev1 := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				URL:            "http://slow-rev1",
				MaxConcurrency: 4,
			},
		},
	}
	if err := mgr.Apply(ctx, cfgRev1); err != nil {
		t.Fatalf("Apply rev1 failed: %v", err)
	}

	// Wait for slow dial to start
	<-slowDialStarted

	// 2. Quickly Apply Rev 2 with different URL while Rev 1 is still in dial
	cfgRev2 := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				URL:            "http://fast-rev2",
				MaxConcurrency: 4,
			},
		},
	}
	if err := mgr.Apply(ctx, cfgRev2); err != nil {
		t.Fatalf("Apply rev2 failed: %v", err)
	}

	// Unblock the Rev 1 dial so it completes
	close(allowSlowDialToFinish)

	// Wait for reconciliation to settle
	time.Sleep(80 * time.Millisecond)

	// Verify catalog only contains fast_tool from Rev 2, never slow_tool from obsolete Rev 1
	snap := cat.Snapshot()
	if _, ok := snap.LookupRoute("srv__slow_tool"); ok {
		t.Fatal("obsolete Rev 1 slow_tool was published! Stale async connection leaked into catalog!")
	}
	if _, ok := snap.LookupRoute("srv__fast_tool"); !ok {
		t.Fatal("expected Rev 2 fast_tool to be published in catalog")
	}

	status := mgr.Status()
	if status["srv"].DesiredRevision != 2 {
		t.Fatalf("expected desired revision 2, got %d", status["srv"].DesiredRevision)
	}
}

func TestManager_RapidConfigChanges_A_B_C(t *testing.T) {
	factory := &fakeSessionFactory{}
	mgr, cat := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	makeCfg := func(url string) *config.ResolvedConfig {
		return &config.ResolvedConfig{
			Servers: map[string]config.ResolvedServer{
				"srv": {
					ID:             "srv",
					Enabled:        true,
					Type:           config.ServerTypeStreamableHTTP,
					URL:            url,
					MaxConcurrency: 4,
				},
			},
		}
	}

	// Rapidly apply A -> B -> C
	_ = mgr.Apply(ctx, makeCfg("http://srv-a"))
	_ = mgr.Apply(ctx, makeCfg("http://srv-b"))
	_ = mgr.Apply(ctx, makeCfg("http://srv-c"))

	// Settle
	time.Sleep(100 * time.Millisecond)

	status := mgr.Status()
	if status["srv"].State != StateReady {
		t.Fatalf("expected srv to settle in StateReady, got %s", status["srv"].State)
	}
	if status["srv"].DesiredRevision != 3 {
		t.Fatalf("expected final revision 3, got %d", status["srv"].DesiredRevision)
	}

	// Verify RouteTool works for final server
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__tool1"},
	}
	res, err := mgr.RouteTool(ctx, req)
	if err != nil || res.IsError {
		t.Fatalf("expected tool call to succeed on C: err=%v, res=%+v", err, res)
	}
	_ = cat
}

func TestManager_ToolFilteringDoesNotRestartSession(t *testing.T) {
	factory := &fakeSessionFactory{}
	mgr, cat := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfgInitial := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
			},
		},
	}

	if err := mgr.Apply(ctx, cfgInitial); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	statusBefore := mgr.Status()
	genIDBefore := statusBefore["srv"].GenerationID
	if statusBefore["srv"].PublishedTools != 1 {
		t.Fatalf("expected 1 published tool initially, got %d", statusBefore["srv"].PublishedTools)
	}

	// Apply updated config with tool1 disabled
	cfgDisabled := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
				DisabledTools: map[string]struct{}{
					"tool1": {},
				},
			},
		},
	}

	if err := mgr.Apply(ctx, cfgDisabled); err != nil {
		t.Fatalf("Apply with disabled tool failed: %v", err)
	}

	// Settle
	time.Sleep(20 * time.Millisecond)

	statusAfter := mgr.Status()
	genIDAfter := statusAfter["srv"].GenerationID

	// Generation ID MUST be identical: session was not restarted!
	if genIDBefore != genIDAfter {
		t.Fatalf("expected generation ID %d to remain unchanged, got %d (session was restarted!)", genIDBefore, genIDAfter)
	}

	// Tool should now be unpublished in catalog
	snap := cat.Snapshot()
	if _, ok := snap.LookupRoute("srv__tool1"); ok {
		t.Fatal("disabled tool1 is still published in catalog")
	}
	if statusAfter["srv"].PublishedTools != 0 {
		t.Fatalf("expected 0 published tools, got %d", statusAfter["srv"].PublishedTools)
	}
}

func TestManager_BreakBeforeMake_NoConcurrentInstances(t *testing.T) {
	factory := &fakeSessionFactory{}
	mgr, _ := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	// Apply initial config
	cfg1 := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				URL:            "http://127.0.0.1:1001",
				MaxConcurrency: 4,
			},
		},
	}
	_ = mgr.Apply(ctx, cfg1)
	time.Sleep(50 * time.Millisecond)

	// Re-apply 5 times with changing connection URL
	for i := 2; i <= 6; i++ {
		cfg := &config.ResolvedConfig{
			Servers: map[string]config.ResolvedServer{
				"srv": {
					ID:             "srv",
					Enabled:        true,
					Type:           config.ServerTypeStreamableHTTP,
					URL:            fmt.Sprintf("http://127.0.0.1:100%d", i),
					MaxConcurrency: 4,
				},
			},
		}
		_ = mgr.Apply(ctx, cfg)
		time.Sleep(40 * time.Millisecond)
	}

	// Assert max concurrent active instances was never > 1!
	maxActive := factory.maxInstances.Load()
	if maxActive > 1 {
		t.Fatalf("break-before-make violated! Observed %d concurrent sessions running simultaneously!", maxActive)
	}
}

func TestManager_AdmissionAndDrainSimultaneous(t *testing.T) {
	var inFlightStarted sync.WaitGroup
	releaseInFlight := make(chan struct{})

	factory := &fakeSessionFactory{
		createHook: func(srv config.ResolvedServer) (Session, io.Closer, error) {
			sess := newFakeSession(srv.ID, []*mcp.Tool{
				{Name: "slow", Description: "slow", InputSchema: map[string]any{"type": "object"}},
			}, nil)
			sess.callFn = func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
				inFlightStarted.Done()
				<-releaseInFlight
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil
			}
			return sess, nil, nil
		},
	}

	mgr, _ := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfg := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
			},
		},
	}
	_ = mgr.Apply(ctx, cfg)
	time.Sleep(40 * time.Millisecond)

	// Start 2 in-flight tool calls
	inFlightStarted.Add(2)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__slow"},
	}

	var callWg sync.WaitGroup
	callWg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer callWg.Done()
			_, _ = mgr.RouteTool(ctx, req)
		}()
	}

	// Wait until both in-flight calls have acquired leases
	inFlightStarted.Wait()

	// While calls are in-flight, trigger drain by updating config URL
	drainFinished := make(chan struct{})
	go func() {
		cfgNew := &config.ResolvedConfig{
			Servers: map[string]config.ResolvedServer{
				"srv": {
					ID:             "srv",
					Enabled:        true,
					Type:           config.ServerTypeStreamableHTTP,
					URL:            "http://new-url",
					MaxConcurrency: 4,
				},
			},
		}
		_ = mgr.Apply(ctx, cfgNew)
		close(drainFinished)
	}()

	// During drain: new lease acquisition MUST fail immediately (admission closed)
	time.Sleep(15 * time.Millisecond)
	lease, err := mgr.AcquireLease("srv")
	if err == nil {
		lease.Release()
		t.Fatal("expected lease acquisition to be rejected during drain/reconnect, but it succeeded")
	}

	// Release in-flight calls
	close(releaseInFlight)
	callWg.Wait()

	// Now drain should complete promptly
	select {
	case <-drainFinished:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("drain took too long to complete after in-flight calls finished")
	}
}

func TestManager_TimeoutCountedOnce(t *testing.T) {
	var callCount atomic.Int64
	factory := &fakeSessionFactory{
		createHook: func(srv config.ResolvedServer) (Session, io.Closer, error) {
			sess := newFakeSession(srv.ID, []*mcp.Tool{
				{Name: "hang", Description: "hang", InputSchema: map[string]any{"type": "object"}},
			}, nil)
			sess.callFn = func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
				callCount.Add(1)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2 * time.Second):
					return &mcp.CallToolResult{}, nil
				}
			}
			return sess, nil, nil
		},
	}

	mgr, _ := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfg := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				CallTimeout:    30 * time.Millisecond,
				MaxConcurrency: 4,
			},
		},
	}
	_ = mgr.Apply(ctx, cfg)
	time.Sleep(40 * time.Millisecond)

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "srv__hang"},
	}

	res, err := mgr.RouteTool(ctx, req)
	if err != nil {
		t.Fatalf("unexpected route error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true on timeout")
	}

	// Verify downstream was called exactly once (no retry/replay!)
	if callCount.Load() != 1 {
		t.Fatalf("expected downstream called exactly 1 time, got %d", callCount.Load())
	}
}

func TestManager_StartupConcurrencyBoundedTo4(t *testing.T) {
	factory := &fakeSessionFactory{
		createHook: func(srv config.ResolvedServer) (Session, io.Closer, error) {
			time.Sleep(30 * time.Millisecond)
			tools := []*mcp.Tool{
				{Name: "tool", Description: "t", InputSchema: map[string]any{"type": "object"}},
			}
			return newFakeSession(srv.ID, tools, nil), nil, nil
		},
	}

	mgr, _ := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	// Configure 8 servers simultaneously
	servers := make(map[string]config.ResolvedServer)
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("srv%d", i)
		servers[id] = config.ResolvedServer{
			ID:             id,
			Enabled:        true,
			Type:           config.ServerTypeStreamableHTTP,
			MaxConcurrency: 2,
		}
	}
	cfg := &config.ResolvedConfig{Servers: servers}

	_ = mgr.Apply(ctx, cfg)

	// Settle
	time.Sleep(150 * time.Millisecond)

	maxDials := factory.maxDials.Load()
	if maxDials > 4 {
		t.Fatalf("startup concurrency exceeded limit 4! Max observed dials was %d", maxDials)
	}
}

func TestManager_StatusPureScalars(t *testing.T) {
	factory := &fakeSessionFactory{}
	mgr, _ := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfg := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
			},
		},
	}
	_ = mgr.Apply(ctx, cfg)
	time.Sleep(40 * time.Millisecond)

	statuses := mgr.Status()
	st, ok := statuses["srv"]
	if !ok {
		t.Fatal("expected status for srv")
	}

	// Check fields are populated and safe
	if st.ID != "srv" || !st.Enabled || st.State != StateReady || st.MaxConcurrency != 4 {
		t.Fatalf("unexpected status values: %+v", st)
	}

	// Verify JSON marshaling succeeds without cycles or leaking pointers
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("JSON marshal of ServiceStatus failed: %v", err)
	}
	if !strings.Contains(string(b), `"state":"ready"`) {
		t.Fatalf("JSON missing state field: %s", string(b))
	}
}

func TestManager_BackoffResetAfterReady(t *testing.T) {
	failCount := atomic.Int32{}
	factory := &fakeSessionFactory{
		createHook: func(srv config.ResolvedServer) (Session, io.Closer, error) {
			if failCount.Add(1) <= 2 {
				return nil, nil, errors.New("initial failure")
			}
			tools := []*mcp.Tool{
				{Name: "tool", Description: "t", InputSchema: map[string]any{"type": "object"}},
			}
			return newFakeSession(srv.ID, tools, nil), nil, nil
		},
	}

	mgr, _ := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfg := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
			},
		},
	}
	_ = mgr.Apply(ctx, cfg)

	// Wait until it connects and enters StateReady
	time.Sleep(100 * time.Millisecond)

	status := mgr.Status()
	if status["srv"].State != StateReady {
		t.Fatalf("expected StateReady, got %s", status["srv"].State)
	}
	if status["srv"].ConsecutiveFailures < 2 {
		t.Fatalf("expected at least 2 consecutive failures recorded, got %d", status["srv"].ConsecutiveFailures)
	}

	// Wait for Ready reset interval (configured as 100ms in setupTestManager)
	time.Sleep(120 * time.Millisecond)

	statusAfter := mgr.Status()
	if statusAfter["srv"].ConsecutiveFailures != 0 {
		t.Fatalf("expected consecutiveFailures to be reset to 0 after staying Ready, got %d", statusAfter["srv"].ConsecutiveFailures)
	}
}

func TestManager_RefreshServer(t *testing.T) {
	var currentSession *fakeSession
	factory := &fakeSessionFactory{
		createHook: func(srv config.ResolvedServer) (Session, io.Closer, error) {
			currentSession = newFakeSession(srv.ID, []*mcp.Tool{
				{Name: "tool1", Description: "v1", InputSchema: map[string]any{"type": "object"}},
			}, nil)
			return currentSession, nil, nil
		},
	}

	mgr, cat := setupTestManager(factory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = mgr.Start(ctx)
	defer func() { _ = mgr.Stop(context.Background()) }()

	cfg := &config.ResolvedConfig{
		Servers: map[string]config.ResolvedServer{
			"srv": {
				ID:             "srv",
				Enabled:        true,
				Type:           config.ServerTypeStreamableHTTP,
				MaxConcurrency: 4,
			},
		},
	}
	_ = mgr.Apply(ctx, cfg)
	time.Sleep(40 * time.Millisecond)

	if mgr.Status()["srv"].PublishedTools != 1 {
		t.Fatalf("expected 1 published tool initially, got %d", mgr.Status()["srv"].PublishedTools)
	}

	// Downstream discovered another tool
	currentSession.SetTools([]*mcp.Tool{
		{Name: "tool1", Description: "v1", InputSchema: map[string]any{"type": "object"}},
		{Name: "tool2", Description: "v2", InputSchema: map[string]any{"type": "object"}},
	})

	// Trigger RefreshServer
	err := mgr.RefreshServer(ctx, "srv")
	if err != nil {
		t.Fatalf("RefreshServer failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if mgr.Status()["srv"].PublishedTools != 2 {
		t.Fatalf("expected 2 published tools after refresh, got %d", mgr.Status()["srv"].PublishedTools)
	}

	snap := cat.Snapshot()
	if _, ok := snap.LookupRoute("srv__tool2"); !ok {
		t.Fatal("srv__tool2 not found in catalog after refresh")
	}
}
