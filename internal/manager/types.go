package manager

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"mcp-hub/internal/catalog"
	"mcp-hub/internal/config"
	"mcp-hub/internal/router"
)

// Server lifecycle states matching Agent实施手册 and 主开发设计文档.
const (
	StateDisabled    = "disabled"
	StateConnecting  = "connecting"
	StateReady       = "ready"
	StateUnavailable = "unavailable"
	StateBackoff     = "backoff"
	StateDraining    = "draining"
	StateClosed      = "closed"
)

var (
	// ErrServerNotFound is returned when attempting to lease an unknown or unconfigured server.
	ErrServerNotFound = router.ErrServerNotFound

	// ErrNotAdmitting is returned when the target server generation is not admitting calls.
	ErrNotAdmitting = router.ErrServerUnavailable

	// ErrConcurrencyLimit is returned when active leases reach maxConcurrency.
	ErrConcurrencyLimit = router.ErrServerBusy

	// ErrManagerStopped indicates operations attempted on a stopped manager.
	ErrManagerStopped = errors.New("manager: already stopped")
)

// DesiredServer pairs a configuration revision with the resolved server configuration.
type DesiredServer struct {
	Revision       int64
	ResolvedConfig config.ResolvedServer
}

// Route defines the mapping from public name to upstream server and original tool name.
type Route struct {
	PublicName   string `json:"publicName"`
	ServerID     string `json:"serverId"`
	OriginalName string `json:"originalName"`
}

// FromCatalogRoute converts catalog.RouteEntry to Route.
func FromCatalogRoute(entry catalog.RouteEntry) Route {
	return Route{
		PublicName:   entry.PublicName,
		ServerID:     entry.ServerID,
		OriginalName: entry.OriginalName,
	}
}

// ToCatalogRoute converts Route to catalog.RouteEntry.
func (r Route) ToCatalogRoute() catalog.RouteEntry {
	return catalog.RouteEntry{
		PublicName:   r.PublicName,
		ServerID:     r.ServerID,
		OriginalName: r.OriginalName,
	}
}

// ServiceStatus holds pure scalar, security-safe diagnostics for a managed downstream server.
// No pointers, mutexes, raw sessions, headers, or credentials are leaked.
type ServiceStatus struct {
	ID                  string    `json:"id"`
	Enabled             bool      `json:"enabled"`
	State               string    `json:"state"`
	DesiredRevision     int64     `json:"desiredRevision"`
	GenerationID        uint64    `json:"generationId"`
	ActiveRevision      int64     `json:"activeRevision"`
	ActiveLeases        int       `json:"activeLeases"`
	MaxConcurrency      int       `json:"maxConcurrency"`
	LastError           string    `json:"lastError,omitempty"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	LastConnectedAt     time.Time `json:"lastConnectedAt,omitempty"`
	LastDisconnectAt    time.Time `json:"lastDisconnectAt,omitempty"`
	PublishedTools      int       `json:"publishedTools"`
}

// Lease binds an active tool invocation to a specific Generation.
// It implements router.Lease.
type Lease struct {
	gen         *Generation
	releaseOnce sync.Once
}

// Release relinquishes the lease on the generation.
// It locks ONLY generation.mu and never acquires publishMu or coordinator mutexes.
func (l *Lease) Release() {
	l.releaseOnce.Do(func() {
		if l.gen != nil {
			l.gen.release()
		}
	})
}

// Session returns the downstream session bound to this lease.
func (l *Lease) Session() router.Session {
	return l.gen.session
}

// ServerID returns the server ID.
func (l *Lease) ServerID() string {
	return l.gen.serverID
}

// GenerationID returns the generation ID.
func (l *Lease) GenerationID() uint64 {
	return l.gen.id
}

// CallTimeout returns the call timeout configured for this generation.
func (l *Lease) CallTimeout() time.Duration {
	return l.gen.callTimeout
}

// GenerationDone closes when the bound generation is forcefully stopped.
func (l *Lease) GenerationDone() <-chan struct{} {
	return l.gen.ctx.Done()
}

// Generation represents one connected downstream instance with an atomic lease counter.
// A Lease acquired against a generation remains bound to that generation across reloads.
type Generation struct {
	id             uint64
	serverID       string
	revision       int64
	session        Session
	processCloser  io.Closer
	maxConcurrency int
	callTimeout    time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	closeOnce      sync.Once

	mu        sync.Mutex
	admission bool
	active    int
	drainCh   chan struct{}
	closed    bool
}

// newGeneration creates an initialized Generation with admission open.
func newGeneration(
	id uint64,
	serverID string,
	session Session,
	processCloser io.Closer,
	maxConcurrency int,
	callTimeout time.Duration,
	ctx context.Context,
	cancel context.CancelFunc,
	revision int64,
) *Generation {
	if maxConcurrency <= 0 {
		maxConcurrency = config.DefaultMaxConcurrency
	}
	return &Generation{
		id:             id,
		serverID:       serverID,
		revision:       revision,
		session:        session,
		processCloser:  processCloser,
		maxConcurrency: maxConcurrency,
		callTimeout:    callTimeout,
		ctx:            ctx,
		cancel:         cancel,
		admission:      true,
		active:         0,
		drainCh:        make(chan struct{}, 1),
	}
}

// acquire attempts to take an active lease on the generation.
// Non-blocking: returns ErrConcurrencyLimit if active >= maxConcurrency.
func (g *Generation) acquire() (*Lease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.admission || g.closed {
		return nil, ErrNotAdmitting
	}
	if g.active >= g.maxConcurrency {
		return nil, ErrConcurrencyLimit
	}

	g.active++
	return &Lease{gen: g}, nil
}

// release decrements active lease count.
// It acquires only g.mu.
func (g *Generation) release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.active--
	if g.active <= 0 {
		g.active = 0
		select {
		case g.drainCh <- struct{}{}:
		default:
		}
	}
}

// drain closes admission and waits for active leases to reach zero or timeout.
func (g *Generation) drain(timeout time.Duration) {
	g.mu.Lock()
	g.admission = false
	if g.active == 0 {
		g.mu.Unlock()
		return
	}
	g.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		g.mu.Lock()
		if g.active == 0 {
			g.mu.Unlock()
			return
		}
		g.mu.Unlock()

		select {
		case <-g.drainCh:
		case <-timer.C:
			return
		}
	}
}

// Close shuts down the generation, cancels active child contexts, and closes session/process.
func (g *Generation) Close() error {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.admission = false
		g.closed = true
		g.mu.Unlock()

		if g.cancel != nil {
			g.cancel()
		}
		if g.session != nil {
			_ = g.session.Close()
		}
		if g.processCloser != nil {
			_ = g.processCloser.Close()
		}
	})
	return nil
}

// Active returns current active lease count.
func (g *Generation) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

// IsAdmitting reports whether the generation is accepting new calls.
func (g *Generation) IsAdmitting() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.admission && !g.closed
}

// IsClosed reports whether the generation has been closed.
func (g *Generation) IsClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// SetCallTimeout updates the call timeout for future calls on this generation.
func (g *Generation) SetCallTimeout(d time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.callTimeout = d
}
