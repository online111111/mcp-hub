package manager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/catalog"
	"mcp-hub/internal/config"
	"mcp-hub/internal/inbound"
	"mcp-hub/internal/router"
)

const (
	defaultDialConcurrency = 4
)

// CatalogPublisherAdapter adapts *catalog.Catalog to catalog.Publisher.
type CatalogPublisherAdapter struct {
	cat *catalog.Catalog
	mu  sync.RWMutex
}

// NewCatalogPublisher wraps a *catalog.Catalog as a catalog.Publisher.
func NewCatalogPublisher(cat *catalog.Catalog) catalog.Publisher {
	if cat == nil {
		cat = catalog.NewCatalog()
	}
	return &CatalogPublisherAdapter{cat: cat}
}

func (a *CatalogPublisherAdapter) PublishServer(serverID string, rawTools []*mcp.Tool, disabled []string) (*catalog.Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _, snap, err := a.cat.UpdateServerTools(serverID, rawTools, disabled)
	return snap, err
}

func (a *CatalogPublisherAdapter) RemoveServer(serverID string) (*catalog.Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, snap := a.cat.RemoveServer(serverID)
	return snap, nil
}

func (a *CatalogPublisherAdapter) LookupRoute(publicName string) (catalog.RouteEntry, bool) {
	return a.cat.LookupRoute(publicName)
}

func (a *CatalogPublisherAdapter) Snapshot() *catalog.Snapshot {
	return a.cat.Snapshot()
}

func (a *CatalogPublisherAdapter) Revision() int64 {
	return a.cat.Revision()
}

func (a *CatalogPublisherAdapter) RLock() {
	a.mu.RLock()
}

func (a *CatalogPublisherAdapter) RUnlock() {
	a.mu.RUnlock()
}

func (a *CatalogPublisherAdapter) Catalog() *catalog.Catalog {
	return a.cat
}

// Manager coordinates downstream MCP servers, their generations, lifecycle,
// reload transitions, lease admission, tool routing, and status inspection.
type Manager struct {
	mu           sync.RWMutex
	pub          catalog.Publisher
	router       *router.Router
	factory      SessionFactory
	coordinators map[string]*Coordinator
	limiter      chan struct{}
	revision     int64

	backoffDelays []time.Duration
	readyResetDur time.Duration
	drainTimeout  time.Duration

	// Bounded, sanitized diagnostics owned by the routing manager.
	recentCalls    []inbound.RecentCallDTO
	maxRecentCalls int

	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	stopped bool
}

// NewManager creates a new Manager instance wired to the given Publisher.
func NewManager(pub catalog.Publisher, opts ...Option) *Manager {
	if pub == nil {
		pub = NewCatalogPublisher(catalog.NewCatalog())
	}

	m := &Manager{
		pub:            pub,
		factory:        NewDefaultSessionFactory(),
		coordinators:   make(map[string]*Coordinator),
		limiter:        make(chan struct{}, defaultDialConcurrency),
		backoffDelays:  defaultBackoffDelays,
		readyResetDur:  defaultReadyResetDuration,
		drainTimeout:   defaultDrainTimeout,
		maxRecentCalls: 200,
	}

	for _, opt := range opts {
		opt(m)
	}

	m.router = router.NewRouter(pub, m)
	m.router.SetCallRecorder(func(call router.CallRecord) {
		m.RecordCall(inbound.RecentCallDTO{
			RequestID:     call.RequestID,
			Time:          call.Time.Format(time.RFC3339Nano),
			DurationMs:    call.Duration.Milliseconds(),
			Tool:          call.Tool,
			ServerID:      call.ServerID,
			Outcome:       call.Outcome,
			ErrorCategory: call.ErrorCategory,
		})
	})
	return m
}

// Router returns the underlying Router instance.
func (m *Manager) Router() *router.Router {
	return m.router
}

