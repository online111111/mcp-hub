package inbound

import (
	"context"
	"errors"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/catalog"
)

var (
	// ErrNilServer indicates a nil MCP server was passed to NewPublisher.
	ErrNilServer = errors.New("mcp server cannot be nil")
)

// RouterCallback is the injected router callback invoked when a tool call arrives.
// The SDK AddTool handler delegates execution to this callback.
type RouterCallback func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error)

// Publisher coordinates the tool catalog with the official MCP Server (SDK v1.4.1).
// It maintains publishMu, registers raw tool handlers that call the injected RouterCallback,
// and installs the tools/list middleware to guarantee atomic list operations.
type Publisher struct {
	publishMu      sync.RWMutex
	server         *mcp.Server
	cat            *catalog.Catalog
	routerCallback RouterCallback
}

// NewPublisher creates a new Publisher wrapping the given SDK Server.
// It installs the tools/list middleware to protect concurrent listings during publish transactions.
// If cat is nil, a new Catalog is created automatically.
func NewPublisher(server *mcp.Server, cat *catalog.Catalog, router RouterCallback) (*Publisher, error) {
	if server == nil {
		return nil, ErrNilServer
	}
	if cat == nil {
		cat = catalog.NewCatalog()
	}

	p := &Publisher{
		server:         server,
		cat:            cat,
		routerCallback: router,
	}

	p.installListMiddleware()
	return p, nil
}

// installListMiddleware wraps receiving method handlers with publishMu.RLock for "tools/list".
// This ensures callers never observe a partial catalog while PublishServer or RemoveServer is updating.
func (p *Publisher) installListMiddleware() {
	p.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				p.publishMu.RLock()
				defer p.publishMu.RUnlock()
				return next(ctx, method, req)
			}
			return next(ctx, method, req)
		}
	})
}

// PublishServer atomically updates the tools for a server.
// It acquires publishMu.Lock, applies naming, validation, filtering and capacity bounds,
// updates routes in the catalog, applies server.RemoveTools and server.AddTool,
// and increments the catalog revision.
func (p *Publisher) PublishServer(serverID string, rawTools []*mcp.Tool, disabled []string) (*catalog.Snapshot, error) {
	p.publishMu.Lock()
	defer p.publishMu.Unlock()

	toAdd, toRemove, snap, err := p.cat.UpdateServerTools(serverID, rawTools, disabled)
	if err != nil {
		return snap, err
	}

	// Remove tools that are no longer published
	if len(toRemove) > 0 {
		p.server.RemoveTools(toRemove...)
	}

	// Add newly published or updated tools
	for _, tool := range toAdd {
		p.server.AddTool(tool, p.dispatchTool)
	}

	return snap, nil
}

// RemoveServer atomically removes all tools associated with serverID.
func (p *Publisher) RemoveServer(serverID string) (*catalog.Snapshot, error) {
	p.publishMu.Lock()
	defer p.publishMu.Unlock()

	toRemove, snap := p.cat.RemoveServer(serverID)
	if len(toRemove) > 0 {
		p.server.RemoveTools(toRemove...)
	}

	return snap, nil
}

// dispatchTool is the raw tool handler registered with Server.AddTool.
// It delegates tool calls to the injected routerCallback without holding publishMu.
func (p *Publisher) dispatchTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if p.routerCallback == nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{Text: "tool router callback not configured"},
			},
		}, nil
	}
	return p.routerCallback(ctx, req)
}

// SetRouterCallback updates the injected router callback.
func (p *Publisher) SetRouterCallback(cb RouterCallback) {
	p.publishMu.Lock()
	defer p.publishMu.Unlock()
	p.routerCallback = cb
}

// RLock acquires the read lock on publishMu.
// Router uses this before inspecting routes and obtaining generation leases.
func (p *Publisher) RLock() {
	p.publishMu.RLock()
}

// RUnlock releases the read lock on publishMu.
func (p *Publisher) RUnlock() {
	p.publishMu.RUnlock()
}

// PublishMu returns the internal publish mutex for components that require explicit lockers.
func (p *Publisher) PublishMu() *sync.RWMutex {
	return &p.publishMu
}

// LookupRoute resolves a public tool name to its RouteEntry.
func (p *Publisher) LookupRoute(publicName string) (catalog.RouteEntry, bool) {
	return p.cat.LookupRoute(publicName)
}

// Snapshot returns the latest immutable catalog snapshot.
func (p *Publisher) Snapshot() *catalog.Snapshot {
	return p.cat.Snapshot()
}

// Revision returns the current catalog revision.
func (p *Publisher) Revision() int64 {
	return p.cat.Revision()
}

// Server returns the underlying MCP server.
func (p *Publisher) Server() *mcp.Server {
	return p.server
}

// Catalog returns the underlying Catalog.
func (p *Publisher) Catalog() *catalog.Catalog {
	return p.cat
}
