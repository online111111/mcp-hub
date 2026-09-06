package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mcp-hub/internal/admin"
	"mcp-hub/internal/bridge"
	"mcp-hub/internal/inbound"
	"mcp-hub/internal/manager"
)

// Exit codes per contract:
// 0: success
// 1: internal error
// 2: parameter / config invalid / conflict
// 3: network / runtime unavailable
const (
	ExitSuccess            = 0
	ExitInternalError      = 1
	ExitInvalidParams      = 2
	ExitRuntimeUnavailable = 3
)

// HubManagerAdapter adapts manager.Manager to inbound.ManagerCallback,
// managing status inspection and configuration reload loops.
type HubManagerAdapter struct {
	mu                     sync.RWMutex
	mgr                    *manager.Manager
	configPath             string
	currentListen          string
	configuredEnabledCount int
	restartRequired        bool
	lastReload             string
	appliedDigest          string
	candidateDigest        string
	candidateCount         int
}

// NewHubManagerAdapter creates an adapter for manager.Manager.
func NewHubManagerAdapter(mgr *manager.Manager, configPath, currentListen string, configuredEnabledCount int) *HubManagerAdapter {
	adapter := &HubManagerAdapter{
		mgr:                    mgr,
		configPath:             configPath,
		currentListen:          currentListen,
		configuredEnabledCount: configuredEnabledCount,
		lastReload:             "initial config loaded",
	}
	if data, err := os.ReadFile(configPath); err == nil {
		adapter.appliedDigest = digestConfigBytes(data)
	}
	return adapter
}

func digestConfigBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// IsReady reports readiness per contract:
// ready if no enabled servers, or at least one is Ready with published tools.
func (a *HubManagerAdapter) IsReady() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// If no servers are enabled in configuration, readyz returns 200 per contract
	if a.configuredEnabledCount == 0 {
		return true
	}

	if a.mgr == nil {
		return false
	}

	statuses := a.mgr.Status()
	if len(statuses) == 0 {
		return false
	}

	hasReadyWithTools := false
	for _, s := range statuses {
		if s.Enabled && s.State == manager.StateReady && s.PublishedTools > 0 {
			hasReadyWithTools = true
			break
		}
	}

	return hasReadyWithTools
}

// GetServerStatuses returns sanitized statuses of managed servers.
func (a *HubManagerAdapter) GetServerStatuses() []inbound.ServerStatusDTO {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.mgr == nil {
		return nil
	}

	statuses := a.mgr.Status()
	res := make([]inbound.ServerStatusDTO, 0, len(statuses))
	for id, s := range statuses {
		res = append(res, inbound.ServerStatusDTO{
			ID:                   id,
			State:                s.State,
			PublishedToolCount:   s.PublishedTools,
			UnpublishedToolCount: 0,
			ActiveCalls:          s.ActiveLeases,
			DesiredRevision:      s.DesiredRevision,
			ActiveRevision:       s.ActiveRevision,
			ErrorCategory:        s.LastError,
		})
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].ID < res[j].ID
	})
	return res
}

// GetRecentCalls returns sanitized summaries recorded by the manager.
func (a *HubManagerAdapter) GetRecentCalls() []inbound.RecentCallDTO {
	a.mu.RLock()
	mgr := a.mgr
	a.mu.RUnlock()
	if mgr == nil {
		return nil
	}
	return mgr.GetRecentCalls()
}

// RestartRequired reports whether a listen address change requires restarting the process.
func (a *HubManagerAdapter) RestartRequired() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.restartRequired
}

// LastReloadStatus returns the outcome of the latest config reload.
func (a *HubManagerAdapter) LastReloadStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastReload
}

// StartReloadLoop starts periodic sampling and reloading of the configuration.
func (a *HubManagerAdapter) StartReloadLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.pollReload(ctx)
			}
		}
	}()
}