// SetMaxRecentCalls changes the bounded diagnostics ring size. Values above the
// product limit are capped, and non-positive values are ignored.
func (m *Manager) SetMaxRecentCalls(n int) {
	if n <= 0 {
		return
	}
	if n > config.MaxRecentCalls {
		n = config.MaxRecentCalls
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxRecentCalls = n
	if len(m.recentCalls) > n {
		m.recentCalls = m.recentCalls[len(m.recentCalls)-n:]
	}
}

// Start initiates the manager and starts background coordinators.
func (m *Manager) Start(parentCtx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return ErrManagerStopped
	}
	if m.running {
		return nil
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	m.ctx, m.cancel = context.WithCancel(parentCtx)
	m.running = true

	for _, coord := range m.coordinators {
		coord.Start(m.ctx)
	}

	return nil
}

// Stop stops the manager and all managed coordinators gracefully, draining active leases.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	m.stopped = true
	m.running = false
	if m.cancel != nil {
		m.cancel()
	}
	coords := make([]*Coordinator, 0, len(m.coordinators))
	for _, c := range m.coordinators {
		coords = append(coords, c)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, c := range coords {
		wg.Add(1)
		go func(coord *Coordinator) {
			defer wg.Done()
			coord.Stop()
		}(c)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Apply reconciles the running coordinators with the given resolved configuration.
// It handles new servers, in-place updates, break-before-make reloads, and removals.
func (m *Manager) Apply(ctx context.Context, cfg *config.ResolvedConfig) error {
	if cfg == nil {
		return errors.New("manager: resolved config cannot be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return ErrManagerStopped
	}

	m.revision++
	rev := m.revision
	// 1. Identify deleted servers
	toRemove := make(map[string]*Coordinator)
	for id, coord := range m.coordinators {
		if _, exists := cfg.Servers[id]; !exists {
			toRemove[id] = coord
			delete(m.coordinators, id)
		}
	}

	// 2. Identify new and updated servers
	for id, srv := range cfg.Servers {
		desired := DesiredServer{
			Revision:       rev,
			ResolvedConfig: srv,
		}

		coord, exists := m.coordinators[id]
		if !exists {
			coord = newCoordinator(
				id,
				desired,
				m.pub,
				m.factory,
				m.limiter,
				m.backoffDelays,
				m.readyResetDur,
				m.drainTimeout,
			)
			m.coordinators[id] = coord
			if m.running {
				coord.Start(m.ctx)
			}
		} else {
			coord.UpdateConfig(desired)
		}
	}
	m.mu.Unlock()

	// 3. Revoke deleted servers from the public catalog before draining them.
	for id := range toRemove {
		if m.pub != nil {
			_, _ = m.pub.RemoveServer(id)
		}
	}

	// Drain removed coordinators concurrently so a multi-server reload is bounded
	// by one drain interval instead of one interval per server.
	var stopWG sync.WaitGroup
	for _, coord := range toRemove {
		stopWG.Add(1)
		go func(c *Coordinator) {
			defer stopWG.Done()
			c.Stop()
		}(coord)
	}
	stopped := make(chan struct{})
	go func() {
		stopWG.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RefreshServer triggers tool re-discovery for the specified server ID.
func (m *Manager) RefreshServer(ctx context.Context, serverID string) error {
	m.mu.RLock()
	coord, ok := m.coordinators[serverID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %q", ErrServerNotFound, serverID)
	}

	return coord.Refresh(ctx)
}

// AcquireLease retrieves a generation lease for the target server.
// Implements router.LeaseProvider.
func (m *Manager) AcquireLease(serverID string) (router.Lease, error) {
	m.mu.RLock()
	if m.stopped {
		m.mu.RUnlock()
		return nil, ErrManagerStopped
	}
	coord, ok := m.coordinators[serverID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrServerNotFound
	}

	return coord.AcquireLease()
}

// RouteTool handles tool routing compatible with inbound.Publisher.RouterCallback.
func (m *Manager) RouteTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return m.router.RouteTool(ctx, req)
}

// Status returns a scalar snapshot of all managed downstream servers.
func (m *Manager) Status() map[string]ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string]ServiceStatus, len(m.coordinators))
	for id, coord := range m.coordinators {
		res[id] = coord.Status()
	}
	return res
}

// IsReady reports readiness: true if no servers are enabled, or at least one is Ready with tools.
func (m *Manager) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	enabledCount := 0
	hasReadyWithTools := false

	for _, coord := range m.coordinators {
		st := coord.Status()
		if st.Enabled {
			enabledCount++
			if st.State == StateReady && st.PublishedTools > 0 {
				hasReadyWithTools = true
			}
		}
	}

	if enabledCount == 0 {
		return true
	}
	return hasReadyWithTools
}

// GetServerStatuses returns sanitized diagnostic summaries for all managed servers.
func (m *Manager) GetServerStatuses() []inbound.ServerStatusDTO {
	m.mu.RLock()
	coordinators := make([]*Coordinator, 0, len(m.coordinators))
	for _, coord := range m.coordinators {
		coordinators = append(coordinators, coord)
	}
	m.mu.RUnlock()

	res := make([]inbound.ServerStatusDTO, 0, len(coordinators))
	for _, coord := range coordinators {
		st := coord.Status()
		res = append(res, inbound.ServerStatusDTO{
			ID:                   st.ID,
			State:                st.State,
			PublishedToolCount:   st.PublishedTools,
			UnpublishedToolCount: 0,
			ActiveCalls:          st.ActiveLeases,
			DesiredRevision:      st.DesiredRevision,
			ActiveRevision:       st.ActiveRevision,
			ErrorCategory:        st.LastError,
		})
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].ID < res[j].ID
	})
	return res
}

// GetRecentCalls returns recorded recent call summaries.
func (m *Manager) GetRecentCalls() []inbound.RecentCallDTO {
	m.mu.RLock()
	defer m.mu.RUnlock()

	copied := make([]inbound.RecentCallDTO, len(m.recentCalls))
	copy(copied, m.recentCalls)
	return copied
}

// RecordCall records a tool call summary for diagnostics.
func (m *Manager) RecordCall(call inbound.RecentCallDTO) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.maxRecentCalls <= 0 {
		m.maxRecentCalls = config.MaxRecentCalls
	}
	m.recentCalls = append(m.recentCalls, call)
	if m.maxRecentCalls > config.MaxRecentCalls {
		m.maxRecentCalls = config.MaxRecentCalls
	}
	if len(m.recentCalls) > m.maxRecentCalls {
		m.recentCalls = m.recentCalls[len(m.recentCalls)-m.maxRecentCalls:]
	}
}
