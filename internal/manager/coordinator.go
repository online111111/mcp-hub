package manager

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/catalog"
	"mcp-hub/internal/config"
)

type configChangeType int

const (
	changeNone configChangeType = iota
	changeToolsOnly
	changeTimeoutOnly
	changeToolsAndTimeout
	changeConnection
)

// Default backoff progression and ready reset duration.
var (
	defaultBackoffDelays = []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
	}
	defaultReadyResetDuration = 60 * time.Second
	defaultDrainTimeout       = 10 * time.Second
)

// Coordinator manages the lifecycle, backoff, reconnection, and generation of one downstream server.
type Coordinator struct {
	serverID string
	pub      catalog.Publisher
	factory  SessionFactory
	limiter  chan struct{}

	backoffDelays []time.Duration
	readyResetDur time.Duration
	drainTimeout  time.Duration

	mu              sync.Mutex
	desired         DesiredServer
	currentGen      *Generation
	state           string
	consecutiveFail int
	lastError       string
	lastConnected   time.Time
	lastDisconnect  time.Time
	publishedCount  int
	cachedTools     []*mcp.Tool
	everSucceeded   bool
	genSeq          uint64

	ctx           context.Context
	cancel        context.CancelFunc
	reconcileCh   chan struct{}
	toolChangedCh chan struct{}
	refreshCh     chan chan error
	stopped       bool
	stoppedCh     chan struct{}
}

// newCoordinator creates a new Coordinator for serverID.
func newCoordinator(
	serverID string,
	desired DesiredServer,
	pub catalog.Publisher,
	factory SessionFactory,
	limiter chan struct{},
	backoffDelays []time.Duration,
	readyResetDur time.Duration,
	drainTimeout time.Duration,
) *Coordinator {
	if len(backoffDelays) == 0 {
		backoffDelays = defaultBackoffDelays
	}
	if readyResetDur <= 0 {
		readyResetDur = defaultReadyResetDuration
	}
	if drainTimeout <= 0 {
		drainTimeout = defaultDrainTimeout
	}

	c := &Coordinator{
		serverID:      serverID,
		desired:       desired,
		pub:           pub,
		factory:       factory,
		limiter:       limiter,
		backoffDelays: backoffDelays,
		readyResetDur: readyResetDur,
		drainTimeout:  drainTimeout,
		state:         StateDisabled,
		reconcileCh:   make(chan struct{}, 1),
		toolChangedCh: make(chan struct{}, 1),
		refreshCh:     make(chan chan error, 1),
		stoppedCh:     make(chan struct{}),
	}
	if desired.ResolvedConfig.Enabled {
		c.state = StateConnecting
	}
	return c
}

// Start launches the coordinator's event loop.
func (c *Coordinator) Start(parentCtx context.Context) {
	c.mu.Lock()
	if c.ctx != nil {
		c.mu.Unlock()
		return
	}
	c.ctx, c.cancel = context.WithCancel(parentCtx)
	c.mu.Unlock()

	go c.run()
}

// Stop stops the coordinator, drains active leases, and cleans up.
func (c *Coordinator) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	if c.cancel != nil {
		c.cancel()
	}
	gen := c.currentGen
	c.currentGen = nil
	c.state = StateClosed
	c.mu.Unlock()

	if gen != nil {
		gen.drain(c.drainTimeout)
		_ = gen.Close()
	}

	close(c.stoppedCh)
}

