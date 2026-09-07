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
	ErrServerNotFound = router.ErrServerNotFound
	ErrNotAdmitting = router.ErrServerUnavailable
	ErrConcurrencyLimit = router.ErrServerBusy
	ErrManagerStopped = errors.New("manager: already stopped")
)

type DesiredServer struct {
	Revision       int64
	ResolvedConfig config.ResolvedServer
}

type Route struct {
	PublicName   string `json:"publicName"`
	ServerID     string `json:"serverId"`
	OriginalName string `json:"originalName"`
}

func FromCatalogRoute(entry catalog.RouteEntry) Route {
	return Route{PublicName: entry.PublicName, ServerID: entry.ServerID, OriginalName: entry.OriginalName}
}

func (r Route) ToCatalogRoute() catalog.RouteEntry {
	return catalog.RouteEntry{PublicName: r.PublicName, ServerID: r.ServerID, OriginalName: r.OriginalName}
}

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

type Lease struct {
	gen         *Generation
	releaseOnce sync.Once
}

func (l *Lease) Release() {
	l.releaseOnce.Do(func() {
		if l.gen != nil {
			l.gen.release()
		}
	})
}

func (l *Lease) Session() router.Session { return l.gen.session }
func (l *Lease) ServerID() string { return l.gen.serverID }
func (l *Lease) GenerationID() uint64 { return l.gen.id }

func (l *Lease) CallTimeout() time.Duration {
	if l == nil || l.gen == nil {
		return 0
	}
	l.gen.mu.Lock()
	defer l.gen.mu.Unlock()
	return l.gen.callTimeout
}

func (l *Lease) GenerationDone() <-chan struct{} { return l.gen.ctx.Done() }

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
		id: id,
		serverID: serverID,
		revision: revision,
		session: session,
		processCloser: processCloser,
		maxConcurrency: maxConcurrency,
		callTimeout: callTimeout,
		ctx: ctx,
		cancel: cancel,
		admission: true,
		active: 0,
		drainCh: make(chan struct{}, 1),
	}
}

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

func (g *Generation) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

func (g *Generation) IsAdmitting() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.admission && !g.closed
}

func (g *Generation) IsClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

func (g *Generation) SetCallTimeout(d time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.callTimeout = d
}
