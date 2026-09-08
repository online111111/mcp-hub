package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/online111111/mcp-manager/internal/inbound"
)

func TestCLI_Validate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"validate"}, &stdout, &stderr)
	if code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d), got %d", ExitInvalidParams, code)
	}

	t.Setenv("SEARCH_TOKEN", "test-token")
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"validate", "--config", filepath.Join("..", "..", "testdata", "config", "valid.json")}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Errorf("expected ExitSuccess (%d), got %d (stderr: %s)", ExitSuccess, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Configuration is valid") {
		t.Errorf("expected validation success message, got: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"validate", "--config", filepath.Join("..", "..", "testdata", "config", "duplicate_key.json")}, &stdout, &stderr)
	if code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d), got %d", ExitInvalidParams, code)
	}
}

func TestCLI_Import(t *testing.T) {
	var stdout, stderr bytes.Buffer
	src := filepath.Join("..", "..", "testdata", "config", "import_source.json")
	target := filepath.Join("..", "..", "testdata", "config", "valid.json")

	if code := Run([]string{"import"}, &stdout, &stderr); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d), got %d", ExitInvalidParams, code)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"import", "--from", src, "--config", target, "--dry-run", "--yes"}, &stdout, &stderr); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d), got %d", ExitInvalidParams, code)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"import", "--from", src, "--config", target}, &stdout, &stderr); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d), got %d", ExitInvalidParams, code)
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"import", "--from", src, "--config", target, "--dry-run"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Errorf("expected ExitSuccess (%d), got %d (stderr: %s)", ExitSuccess, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Target Config:") {
		t.Errorf("expected preview output, got: %s", stdout.String())
	}
}

func TestCLI_Export(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"export"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Errorf("expected ExitSuccess (%d), got %d (stderr: %s)", ExitSuccess, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"command":`) || !strings.Contains(stdout.String(), `"mcp-manager"`) {
		t.Errorf("expected MCP Manager stdio export, got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "NOT_RUN") {
		t.Errorf("expected NOT_RUN reminder in stderr, got: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"export", "--token-env"}, &stdout, &stderr)
	if code != ExitSuccess || !strings.Contains(stdout.String(), `"MCP_MANAGER_TOKEN"`) || strings.Contains(stdout.String(), `"MCP_HUB_TOKEN"`) {
		t.Fatalf("expected new token variable in export, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"export", "--client", "claude-desktop", "--transport", "http", "--endpoint", "http://127.0.0.1:8080/mcp"}, &stdout, &stderr)
	if code != ExitInvalidParams || !strings.Contains(stderr.String(), "only stdio") {
		t.Errorf("expected stdio-only rejection, code=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"export", "--client", "unknown-client"}, &stdout, &stderr); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams for invalid client, got %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"export", "--transport", "unknown-transport"}, &stdout, &stderr); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams for invalid transport, got %d", code)
	}
}

func TestCLI_Stdio(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"stdio"}, &stdout, &stderr)
	if code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d), got %d", ExitInvalidParams, code)
	}
	if !strings.Contains(stderr.String(), "--connect") {
		t.Errorf("expected missing connect message, got: %s", stderr.String())
	}
}

func TestCLI_StatusAndDoctor(t *testing.T) {
	ln, err := inbound.BindLoopbackListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("BindLoopbackListener failed: %v", err)
	}
	defer ln.Close()

	hubServer := inbound.NewHubServer("test-manager", "0.1.0")
	pub, err := inbound.NewPublisher(hubServer, nil, nil)
	if err != nil {
		t.Fatalf("NewPublisher failed: %v", err)
	}

	httpSrv, err := inbound.NewHTTPServer(ln, pub, nil, &inbound.HTTPServerOptions{Version: "0.2.0"})
	if err != nil {
		t.Fatalf("NewHTTPServer failed: %v", err)
	}
	go func() { _ = httpSrv.Serve() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--endpoint", httpSrv.URL()}, &stdout, &stderr)
	if code != ExitSuccess || !strings.Contains(stdout.String(), "Version:          0.2.0") || !strings.Contains(stdout.String(), "MCP Manager Status") {
		t.Errorf("status failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"status", "--endpoint", httpSrv.URL(), "--json"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Errorf("status JSON failed with code %d (stderr: %s)", code, stderr.String())
	}
	var st inbound.StatusDTO
	if err := json.Unmarshal(stdout.Bytes(), &st); err != nil {
		t.Fatalf("failed to decode status JSON: %v", err)
	}
	if st.Version != "0.2.0" {
		t.Errorf("expected version 0.2.0, got %q", st.Version)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"doctor", "--endpoint", httpSrv.URL()}, &stdout, &stderr)
	if code != ExitSuccess || !strings.Contains(stdout.String(), "MCP Manager Doctor") || !strings.Contains(stdout.String(), "All checks passed") {
		t.Errorf("doctor failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	dummyLn, _ := net.Listen("tcp", "127.0.0.1:0")
	dummyPort := dummyLn.Addr().(*net.TCPAddr).Port
	dummyLn.Close()
	offlineEndpoint := fmt.Sprintf("http://127.0.0.1:%d", dummyPort)

	if code := Run([]string{"status", "--endpoint", offlineEndpoint}, &stdout, &stderr); code != ExitRuntimeUnavailable {
		t.Errorf("expected ExitRuntimeUnavailable (%d) for offline status, got %d", ExitRuntimeUnavailable, code)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"doctor", "--endpoint", offlineEndpoint}, &stdout, &stderr)
	if code != ExitRuntimeUnavailable {
		t.Errorf("expected ExitRuntimeUnavailable (%d) for offline doctor, got %d", ExitRuntimeUnavailable, code)
	}
	if !strings.Contains(stderr.String(), "mcp-manager serve") {
		t.Errorf("expected doctor to suggest mcp-manager serve, got: %s", stderr.String())
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"unknown-cmd"}, &stdout, &stderr)
	if code != ExitInvalidParams || !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("unexpected unknown-command result code=%d stderr=%s", code, stderr.String())
	}
}

func TestCLI_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, &stdout, &stderr); code != ExitSuccess {
		t.Fatalf("version exited with %d: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "mcp-manager 0.4.0" {
		t.Fatalf("unexpected version output %q", got)
	}
}

func TestCLI_Serve(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"serve"}, &stdout, &stderr); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams for missing config, got %d", code)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind ephemeral port: %v", err)
	}
	defer ln.Close()
	occupiedPort := ln.Addr().(*net.TCPAddr).Port

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgContent := fmt.Sprintf(`{
		"version": 1,
		"hub": {"listen": "127.0.0.1:%d"},
		"mcpServers": {}
	}`, occupiedPort)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runServe([]string{"--config", cfgPath}, &stdout, &stderr, nil); code != ExitInvalidParams {
		t.Errorf("expected ExitInvalidParams (%d) on port conflict, got %d", ExitInvalidParams, code)
	}
	if !strings.Contains(stderr.String(), "failed to bind listen address") {
		t.Errorf("expected failed to bind listen address error, got: %s", stderr.String())
	}

	ln.Close()
	stopCh := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(stopCh)
	}()

	stdout.Reset()
	stderr.Reset()
	code := runServe([]string{"--config", cfgPath}, &stdout, &stderr, stopCh)
	if code != ExitSuccess {
		t.Errorf("expected ExitSuccess (%d) on clean shutdown, got %d (stderr: %s)", ExitSuccess, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "MCP Manager serving on") {
		t.Errorf("expected serving message in stderr, got: %s", stderr.String())
	}
}