// ReloadNow validates and applies the current file immediately after an atomic
// admin write. Polling remains the fallback for external editor changes.
func (a *HubManagerAdapter) ReloadNow(ctx context.Context) error {
	cfg, resolved, err := ValidateConfig(a.configPath)
	if err != nil {
		return err
	}
	if a.mgr != nil {
		if err := a.mgr.Apply(ctx, resolved); err != nil {
			return err
		}
	}
	enabled := 0
	for _, server := range cfg.MCPServers {
		if server.Enabled == nil || *server.Enabled {
			enabled++
		}
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.configuredEnabledCount = enabled
	if resolved.Listen != a.currentListen {
		a.restartRequired = true
		a.lastReload = "restart_required (listen address changed)"
	} else {
		a.lastReload = "reloaded successfully"
	}
	a.appliedDigest = digestConfigBytes(data)
	a.candidateDigest = ""
	a.candidateCount = 0
	a.mu.Unlock()
	return nil
}

func (a *HubManagerAdapter) pollReload(ctx context.Context) {
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		a.mu.Lock()
		a.lastReload = "reload rejected: configuration unavailable"
		a.candidateDigest = ""
		a.candidateCount = 0
		a.mu.Unlock()
		return
	}
	digest := digestConfigBytes(data)

	a.mu.Lock()
	if digest == a.appliedDigest {
		a.candidateDigest = ""
		a.candidateCount = 0
		a.mu.Unlock()
		return
	}
	if digest != a.candidateDigest {
		a.candidateDigest = digest
		a.candidateCount = 1
		a.mu.Unlock()
		return
	}
	a.candidateCount++
	if a.candidateCount < 2 {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	newCfg, newResolved, err := ValidateConfig(a.configPath)

	a.mu.Lock()
	if err != nil {
		a.lastReload = "reload rejected: invalid configuration"
		a.candidateDigest = ""
		a.candidateCount = 0
		a.mu.Unlock()
		return
	}

	newEnabledCount := 0
	for _, s := range newCfg.MCPServers {
		if s.Enabled == nil || *s.Enabled {
			newEnabledCount++
		}
	}
	a.configuredEnabledCount = newEnabledCount

	if newResolved.Listen != a.currentListen {
		a.restartRequired = true
		a.lastReload = "restart_required (listen address changed)"
	} else {
		a.lastReload = "reloaded successfully"
	}
	mgr := a.mgr
	a.mu.Unlock()

	if mgr != nil {
		if applyErr := mgr.Apply(ctx, newResolved); applyErr != nil {
			a.mu.Lock()
			a.lastReload = "reload rejected: apply failed"
			a.candidateDigest = ""
			a.candidateCount = 0
			a.mu.Unlock()
			return
		}
	}

	a.mu.Lock()
	a.appliedDigest = digest
	a.candidateDigest = ""
	a.candidateCount = 0
	a.mu.Unlock()
}

// Run parses command-line arguments and executes the requested subcommand.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return ExitInvalidParams
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:], stdout, stderr, nil)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "import":
		return runImport(args[1:], stdout, stderr)
	case "export":
		return runExport(args[1:], stdout, stderr)
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "stdio":
		return runStdio(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return ExitSuccess
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return ExitInvalidParams
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `MCP Hub - Model Context Protocol Hub & Aggregator

Usage:
  mcp-hub serve --config <path>
  mcp-hub validate --config <path>
  mcp-hub import --from <path> --config <path> [--dry-run | --yes] [--remote-type <type>]
  mcp-hub export [--client <cursor|claude-desktop>] [--transport <stdio|http>] [--endpoint <url>] [--token-env]
  mcp-hub status [--endpoint <url>] [--json]
  mcp-hub doctor [--endpoint <url>]
  mcp-hub stdio --connect <url> [--token <bearer-token>]
`)
}

