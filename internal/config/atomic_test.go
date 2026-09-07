package config_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-hub/internal/config"
)

const lockHelperEnv = "MCP_HUB_LOCK_HELPER"

func TestAtomicWrite_SuccessAndCAS(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialData := []byte(`{"version": 1, "mcpServers": {}}`)

	if err := config.WriteConfigFileAtomic(configPath, initialData, ""); err != nil {
		t.Fatalf("failed initial atomic write: %v", err)
	}

	readBack, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read back config: %v", err)
	}
	if string(readBack) != string(initialData) {
		t.Errorf("readback data mismatch: got %q, want %q", string(readBack), string(initialData))
	}

	initialDigest := config.ComputeDigest(initialData)
	updatedData := []byte(`{"version": 1, "mcpServers": {"s1": {"type": "stdio", "command": "npx"}}}`)
	if err := config.WriteConfigFileAtomic(configPath, updatedData, initialDigest); err != nil {
		t.Fatalf("failed CAS write with matching digest: %v", err)
	}

	readBackUpdated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read back updated config: %v", err)
	}
	if string(readBackUpdated) != string(updatedData) {
		t.Errorf("updated data mismatch")
	}

	badData := []byte(`{"version": 1, "mcpServers": {"corrupted": true}}`)
	err = config.WriteConfigFileAtomic(configPath, badData, "wrong-digest-12345")
	if err == nil {
		t.Fatal("expected ErrDigestMismatch, got nil")
	}

	var digestErr config.ErrDigestMismatch
	if !errors.As(err, &digestErr) {
		t.Fatalf("expected ErrDigestMismatch, got %T: %v", err, err)
	}

	intactData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if string(intactData) != string(updatedData) {
		t.Fatalf("file was altered despite failed CAS write! got %q", string(intactData))
	}
}

func TestAtomicWrite_LockConflict(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("failed to acquire initial lock: %v", err)
	}
	defer unlock()

	data := []byte(`{"version": 1}`)
	err = config.WriteConfigFileAtomic(configPath, data, "")
	if err == nil {
		t.Fatal("expected lock conflict error, got nil")
	}

	var lockErr config.ErrLockHeld
	if !errors.As(err, &lockErr) {
		t.Fatalf("expected ErrLockHeld, got %T: %v", err, err)
	}
	if !strings.Contains(lockErr.Details, "pid=") {
		t.Fatalf("expected holder diagnostics, got %q", lockErr.Details)
	}
}

func TestAtomicWrite_LockReleaseAllowsSubsequentWrite(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	unlock()

	if _, err := os.Stat(configPath + ".lock"); err != nil {
		t.Fatalf("lock file should persist after unlock: %v", err)
	}

	data := []byte(`{"version": 1, "mcpServers": {}}`)
	if err := config.WriteConfigFileAtomic(configPath, data, ""); err != nil {
		t.Fatalf("write failed after releasing lock: %v", err)
	}
}

func TestAcquireLock_ExistingUnlockedFileDoesNotBlock(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	lockPath := configPath + ".lock"
	if err := os.WriteFile(lockPath, []byte("pid=999999 time=stale"), 0600); err != nil {
		t.Fatalf("failed to create stale lock file: %v", err)
	}

	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("stale lock file should not block acquisition: %v", err)
	}
	unlock()

	metadata, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read persistent lock metadata: %v", err)
	}
	if strings.Contains(string(metadata), "999999") || !strings.Contains(string(metadata), fmt.Sprintf("pid=%d", os.Getpid())) {
		t.Fatalf("stale lock metadata was not replaced: %q", string(metadata))
	}
}

func TestAcquireLock_CrashReleasesKernelLock(t *testing.T) {
	if os.Getenv(lockHelperEnv) != "" {
		t.Skip("parent-only test")
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestAcquireLockHelperProcess$")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+configPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create helper stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start lock helper: %v", err)
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("lock helper did not become ready: %v", err)
	}
	if strings.TrimSpace(line) != "locked" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("unexpected lock helper output: %q", line)
	}

	_, err = config.AcquireLock(configPath)
	var lockErr config.ErrLockHeld
	if !errors.As(err, &lockErr) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("expected cross-process ErrLockHeld, got %T: %v", err, err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed to kill lock helper: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed helper to exit unsuccessfully")
	}

	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("kernel lock should be released automatically after helper death: %v", err)
	}
	unlock()
}

func TestAcquireLockHelperProcess(t *testing.T) {
	configPath := os.Getenv(lockHelperEnv)
	if configPath == "" {
		t.Skip("helper only")
	}

	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("helper failed to acquire lock: %v", err)
	}
	defer unlock()
	fmt.Println("locked")
	time.Sleep(24 * time.Hour)
}
