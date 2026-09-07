//go:build windows

package process

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestWindows_ContextCancelTerminatesGrandchild(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	tempDir := t.TempDir()
	grandchildPIDFile := filepath.Join(tempDir, "grandchild.pid")
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := Start(ctx, Spec{
		Command: selfExe,
		Env: append(os.Environ(),
			"MCP_TEST_HELPER_MODE=spawn_grandchild",
			"MCP_TEST_GRANDCHILD_PID_FILE="+grandchildPIDFile,
		),
	}, WithGracePeriod(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	var grandchildPID int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(grandchildPIDFile)
		if readErr == nil {
			grandchildPID, _ = strconv.Atoi(string(data))
			if grandchildPID > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if grandchildPID <= 0 {
		t.Fatal("grandchild PID was not written")
	}

	cancel()
	if !waitForProcessExit(grandchildPID, 3*time.Second) {
		t.Fatalf("grandchild PID %d survived context cancellation", grandchildPID)
	}
}