// UpdateConfig applies a new DesiredServer configuration to the coordinator.
// Returns true if a full reconnect/break-before-make was initiated, or false if handled in-place.
func (c *Coordinator) UpdateConfig(newDesired DesiredServer) bool {
	c.mu.Lock()
	oldCfg := c.desired.ResolvedConfig
	newCfg := newDesired.ResolvedConfig
	change := diffConfig(oldCfg, newCfg)

	switch change {
	case changeNone:
		c.mu.Unlock()
		return false

	case changeToolsOnly:
		c.desired = newDesired
		tools := c.cachedTools
		state := c.state
		c.mu.Unlock()

		if state == StateReady && len(tools) > 0 && c.pub != nil {
			var disabled []string
			for d := range newCfg.DisabledTools {
				disabled = append(disabled, d)
			}
			snap, err := c.pub.PublishServer(c.serverID, tools, disabled)
			if err == nil && snap != nil {
				c.mu.Lock()
				c.publishedCount = snap.PublishedCount(c.serverID)
				c.mu.Unlock()
			}
		}
		return false

	case changeTimeoutOnly:
		c.desired = newDesired
		if c.currentGen != nil {
			c.currentGen.SetCallTimeout(newCfg.CallTimeout)
		}
		c.mu.Unlock()
		return false

	case changeToolsAndTimeout:
		c.desired = newDesired
		if c.currentGen != nil {
			c.currentGen.SetCallTimeout(newCfg.CallTimeout)
		}
		tools := c.cachedTools
		state := c.state
		c.mu.Unlock()

		if state == StateReady && len(tools) > 0 && c.pub != nil {
			var disabled []string
			for d := range newCfg.DisabledTools {
				disabled = append(disabled, d)
			}
			snap, err := c.pub.PublishServer(c.serverID, tools, disabled)
			if err == nil && snap != nil {
				c.mu.Lock()
				c.publishedCount = snap.PublishedCount(c.serverID)
				c.mu.Unlock()
			}
		}
		return false

	case changeConnection:
		c.desired = newDesired
		c.mu.Unlock()

		// Trigger break-before-make reconciliation
		select {
		case c.reconcileCh <- struct{}{}:
		default:
		}
		return true

	default:
		c.mu.Unlock()
		return false
	}
}

// Refresh triggers an asynchronous or synchronous discovery refresh.
func (c *Coordinator) Refresh(ctx context.Context) error {
	errCh := make(chan error, 1)
	select {
	case c.refreshCh <- errCh:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stoppedCh:
		return ErrManagerStopped
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stoppedCh:
		return ErrManagerStopped
	}
}

// AcquireLease attempts to get an active lease on current generation.
func (c *Coordinator) AcquireLease() (*Lease, error) {
	c.mu.Lock()
	gen := c.currentGen
	state := c.state
	c.mu.Unlock()

	if state != StateReady || gen == nil {
		return nil, ErrNotAdmitting
	}

	return gen.acquire()
}

// Status returns a scalar snapshot of the server's state.
func (c *Coordinator) Status() ServiceStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	active := 0
	var genID uint64
	var activeRevision int64
	if c.currentGen != nil {
		active = c.currentGen.Active()
		genID = c.currentGen.id
		activeRevision = c.currentGen.revision
	}

	return ServiceStatus{
		ID:                  c.serverID,
		Enabled:             c.desired.ResolvedConfig.Enabled,
		State:               c.state,
		DesiredRevision:     c.desired.Revision,
		GenerationID:        genID,
		ActiveRevision:      activeRevision,
		ActiveLeases:        active,
		MaxConcurrency:      c.desired.ResolvedConfig.MaxConcurrency,
		LastError:           c.lastError,
		ConsecutiveFailures: c.consecutiveFail,
		LastConnectedAt:     c.lastConnected,
		LastDisconnectAt:    c.lastDisconnect,
		PublishedTools:      c.publishedCount,
	}
}

// onToolListChanged is invoked when downstream fires tools/list_changed.
// Non-blocking signal coalescing.
func (c *Coordinator) onToolListChanged() {
	select {
	case c.toolChangedCh <- struct{}{}:
	default:
	}
}

// run is the main coordinator event loop.
func (c *Coordinator) run() {
	for {
		c.mu.Lock()
		if c.stopped {
			c.mu.Unlock()
			return
		}
		enabled := c.desired.ResolvedConfig.Enabled
		c.mu.Unlock()

		if !enabled {
			c.handleDisabled()
			continue
		}

		c.handleConnectingAndReady()
	}
}

// handleDisabled manages the server when disabled in configuration.
func (c *Coordinator) handleDisabled() {
	c.mu.Lock()
	c.state = StateDisabled
	gen := c.currentGen
	c.currentGen = nil
	c.mu.Unlock()

	// Revoke public discovery before draining the old generation so clients do
	// not see a tool that no longer admits new calls.
	if c.pub != nil {
		_, _ = c.pub.RemoveServer(c.serverID)
	}

	if gen != nil {
		gen.drain(c.drainTimeout)
		_ = gen.Close()
	}

	select {
	case <-c.reconcileCh:
		// Configuration changed, re-evaluate
		return
	case ch := <-c.refreshCh:
		ch <- fmt.Errorf("server %q is disabled", c.serverID)
	case <-c.ctx.Done():
		return
	}
}

