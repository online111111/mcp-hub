package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrLockHeld indicates that another process holds the configuration lock file.
type ErrLockHeld struct {
	Path    string
	Details string
}

func (e ErrLockHeld) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("config lock file %q already exists (held by %s); manual intervention required", e.Path, e.Details)
	}
	return fmt.Sprintf("config lock file %q already exists; another write operation is in progress", e.Path)
}

// ErrDigestMismatch is returned when CAS (compare-and-swap) validation fails.
type ErrDigestMismatch struct {
	Expected string
	Actual   string
}

func (e ErrDigestMismatch) Error() string {
	return fmt.Sprintf("configuration digest mismatch: expected %s, current file is %s", e.Expected, e.Actual)
}

// ComputeDigest returns the hexadecimal SHA-256 digest of the given data.
func ComputeDigest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ComputeFileDigest reads the file at path and returns its SHA-256 digest.
func ComputeFileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ComputeDigest(data), nil
}

// AcquireLock attempts to atomically acquire an exclusive lock file (<configPath>.lock).
// It returns an unlock function on success, or ErrLockHeld if the lock already exists.
func AcquireLock(configPath string) (func(), error) {
	lockPath := configPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			details, _ := os.ReadFile(lockPath)
			return nil, ErrLockHeld{Path: lockPath, Details: string(details)}
		}
		return nil, fmt.Errorf("failed to create lock file %q: %w", lockPath, err)
	}

	// Record PID and timestamp into lock file before publishing the lock.
	info := fmt.Sprintf("pid=%d time=%s", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if _, writeErr := f.WriteString(info); writeErr != nil {
		_ = f.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to write lock file %q: %w", lockPath, writeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to close lock file %q: %w", lockPath, closeErr)
	}

	var unlockOnce sync.Once
	unlock := func() {
		unlockOnce.Do(func() {
			_ = os.Remove(lockPath)
		})
	}

	return unlock, nil
}

// WriteConfigFileAtomic writes data to path atomically using a temporary file in the same directory,
// protected by an exclusive lock file and optional CAS expectedDigest check.
func WriteConfigFileAtomic(path string, data []byte, expectedDigest string) error {
	// The sibling lock must have a parent before acquisition, just like the
	// temporary file. Creating it after AcquireLock made first writes fail.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}
	unlock, err := AcquireLock(path)
	if err != nil {
		return err
	}
	defer unlock()

	// CAS check if expectedDigest is specified
	if expectedDigest != "" {
		existingData, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrDigestMismatch{Expected: expectedDigest, Actual: "none (file does not exist)"}
			}
			return fmt.Errorf("failed to read existing file for CAS check: %w", err)
		}
		actualDigest := ComputeDigest(existingData)
		if actualDigest != expectedDigest {
			return ErrDigestMismatch{Expected: expectedDigest, Actual: actualDigest}
		}
	}

	// Create temporary file in the exact same directory to ensure same filesystem / drive
	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	cleanTemp := true
	defer func() {
		if cleanTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write to temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomically replace target file using platform-specific implementation
	if err := replaceFile(tmpPath, path); err != nil {
		return fmt.Errorf("atomic replacement failed: %w", err)
	}

	cleanTemp = false
	return nil
}