// runServe executes the serve subcommand.
func runServe(args []string, stdout, stderr io.Writer, stopCh <-chan struct{}) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to configuration file")

	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}

	if *configPath == "" {
		fmt.Fprintln(stderr, "error: --config flag is required")
		return ExitInvalidParams
	}

	// 1. Validate configuration strictly before binding or starting downstream
	cfg, resolved, err := ValidateConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "configuration validation error: %v\n", err)
		return ExitInvalidParams
	}

	// 2. Bind loopback listener FIRST; fail fast before allocating downstream resources
	listener, err := inbound.BindListener(resolved.Listen, resolved.PublicMode)
	if err != nil {
		fmt.Fprintf(stderr, "failed to bind listen address %q: %v\n", resolved.Listen, err)
		return ExitInvalidParams
	}
	defer listener.Close()

	// 3. Initialize SDK Hub server with Tools.ListChanged=true capability
	hubServer := inbound.NewHubServer("mcp-hub", "0.1.0")
	publisher, err := inbound.NewPublisher(hubServer, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize publisher: %v\n", err)
		return ExitInternalError
	}

	// 4. Initialize Manager, wire router callback, and start downstream
	mgrCtx, cancelMgr := context.WithCancel(context.Background())
	defer cancelMgr()

	mgr := manager.NewManager(publisher)
	publisher.SetRouterCallback(mgr.RouteTool)

	if err := mgr.Start(mgrCtx); err != nil {
		fmt.Fprintf(stderr, "failed to start downstream manager: %v\n", err)
		return ExitInternalError
	}
	if err := mgr.Apply(mgrCtx, resolved); err != nil {
		fmt.Fprintf(stderr, "failed to apply initial configuration: %v\n", err)
		return ExitInternalError
	}

	enabledCount := 0
	for _, s := range cfg.MCPServers {
		if s.Enabled == nil || *s.Enabled {
			enabledCount++
		}
	}

	// 5. Initialize Adapter and start reload loop
	adapter := NewHubManagerAdapter(mgr, *configPath, resolved.Listen, enabledCount)
	adapter.StartReloadLoop(mgrCtx, 1*time.Second)

	// 6. Initialize optional embedded admin UI and inbound HTTP server.
	var adminHandler http.Handler
	if resolved.AdminEnabled {
		adminUI, adminErr := admin.New(admin.Options{
			ConfigPath:     *configPath,
			AdminToken:     resolved.AdminToken,
			PublicURL:      resolved.PublicURL,
			PublicMode:     resolved.PublicMode,
			TrustedProxies: resolved.TrustedProxies,
			SessionTimeout: resolved.AdminSessionTimeout,
			Status: func() any {
				return inbound.StatusDTO{
					Version:          "0.2.0",
					CatalogRevision:  publisher.Revision(),
					RestartRequired:  adapter.RestartRequired(),
					LastReloadStatus: adapter.LastReloadStatus(),
					Servers:          adapter.GetServerStatuses(),
					RecentCalls:      adapter.GetRecentCalls(),
				}
			},
			Reload: adapter.ReloadNow,
		})
		if adminErr != nil {
			fmt.Fprintf(stderr, "failed to initialize admin UI: %v\n", adminErr)
			return ExitInternalError
		}
		adminHandler = adminUI
	}
	httpSrv, err := inbound.NewHTTPServer(listener, publisher, adapter, &inbound.HTTPServerOptions{
		Version:        "0.2.0",
		PublicMode:     resolved.PublicMode,
		PublicURL:      resolved.PublicURL,
		AllowedHosts:   resolved.AllowedHosts,
		TrustedProxies: resolved.TrustedProxies,
		BearerToken:    resolved.BearerToken,
		AdminHandler:   adminHandler,
	})
	if err != nil {
		fmt.Fprintf(stderr, "failed to create HTTP server: %v\n", err)
		return ExitInternalError
	}

	// 7. Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	shutdownDone := make(chan struct{})

	go func() {
		select {
		case <-sigChan:
		case <-stopCh:
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		_ = mgr.Stop(shutdownCtx)
		cancelMgr()
		close(shutdownDone)
	}()

	fmt.Fprintf(stderr, "MCP Hub serving on %s (listening on %s, %d servers configured)\n",
		httpSrv.URL(), resolved.Listen, len(cfg.MCPServers))

	if err := httpSrv.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "server encountered an error: %v\n", err)
		cancelMgr()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = mgr.Stop(shutdownCtx)
		cancel()
		return ExitInternalError
	}

	<-shutdownDone
	return ExitSuccess
}

// runValidate executes the validate subcommand.
func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to configuration file")

	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}

	if *configPath == "" {
		fmt.Fprintln(stderr, "error: --config flag is required")
		return ExitInvalidParams
	}

	cfg, _, err := ValidateConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "validation failed for %q: %v\n", *configPath, err)
		return ExitInvalidParams
	}

	fmt.Fprintf(stdout, "Configuration is valid: %s (%d servers configured)\n", *configPath, len(cfg.MCPServers))
	return ExitSuccess
}

// runImport executes the import subcommand.
func runImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fromPath := fs.String("from", "", "Source configuration file")
	configPath := fs.String("config", "", "Target configuration file")
	dryRun := fs.Bool("dry-run", false, "Preview import without modifying configuration")
	yes := fs.Bool("yes", false, "Execute import and save to configuration")
	remoteType := fs.String("remote-type", "", "Remote server type: streamableHttp or http")

	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}

	if *fromPath == "" || *configPath == "" {
		fmt.Fprintln(stderr, "error: both --from and --config are required")
		return ExitInvalidParams
	}

	// Mutually exclusive flags per doc
	if (*dryRun && *yes) || (!*dryRun && !*yes) {
		fmt.Fprintln(stderr, "error: exactly one of --dry-run or --yes must be specified")
		return ExitInvalidParams
	}

	preview, err := RunImport(*fromPath, *configPath, *remoteType, *dryRun, *yes)
	if err != nil {
		fmt.Fprintf(stderr, "import failed: %v\n", err)
		return ExitInvalidParams
	}

	if *dryRun {
		fmt.Fprint(stdout, FormatPreview(preview))
	} else {
		fmt.Fprintf(stdout, "Successfully imported %d new servers into %s (total: %d)\n",
			preview.NewCount, preview.TargetConfigPath, preview.ExistingCount+preview.NewCount)
	}

	return ExitSuccess
}