// handleConnectingAndReady connects to downstream and runs the Ready loop.
func (c *Coordinator) handleConnectingAndReady() {
	// 1. Mark connecting and snapshot desired target revision
	c.mu.Lock()
	c.state = StateConnecting
	targetRev := c.desired.Revision
	cfg := c.desired.ResolvedConfig
	c.mu.Unlock()

	// Break-before-make: If there is an existing generation, drain and close it first!
	c.drainAndCloseOldGen()

	// 2. Bound startup concurrency (max 4 global dials)
	select {
	case c.limiter <- struct{}{}:
	case <-c.ctx.Done():
		return
	case <-c.reconcileCh:
		return
	}

	// Connect downstream using long-lived c.ctx for session and process lifetime.
	// DialHTTP and DialIO enforce cfg.StartupTimeout internally on client.Connect.
	sess, procCloser, err := c.factory.CreateSession(c.ctx, cfg, c.onToolListChanged)

	// Release startup concurrency slot immediately after dial
	<-c.limiter

	// Handle connect failure
	if err != nil {
		c.handleConnectFailure(err)
		return
	}

	// 3. Stale revision check: if config changed during dial, close immediately!
	c.mu.Lock()
	if c.desired.Revision != targetRev || !c.desired.ResolvedConfig.Enabled || c.stopped {
		c.mu.Unlock()
		_ = sess.Close()
		if procCloser != nil {
			_ = procCloser.Close()
		}
		return
	}
	c.mu.Unlock()

	// 4. Discover tools
	discoverCtx := c.ctx
	var discoverCancel context.CancelFunc
	if cfg.StartupTimeout > 0 {
		discoverCtx, discoverCancel = context.WithTimeout(c.ctx, cfg.StartupTimeout)
	}
	tools, err := sess.ListAllTools(discoverCtx)
	if discoverCancel != nil {
		discoverCancel()
	}

	if err != nil {
		_ = sess.Close()
		if procCloser != nil {
			_ = procCloser.Close()
		}
		c.handleConnectFailure(fmt.Errorf("tool discovery failed: %w", err))
		return
	}

	// 5. Publish tools to Publisher
	var disabled []string
	for d := range cfg.DisabledTools {
		disabled = append(disabled, d)
	}

	snap, err := c.pub.PublishServer(c.serverID, tools, disabled)
	if err != nil {
		_ = sess.Close()
		if procCloser != nil {
			_ = procCloser.Close()
		}
		c.handleConnectFailure(fmt.Errorf("publish tools failed: %w", err))
		return
	}

	// 6. Establish new Generation and transition to StateReady
	c.mu.Lock()
	c.genSeq++
	genID := c.genSeq
	genCtx, genCancel := context.WithCancel(c.ctx)
	gen := newGeneration(genID, c.serverID, sess, procCloser, cfg.MaxConcurrency, cfg.CallTimeout, genCtx, genCancel, targetRev)
	c.currentGen = gen
	c.cachedTools = tools
	c.publishedCount = snap.PublishedCount(c.serverID)
	c.state = StateReady
	c.lastConnected = time.Now()
	c.lastError = ""
	c.everSucceeded = true
	c.mu.Unlock()

	// 7. Ready loop: monitor session health, tool changes, and 60s failure reset
	c.readyLoop(gen, sess)
}

// drainAndCloseOldGen ensures any lingering generation is drained and closed before dialing.
func (c *Coordinator) drainAndCloseOldGen() {
	c.mu.Lock()
	gen := c.currentGen
	c.currentGen = nil
	c.mu.Unlock()

	if gen != nil {
		gen.drain(c.drainTimeout)
		_ = gen.Close()
	}
}

// readyLoop runs while the server is in StateReady.
func (c *Coordinator) readyLoop(gen *Generation, sess Session) {
	resetTimer := time.NewTimer(c.readyResetDur)
	defer resetTimer.Stop()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-resetTimer.C:
			// Reset consecutive failure count after continuous Ready
			c.mu.Lock()
			if c.state == StateReady {
				c.consecutiveFail = 0
			}
			c.mu.Unlock()

		case <-c.toolChangedCh:
			// Re-discover tools without restarting the session
			c.refreshTools(sess)

		case ch := <-c.refreshCh:
			// Manual refresh requested
			err := c.refreshTools(sess)
			ch <- err

		case <-c.reconcileCh:
			// Config changed: break-before-make
			c.mu.Lock()
			c.state = StateDraining
			c.mu.Unlock()
			gen.drain(c.drainTimeout)
			_ = gen.Close()
			return

		case <-ticker.C:
			// Check if downstream session dropped
			if sess.IsClosed() || gen.IsClosed() {
				c.mu.Lock()
				c.state = StateUnavailable
				c.lastDisconnect = time.Now()
				c.lastError = "downstream session closed"
				c.mu.Unlock()
				gen.drain(c.drainTimeout)
				_ = gen.Close()
				return
			}

		case <-c.ctx.Done():
			gen.drain(c.drainTimeout)
			_ = gen.Close()
			return
		}
	}
}

