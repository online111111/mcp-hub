package manager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/online111111/mcp-manager/internal/config"
)

type preflightSession struct {
	tools  []*mcp.Tool
	closed atomic.Bool
}

func (s *preflightSession) CallTool(context.Context, string, json.RawMessage) (*mcp.CallToolResult, error) {
	return nil, errors.New("not used")
}
func (s *preflightSession) ListAllTools(context.Context) ([]*mcp.Tool, error) { return s.tools, nil }
func (s *preflightSession) Close() error                                      { s.closed.Store(true); return nil }
func (s *preflightSession) IsClosed() bool                                    { return s.closed.Load() }

type preflightFactory struct {
	calls atomic.Int32
	fail  bool
	tools []*mcp.Tool
}

func (f *preflightFactory) CreateSession(context.Context, config.ResolvedServer, func()) (Session, io.Closer, error) {
	f.calls.Add(1)
	if f.fail {
		return nil, nil, errors.New("connection refused secret=must-not-leak")
	}
	return &preflightSession{tools: f.tools}, nil, nil
}

func TestManagerPreflightSkipsUnchangedAndDisabledServers(t *testing.T) {
	factory := &preflightFactory{tools: []*mcp.Tool{{Name: "ok", InputSchema: map[string]any{"type": "object"}}}}
	mgr := NewManager(nil, WithSessionFactory(factory))
	mgr.coordinators["same"] = newCoordinator("same", DesiredServer{Revision: 1, ResolvedConfig: config.ResolvedServer{
		ID: "same", Enabled: true, Type: config.ServerTypeStreamableHTTP, URL: "https://example.test/mcp", StartupTimeout: time.Second,
	}}, mgr.pub, factory, mgr.limiter, nil, 0, 0)

	cfg := &config.ResolvedConfig{Servers: map[string]config.ResolvedServer{
		"same": {ID: "same", Enabled: true, Type: config.ServerTypeStreamableHTTP, URL: "https://example.test/mcp", StartupTimeout: time.Second},
		"off":  {ID: "off", Enabled: false, Type: config.ServerTypeStreamableHTTP, URL: "https://off.example/mcp", StartupTimeout: time.Second},
	}}
	if err := mgr.Preflight(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := factory.calls.Load(); got != 0 {
		t.Fatalf("unexpected probe count: %d", got)
	}
}

func TestManagerPreflightProbesChangedServerAndSanitizesFailure(t *testing.T) {
	factory := &preflightFactory{fail: true}
	mgr := NewManager(nil, WithSessionFactory(factory))
	cfg := &config.ResolvedConfig{Servers: map[string]config.ResolvedServer{
		"new": {ID: "new", Enabled: true, Type: config.ServerTypeStreamableHTTP, URL: "https://example.test/mcp", StartupTimeout: time.Second},
	}}
	err := mgr.Preflight(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected preflight failure")
	}
	if got := err.Error(); got != `server "new" preflight failed: connection_refused` {
		t.Fatalf("unexpected sanitized error: %q", got)
	}
}

func TestManagerPreflightRejectsInvalidDiscoveredToolCatalog(t *testing.T) {
	factory := &preflightFactory{tools: []*mcp.Tool{{Name: "bad-schema", InputSchema: map[string]any{"type": "array"}}}}
	mgr := NewManager(nil, WithSessionFactory(factory))
	cfg := &config.ResolvedConfig{Servers: map[string]config.ResolvedServer{
		"new": {ID: "new", Enabled: true, Type: config.ServerTypeStreamableHTTP, URL: "https://example.test/mcp", StartupTimeout: time.Second},
	}}
	if err := mgr.Preflight(context.Background(), cfg); err == nil {
		t.Fatal("expected invalid catalog preflight failure")
	}
}