type exportServerEntry struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

type exportConfig struct {
	MCPServers map[string]exportServerEntry `json:"mcpServers"`
}

// runExport executes the export subcommand.
func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	client := fs.String("client", "cursor", "Target client: cursor or claude-desktop")
	transport := fs.String("transport", "stdio", "Client transport: stdio or http")
	endpoint := fs.String("endpoint", "http://127.0.0.1:8080/mcp", "MCP endpoint URL")
	withTokenEnv := fs.Bool("token-env", false, "Add MCP_HUB_TOKEN environment placeholder for public Hub stdio export")

	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}

	clientLower := strings.ToLower(strings.TrimSpace(*client))
	if clientLower != "cursor" && clientLower != "claude-desktop" {
		fmt.Fprintf(stderr, "error: unsupported client %q (supported: cursor, claude-desktop)\n", *client)
		return ExitInvalidParams
	}

	transportLower := strings.ToLower(strings.TrimSpace(*transport))
	if transportLower != "stdio" && transportLower != "http" {
		fmt.Fprintf(stderr, "error: unsupported transport %q (supported: stdio, http)\n", *transport)
		return ExitInvalidParams
	}
	if clientLower == "claude-desktop" && transportLower != "stdio" {
		fmt.Fprintln(stderr, "error: claude-desktop export supports only stdio transport")
		return ExitInvalidParams
	}

	exp := exportConfig{
		MCPServers: make(map[string]exportServerEntry),
	}

	if transportLower == "stdio" {
		exePath, err := os.Executable()
		if err != nil {
			exePath = "mcp-hub"
		} else {
			if abs, err := filepath.Abs(exePath); err == nil {
				exePath = abs
			}
		}
		entry := exportServerEntry{Command: exePath, Args: []string{"stdio", "--connect", *endpoint}}
		if *withTokenEnv {
			entry.Env = map[string]string{"MCP_HUB_TOKEN": "<YOUR_MCP_HUB_TOKEN>"}
		}
		exp.MCPServers["mcp-hub"] = entry
	} else {
		exp.MCPServers["mcp-hub"] = exportServerEntry{
			URL: *endpoint,
		}
	}

	data, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal export configuration: %v\n", err)
		return ExitInternalError
	}

	// Output compatibility status reminder per doc
	fmt.Fprintf(stderr, "Note: Client compatibility status for %s is NOT_RUN until verified with an installed client.\n", clientLower)
	fmt.Fprintln(stdout, string(data))

	return ExitSuccess
}

// runStatus executes the status subcommand.
func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoint := fs.String("endpoint", "http://127.0.0.1:8080", "Hub base URL")
	asJSON := fs.Bool("json", false, "Output status as JSON")

	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}

	baseURL := strings.TrimRight(*endpoint, "/")
	if strings.HasSuffix(baseURL, "/mcp") {
		baseURL = strings.TrimSuffix(baseURL, "/mcp")
	}
	statusURL := baseURL + "/api/v1/status"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(statusURL)
	if err != nil {
		fmt.Fprintf(stderr, "error: failed to connect to Hub at %s: %v\n", statusURL, err)
		return ExitRuntimeUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "error: Hub returned HTTP status %d (%s)\n", resp.StatusCode, resp.Status)
		return ExitRuntimeUnavailable
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(stderr, "error: failed to read status response: %v\n", err)
		return ExitInternalError
	}

	if *asJSON {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err == nil {
			fmt.Fprintln(stdout, pretty.String())
		} else {
			fmt.Fprintln(stdout, string(body))
		}
		return ExitSuccess
	}

	var status inbound.StatusDTO
	if err := json.Unmarshal(body, &status); err != nil {
		fmt.Fprintf(stderr, "error: failed to parse status JSON: %v\n", err)
		return ExitInternalError
	}

	fmt.Fprintf(stdout, `MCP Hub Status:
  Version:          %s
  Uptime:           %d seconds
  Catalog Revision: %d
  Restart Required: %v
  Last Reload:      %s

Managed Servers (%d):
`, status.Version, status.UptimeSeconds, status.CatalogRevision, status.RestartRequired, status.LastReloadStatus, len(status.Servers))

	for _, s := range status.Servers {
		fmt.Fprintf(stdout, "  - [%s] State: %s | Published Tools: %d | Unpublished Tools: %d | Active Calls: %d\n",
			s.ID, s.State, s.PublishedToolCount, s.UnpublishedToolCount, s.ActiveCalls)
	}

	if len(status.RecentCalls) > 0 {
		fmt.Fprintf(stdout, "\nRecent Calls (%d):\n", len(status.RecentCalls))
		for _, c := range status.RecentCalls {
			fmt.Fprintf(stdout, "  - [%s] Tool: %s (Server: %s) -> Outcome: %s (%d ms)\n",
				c.Time, c.Tool, c.ServerID, c.Outcome, c.DurationMs)
		}
	}

	return ExitSuccess
}

