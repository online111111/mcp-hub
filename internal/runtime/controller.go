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
	mu              sync.RWMutex
	manager         *manager.Manager
	configPath      string
	currentListen   string
	restartRequired bool
	lastReload      string
	appliedDigest   string
	candidateDigest string
	candidateCount  int
	load            ConfigLoader
}

// NewController creates a live-reload controller for an already started
// manager. load is injectable to keep reload state deterministic in tests.
func NewController(mgr *manager.Manager, configPath, currentListen string, load ConfigLoader) *Controller {
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
// the listener, which cannot be replaced in-process.
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
	data, err := os.ReadFile(c.configPath)
	if err != nil {
		return err
	}
	_, resolved, err := c.load(data, c.configPath)
	if err != nil {
		return err
	}
	if err := c.apply(ctx, resolved, digest(data)); err != nil {
		return err
	}
	return nil
}

func (c *Controller) poll(ctx context.Context) {
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
	if c.manager != nil {
		if err := c.manager.Apply(ctx, resolved); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restartRequired = resolved.Listen != c.currentListen
	if c.restartRequired {
		c.lastReload = "restart_required (listen address changed)"
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