// refreshTools performs tool discovery and re-publishes them to Publisher.
func (c *Coordinator) refreshTools(sess Session) error {
	c.mu.Lock()
	cfg := c.desired.ResolvedConfig
	c.mu.Unlock()

	discoverCtx := c.ctx
	var cancel context.CancelFunc
	if cfg.StartupTimeout > 0 {
		discoverCtx, cancel = context.WithTimeout(c.ctx, cfg.StartupTimeout)
		defer cancel()
	}

	tools, err := sess.ListAllTools(discoverCtx)
	if err != nil {
		return err
	}

	var disabled []string
	for d := range cfg.DisabledTools {
		disabled = append(disabled, d)
	}

	snap, err := c.pub.PublishServer(c.serverID, tools, disabled)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.cachedTools = tools
	c.publishedCount = snap.PublishedCount(c.serverID)
	c.mu.Unlock()
	return nil
}

// handleConnectFailure records the failure and sleeps with jittered backoff.
func (c *Coordinator) handleConnectFailure(err error) {
	c.mu.Lock()
	c.consecutiveFail++
	c.lastError = sanitizeError(err)
	c.lastDisconnect = time.Now()
	c.state = StateUnavailable
	fails := c.consecutiveFail
	c.mu.Unlock()

	delay := c.computeBackoff(fails)

	c.mu.Lock()
	c.state = StateBackoff
	c.mu.Unlock()

	select {
	case <-time.After(delay):
	case <-c.reconcileCh:
	case ch := <-c.refreshCh:
		ch <- nil // acknowledge refresh and retry immediately
	case <-c.ctx.Done():
	}
}

// computeBackoff calculates backoff duration with +/- 20% jitter.
func (c *Coordinator) computeBackoff(failCount int) time.Duration {
	if failCount <= 0 {
		failCount = 1
	}
	idx := failCount - 1
	if idx >= len(c.backoffDelays) {
		idx = len(c.backoffDelays) - 1
	}
	base := c.backoffDelays[idx]

	// Jitter: +/- 20%
	// factor in [0.8, 1.2]
	factor := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(base) * factor)
}

// diffConfig categorizes configuration differences for break-before-make reload decisions.
func diffConfig(old, new config.ResolvedServer) configChangeType {
	// Connection / lifecycle altering changes:
	if old.Enabled != new.Enabled ||
		old.Type != new.Type ||
		old.Command != new.Command ||
		old.Cwd != new.Cwd ||
		old.URL != new.URL ||
		old.MaxConcurrency != new.MaxConcurrency ||
		old.StartupTimeout != new.StartupTimeout ||
		!slicesEqual(old.Args, new.Args) ||
		!mapsEqual(old.Env, new.Env) ||
		!mapsEqual(old.Headers, new.Headers) {
		return changeConnection
	}

	toolsDiff := !toolSetsEqual(old.DisabledTools, new.DisabledTools)
	timeoutDiff := old.CallTimeout != new.CallTimeout

	if toolsDiff && timeoutDiff {
		return changeToolsAndTimeout
	}
	if toolsDiff {
		return changeToolsOnly
	}
	if timeoutDiff {
		return changeTimeoutOnly
	}

	return changeNone
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func toolSetsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "process_not_found"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission_denied"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "network_error"
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "connection refused"), strings.Contains(message, "connectex"):
		return "connection_refused"
	case strings.Contains(message, "no such host"), strings.Contains(message, "lookup"):
		return "dns_error"
	case strings.Contains(message, "initialize"):
		return "initialize_failed"
	case strings.Contains(message, "launch stdio"), strings.Contains(message, "start worker"), strings.Contains(message, "start downstream"):
		return "process_start_failed"
	case strings.Contains(message, "session closed"), strings.Contains(message, "eof"), strings.Contains(message, "closed pipe"):
		return "session_closed"
	default:
		return "downstream_error"
	}
}
