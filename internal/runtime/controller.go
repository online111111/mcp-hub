// Package runtime coordinates configuration reloads with the live downstream
// manager. It deliberately sits between CLI process wiring and the manager so
// file polling and operator-facing reload state are not transport concerns.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"mcp-hub/internal/config"
	"mcp-hub/internal/inbound"
	"mcp-hub/internal/manager"
)

// ConfigLoader validates and resolves a configuration file.
type ConfigLoader func([]byte, string) (*config.Config, *config.ResolvedConfig, error)

// Controller owns live reload observation and the public diagnostic view.
type Controller struct {
	// transactionMu serializes the entire read/apply/state sequence. Never hold
	// mu while acquiring this lock or calling Manager.
	transactionMu   sync.Mutex
	mu              sync.RWMutex
	manager         *manager.Manager
	configPath      string
	currentListen   string
	startup         *config.ResolvedConfig
	restartRequired bool
	lastReload      string
	appliedDigest   string
	candidateDigest string
	candidateCount  int
	load            ConfigLoader
}

// NewController creates a live-reload controller for an already started
// manager. load is injectable to keep reload state deterministic in tests.
func NewController(mgr *manager.Manager, configPath, currentListen string, load ConfigLoader, startup ...*config.ResolvedConfig) *Controller {
	if load == nil {
		load = func(data []byte, path string) (*config.Config, *config.ResolvedConfig, error) {
			return config.Parse(data, filepath.Dir(path))
		}
	}
	controller := &Controller{
		manager:       mgr,
		configPath:    configPath,
		currentListen: currentListen,
		lastReload:    "initial configuration loaded",
		load:          load,
	}
	if data, err := os.ReadFile(configPath); err == nil {
		controller.appliedDigest = digest(data)
		_, controller.startup, _ = config.Parse(data, filepath.Dir(configPath))
	}
	if len(startup) > 0 && startup[0] != nil {
		controller.startup = startup[0]
	}
	if controller.startup != nil {
		snapshot := *controller.startup
		snapshot.AllowedHosts = append([]string(nil), snapshot.AllowedHosts...)
		snapshot.TrustedProxies = append([]string(nil), snapshot.TrustedProxies...)
		controller.startup = &snapshot
	}
	return controller
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// IsReady delegates to the manager's current coordinator snapshot.
func (c *Controller) IsReady() bool {
	return c.manager == nil || c.manager.IsReady()
}

// GetServerStatuses returns sanitized manager status records.
func (c *Controller) GetServerStatuses() []inbound.ServerStatusDTO {
	if c.manager == nil {
		return nil
	}
	return c.manager.GetServerStatuses()
}

// GetRecentCalls returns sanitized recent call records.
func (c *Controller) GetRecentCalls() []inbound.RecentCallDTO {
	if c.manager == nil {
		return nil
	}
	return c.manager.GetRecentCalls()
}

// RestartRequired reports whether a successfully applied configuration changed
// startup-bound listener or HTTP/admin security settings.
func (c *Controller) RestartRequired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.restartRequired
}

// LastReloadStatus returns a non-secret operator-facing reload summary.
func (c *Controller) LastReloadStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastReload
}

// Start begins stable-sample polling. A changed file must be observed twice so
// ordinary editors cannot expose a partially written configuration.
func (c *Controller) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.poll(ctx)
			}
		}
	}()
}

// ReloadNow validates and applies the current file immediately after an atomic
// admin write.
func (c *Controller) ReloadNow(ctx context.Context) error {
	c.transactionMu.Lock()
	defer c.transactionMu.Unlock()
	return c.reloadNow(ctx)
}

// WithConfigTransaction runs synchronous file read/write/reload/rollback work.
// The supplied reload must not escape the callback. Do not call ReloadNow or
// nest transactions from the callback (the transaction lock is not reentrant).
func (c *Controller) WithConfigTransaction(fn func(func(context.Context) error)) {
	c.transactionMu.Lock()
	defer c.transactionMu.Unlock()
	fn(c.reloadNow)
}

func (c *Controller) reloadNow(ctx context.Context) error {
	data, err := os.ReadFile(c.configPath)
	if err != nil {
		c.reject("reload rejected: configuration unavailable")
		return err
	}
	_, resolved, err := c.load(data, c.configPath)
	if err != nil {
		c.reject("reload rejected: invalid configuration")
		return err
	}
	if err := c.apply(ctx, resolved, digest(data)); err != nil {
		c.reject("reload rejected: apply failed")
		return err
	}
	return nil
}

func (c *Controller) poll(ctx context.Context) {
	c.transactionMu.Lock()
	defer c.transactionMu.Unlock()
	data, err := os.ReadFile(c.configPath)
	if err != nil {
		c.reject("reload rejected: configuration unavailable")
		return
	}
	fileDigest := digest(data)
	if !c.observeCandidate(fileDigest) {
		return
	}

	_, resolved, err := c.load(data, c.configPath)
	if err != nil {
		c.reject("reload rejected: invalid configuration")
		return
	}
	if err := c.apply(ctx, resolved, fileDigest); err != nil {
		c.reject("reload rejected: apply failed")
	}
}

func (c *Controller) observeCandidate(fileDigest string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fileDigest == c.appliedDigest {
		c.candidateDigest = ""
		c.candidateCount = 0
		return false
	}
	if fileDigest != c.candidateDigest {
		c.candidateDigest = fileDigest
		c.candidateCount = 1
		return false
	}
	c.candidateCount++
	return c.candidateCount >= 2
}

func (c *Controller) apply(ctx context.Context, resolved *config.ResolvedConfig, fileDigest string) error {
	// The transaction lock orders Hub writers, but an external editor does not
	// take it. Discard a snapshot replaced while it was being parsed/resolved.
	currentDigest, err := config.ComputeFileDigest(c.configPath)
	if err != nil {
		return err
	}
	if currentDigest != fileDigest {
		return config.ErrDigestMismatch{Expected: fileDigest, Actual: currentDigest}
	}
	if c.manager != nil {
		if err := c.manager.Apply(ctx, resolved); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	boundChanged := c.startup != nil && (resolved.PublicMode != c.startup.PublicMode ||
		resolved.PublicURL != c.startup.PublicURL ||
		!reflect.DeepEqual(resolved.AllowedHosts, c.startup.AllowedHosts) ||
		!reflect.DeepEqual(resolved.TrustedProxies, c.startup.TrustedProxies) ||
		resolved.BearerToken != c.startup.BearerToken ||
		resolved.AdminEnabled != c.startup.AdminEnabled ||
		resolved.AdminToken != c.startup.AdminToken ||
		resolved.AdminSessionTimeout != c.startup.AdminSessionTimeout)
	c.restartRequired = resolved.Listen != c.currentListen || boundChanged
	if c.restartRequired {
		c.lastReload = "restart_required (listen address changed)"
		if boundChanged {
			c.lastReload = "restart_required (startup-bound HTTP/admin security settings changed)"
		}
	} else {
		c.lastReload = "reloaded successfully"
	}
	c.appliedDigest = fileDigest
	c.candidateDigest = ""
	c.candidateCount = 0
	return nil
}

func (c *Controller) reject(status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastReload = status
	c.candidateDigest = ""
	c.candidateCount = 0
}
