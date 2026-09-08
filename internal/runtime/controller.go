// Package runtime coordinates configuration reloads with the live downstream
// manager. It deliberately sits between CLI process wiring and the manager so
// file polling and operator-facing reload state are not transport concerns.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/online111111/mcp-manager/internal/config"
	"github.com/online111111/mcp-manager/internal/inbound"
	"github.com/online111111/mcp-manager/internal/manager"
)

type ConfigLoader func([]byte, string) (*config.Config, *config.ResolvedConfig, error)

type Controller struct {
	transactionMu   sync.Mutex
	mu              sync.RWMutex
	manager         *manager.Manager
	configPath      string
	currentListen   string
	startup         *config.ResolvedConfig
	current         *config.ResolvedConfig
	restartRequired bool
	lastReload      string
	appliedDigest   string
	candidateDigest string
	candidateCount  int
	load            ConfigLoader
}

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
	if data, err := config.ReadFileLimited(configPath); err == nil {
		controller.appliedDigest = digest(data)
		_, controller.startup, _ = config.Parse(data, filepath.Dir(configPath))
	}
	if len(startup) > 0 && startup[0] != nil {
		controller.startup = startup[0]
	}
	controller.startup = cloneResolvedConfig(controller.startup)
	controller.current = cloneResolvedConfig(controller.startup)
	return controller
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneResolvedConfig(src *config.ResolvedConfig) *config.ResolvedConfig {
	if src == nil {
		return nil
	}
	clone := *src
	clone.AllowedHosts = append([]string(nil), src.AllowedHosts...)
	clone.TrustedProxies = append([]string(nil), src.TrustedProxies...)
	clone.BearerTokens = append([]string(nil), src.BearerTokens...)
	return &clone
}

func (c *Controller) IsReady() bool {
	return c.manager == nil || c.manager.IsReady()
}

func (c *Controller) GetServerStatuses() []inbound.ServerStatusDTO {
	if c.manager == nil {
		return nil
	}
	return c.manager.GetServerStatuses()
}

func (c *Controller) GetRecentCalls() []inbound.RecentCallDTO {
	if c.manager == nil {
		return nil
	}
	return c.manager.GetRecentCalls()
}

// BearerTokens returns the currently active MCP access tokens. Unlike the
// Admin credential and listener settings, MCP bearer tokens are deliberately
// hot-reloadable so the Admin console can add or remove client credentials
// without requiring SSH access or a process restart.
func (c *Controller) BearerTokens() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return nil
	}
	if len(c.current.BearerTokens) > 0 {
		return append([]string(nil), c.current.BearerTokens...)
	}
	if c.current.BearerToken != "" {
		return []string{c.current.BearerToken}
	}
	return nil
}

func (c *Controller) RestartRequired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.restartRequired
}

func (c *Controller) LastReloadStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastReload
}

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

func (c *Controller) ReloadNow(ctx context.Context) error {
	c.transactionMu.Lock()
	defer c.transactionMu.Unlock()
	return c.reloadNow(ctx)
}

func (c *Controller) WithConfigTransaction(fn func(func(context.Context) error)) {
	c.transactionMu.Lock()
	defer c.transactionMu.Unlock()
	fn(c.reloadNow)
}

func (c *Controller) reloadNow(ctx context.Context) error {
	data, err := config.ReadFileLimited(c.configPath)
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
	data, err := config.ReadFileLimited(c.configPath)
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
	currentDigest, err := config.ComputeFileDigest(c.configPath)
	if err != nil {
		return err
	}
	if currentDigest != fileDigest {
		return config.ErrDigestMismatch{Expected: fileDigest, Actual: currentDigest}
	}

	// The Admin credential remains startup-bound even when the file changes.
	// Never hot-publish that still-active credential as an MCP access token.
	if c.startup != nil && c.startup.AdminEnabled {
		tokens := append([]string{resolved.BearerToken}, resolved.BearerTokens...)
		for _, token := range tokens {
			if token != "" && token == c.startup.AdminToken {
				return errors.New("MCP token must differ from the active Admin credential; restart after changing Admin settings")
			}
		}
	}

	// Startup-bound Admin credentials remain active until the process is
	// restarted. MCP bearer tokens are hot-reloadable, but both the new active
	// values and the old startup values must stay filtered from stdio children.
	stripStartupSecrets(resolved, c.startup)

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
	c.current = cloneResolvedConfig(resolved)
	c.appliedDigest = fileDigest
	c.candidateDigest = ""
	c.candidateCount = 0
	return nil
}

func stripStartupSecrets(resolved, startup *config.ResolvedConfig) {
	if resolved == nil || startup == nil {
		return
	}
	secrets := append([]string(nil), startup.BearerTokens...)
	if len(secrets) == 0 && startup.BearerToken != "" {
		secrets = append(secrets, startup.BearerToken)
	}
	secrets = append(secrets, startup.AdminToken)
	for id, srv := range resolved.Servers {
		if srv.Type != config.ServerTypeStdio || len(srv.Env) == 0 {
			continue
		}
		for key, value := range srv.Env {
			for _, secret := range secrets {
				if secret != "" && value == secret {
					delete(srv.Env, key)
					break
				}
			}
		}
		resolved.Servers[id] = srv
	}
}

func (c *Controller) reject(status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastReload = status
	c.candidateDigest = ""
	c.candidateCount = 0
}
