//go:build !windows

package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func init() {
	switch os.Getenv("MCP_POSIX_HELPER_MODE") {
	case "spawn_grandchild", "spawn_grandchild_and_exit":
		grandchild := exec.Command(os.Args[0])
		grandchild.Env = append(os.Environ(), "MCP_POSIX_HELPER_MODE=hang")
		if err := grandchild.Start(); err != nil {
			os.Exit(2)
		}
		pidFile := os.Getenv("MCP_POSIX_GRANDCHILD_PID_FILE")
		if pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(grandchild.Process.Pid)), 0644)
		}
		if os.Getenv("MCP_POSIX_HELPER_MODE") == "spawn_grandchild_and_exit" {
			os.Exit(0)
		}
		_, _ = os.Stdin.Read(make([]byte, 1))
		_ = grandchild.Wait()
		os.Exit(0)
	case "hang":
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
}

func posixProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 2 && fields[2] == "Z" {
			return false
		}
	}
	return syscall.Kill(pid, 0) == nil
}

func waitForPosixProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !posixProcessAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !posixProcessAlive(pid)
}

func startPosixGrandchildFixture(t *testing.T, ctx context.Context) (Process, int) {
	return startPosixGrandchildFixtureMode(t, ctx, "spawn_grandchild")
}

func startPosixGrandchildFixtureMode(t *testing.T, ctx context.Context, mode string) (Process, int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	proc, err := Start(ctx, Spec{
		Command: os.Args[0],
		Env: append(os.Environ(),
			"MCP_POSIX_HELPER_MODE="+mode,
			"MCP_POSIX_GRANDCHILD_PID_FILE="+pidFile,
		),
	}, WithGracePeriod(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ := strconv.Atoi(string(data))
			if pid > 0 {
				return proc, pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = proc.Close()
	t.Fatal("grandchild PID was not written")
	return nil, 0
}

func TestPOSIX_CloseTerminatesGrandchild(t *testing.T) {
	proc, grandchildPID := startPosixGrandchildFixture(t, context.Background())
	if err := proc.Close(); err != nil {
		t.Logf("Close returned process exit error: %v", err)
	}
	if !waitForPosixProcessExit(grandchildPID, 2*time.Second) {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
		t.Fatalf("grandchild PID %d survived Process.Close", grandchildPID)
	}
}

func TestPOSIX_ContextCancelTerminatesGrandchild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	proc, grandchildPID := startPosixGrandchildFixture(t, ctx)
	defer proc.Close()

	cancel()
	if !waitForPosixProcessExit(grandchildPID, 3*time.Second) {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
		t.Fatalf("grandchild PID %d survived context cancellation", grandchildPID)
	}
}

func TestPOSIX_CloseTerminatesGrandchildAfterRootExitsFirst(t *testing.T) {
	proc, grandchildPID := startPosixGrandchildFixtureMode(t, context.Background(), "spawn_grandchild_and_exit")
	if err := proc.Wait(); err != nil {
		t.Fatalf("root helper should exit cleanly: %v", err)
	}
	if !posixProcessAlive(grandchildPID) {
		t.Fatal("fixture grandchild exited before cleanup could be tested")
	}
	if err := proc.Close(); err != nil {
		t.Fatalf("Close after root exit returned error: %v", err)
	}
	if !waitForPosixProcessExit(grandchildPID, 2*time.Second) {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
		t.Fatalf("grandchild PID %d survived after root exited first", grandchildPID)
	}
}
