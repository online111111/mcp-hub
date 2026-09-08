package manager

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/online111111/mcp-manager/internal/config"
	"io"
	"testing"
	"time"
)

func TestEmptyEnvValuesStillCompareKeyIdentity(t *testing.T) {
	if mapsEqual(map[string]string{"A": ""}, map[string]string{"B": ""}) {
		t.Fatal("different empty-valued keys compared equal")
	}
}

func TestParentCancellationStopsDisabledCoordinator(t *testing.T) {
	c := newCoordinator("audit", DesiredServer{Revision: 1, ResolvedConfig: config.ResolvedServer{ID: "audit", Enabled: false}}, NewCatalogPublisher(nil), &fakeSessionFactory{}, make(chan struct{}, 1), nil, 0, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer c.Stop()
	c.Start(ctx)
	cancel()
	select {
	case <-c.stoppedCh:
	case <-time.After(time.Second):
		t.Fatal("coordinator keeps looping after parent cancellation")
	}
}

type auditLifetimeFactory struct{ contexts chan context.Context }

func (f *auditLifetimeFactory) CreateSession(ctx context.Context, srv config.ResolvedServer, _ func()) (Session, io.Closer, error) {
	f.contexts <- ctx
	return newFakeSession(srv.ID, []*mcp.Tool{{Name: "ping", InputSchema: map[string]any{"type": "object"}}}, nil), nil, nil
}

func TestHotTimeoutChangeDoesNotCancelLiveSession(t *testing.T) {
	f := &auditLifetimeFactory{contexts: make(chan context.Context, 4)}
	cfg := config.ResolvedServer{ID: "audit", Enabled: true, Type: config.ServerTypeStdio, Command: "node", CallTimeout: time.Second, StartupTimeout: time.Second, MaxConcurrency: 1}
	c := newCoordinator("audit", DesiredServer{Revision: 1, ResolvedConfig: cfg}, NewCatalogPublisher(nil), f, make(chan struct{}, 1), nil, 0, time.Millisecond)
	defer c.Stop()
	c.Start(context.Background())
	var sessionCtx context.Context
	select {
	case sessionCtx = <-f.contexts:
	case <-time.After(time.Second):
		t.Fatal("session did not start")
	}
	until := time.Now().Add(time.Second)
	for c.Status().State != StateReady && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if c.Status().State != StateReady {
		t.Fatal("session not ready")
	}
	cfg.CallTimeout = 2 * time.Second
	c.UpdateConfig(DesiredServer{Revision: 2, ResolvedConfig: cfg})
	if sessionCtx.Err() != nil {
		t.Fatal("hot timeout edit cancelled live connection")
	}
}