// runDoctor executes the doctor subcommand.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoint := fs.String("endpoint", "http://127.0.0.1:8080", "Hub base URL")

	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}

	baseURL := strings.TrimRight(*endpoint, "/")
	if strings.HasSuffix(baseURL, "/mcp") {
		baseURL = strings.TrimSuffix(baseURL, "/mcp")
	}
	mcpURL := baseURL + "/mcp"
	healthURL := baseURL + "/healthz"
	readyURL := baseURL + "/readyz"

	httpClient := &http.Client{Timeout: 5 * time.Second}

	// 1. Health check
	hResp, err := httpClient.Get(healthURL)
	if err != nil {
		fmt.Fprintf(stderr, "Hub is not running at %s. Please start the hub first with: mcp-hub serve --config <config-file>\n", baseURL)
		return ExitRuntimeUnavailable
	}
	defer hResp.Body.Close()

	if hResp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "Hub health check returned non-OK status: %d\n", hResp.StatusCode)
		return ExitRuntimeUnavailable
	}

	// 2. Readiness check
	rResp, err := httpClient.Get(readyURL)
	if err != nil {
		fmt.Fprintf(stderr, "Hub readiness check failed at %s: %v\n", readyURL, err)
		return ExitRuntimeUnavailable
	}
	defer rResp.Body.Close()
	if rResp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "Hub is not ready: readiness check returned HTTP %d\n", rResp.StatusCode)
		return ExitRuntimeUnavailable
	}
	readyStatus := "Ready (200)"

	// 3. MCP Protocol initialize and tools/list check
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-hub-doctor", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: mcpURL}

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		fmt.Fprintf(stderr, "Hub MCP protocol initialize failed at %s: %v\n", mcpURL, err)
		return ExitRuntimeUnavailable
	}
	defer session.Close()

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		fmt.Fprintf(stderr, "Hub MCP tools/list failed: %v\n", err)
		return ExitRuntimeUnavailable
	}

	toolCount := len(toolsResult.Tools)

	fmt.Fprintf(stdout, `MCP Hub Doctor Diagnostic Report
================================
Endpoint:          %s
Health Check:      OK (200)
Readiness:         %s
MCP Initialize:    OK
MCP Tools Listed:  %d tools available
Result:            All checks passed
`, baseURL, readyStatus, toolCount)

	return ExitSuccess
}

// runStdio executes the stdio-to-Hub bridge. The bridge owns stdout and writes
// only MCP messages there; diagnostics and startup failures go to stderr.
func runStdio(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stdio", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoint := fs.String("connect", "", "Running Hub MCP endpoint URL")
	token := fs.String("token", "", "Bearer token for a public Hub (or MCP_HUB_TOKEN)")
	if err := fs.Parse(args); err != nil {
		return ExitInvalidParams
	}
	if strings.TrimSpace(*endpoint) == "" {
		fmt.Fprintln(stderr, "error: --connect flag is required")
		return ExitInvalidParams
	}

	if *token == "" {
		*token = os.Getenv("MCP_HUB_TOKEN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.RunAuthenticated(ctx, *endpoint, *token, os.Stdin, stdout, stderr); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
			return ExitSuccess
		}
		fmt.Fprintf(stderr, "stdio bridge failed: %v\n", err)
		return ExitRuntimeUnavailable
	}
	return ExitSuccess
}
