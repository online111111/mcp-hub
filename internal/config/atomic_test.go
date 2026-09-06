package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mcp-hub/internal/config"
)

func TestAtomicWrite_SuccessAndCAS(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialData := []byte(`{"version": 1, "mcpServers": {}}`)

	// 1. Initial write (no CAS expected digest)
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

	// 2. CAS with matching digest succeeds
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

	// 3. CAS with mismatched digest fails and retains existing file untouched!
	badData := []byte(`{"version": 1, "mcpServers": {"corrupted": true}}`)
	err = config.WriteConfigFileAtomic(configPath, badData, "wrong-digest-12345")
	if err == nil {
		t.Fatal("expected ErrDigestMismatch, got nil")
	}

	var digestErr config.ErrDigestMismatch
	if !errors.As(err, &digestErr) {
		t.Fatalf("expected ErrDigestMismatch, got %T: %v", err, err)
	}

	// Verify original file remains intact after failed write
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

	// 1. Manually acquire lock
	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("failed to acquire initial lock: %v", err)
	}
	defer unlock()

	// 2. Attempting another write while lock is held must fail with ErrLockHeld
	data := []byte(`{"version": 1}`)
	err = config.WriteConfigFileAtomic(configPath, data, "")
	if err == nil {
		t.Fatal("expected lock conflict error, got nil")
	}

	var lockErr config.ErrLockHeld
	if !errors.As(err, &lockErr) {
		t.Fatalf("expected ErrLockHeld, got %T: %v", err, err)
	}
}

func TestAtomicWrite_LockReleaseAllowsSubsequentWrite(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	unlock, err := config.AcquireLock(configPath)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Release lock
	unlock()

	// Now write should succeed
	data := []byte(`{"version": 1, "mcpServers": {}}`)
	if err := config.WriteConfigFileAtomic(configPath, data, ""); err != nil {
		t.Fatalf("write failed after releasing lock: %v", err)
	}
}
