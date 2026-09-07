package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrLockHeld indicates that another process currently holds the configuration lock.
type ErrLockHeld struct {
	Path    string
	Details string
}

func (e ErrLockHeld) Error() string {
	if strings.TrimSpace(e.Details) != "" {
		return fmt.Sprintf("configuration lock %q is held by %s", e.Path, strings.TrimSpace(e.Details))
	}
	return fmt.Sprintf("configuration lock %q is held; another write operation is in progress", e.Path)
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
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if info, statErr := f.Stat(); statErr == nil && info.Mode().IsRegular() && info.Size() > MaxConfigFileSize {
		return "", ErrConfigFileTooLarge
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, MaxConfigFileSize+1))
	if err != nil {
		return "", err
	}
	if n > MaxConfigFileSize {
		return "", ErrConfigFileTooLarge
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// AcquireLock attempts to acquire an OS-backed exclusive advisory lock on
// <configPath>.lock. The lock file is intentionally persistent: lock ownership is
// represented by the kernel lock on the open file, not by path existence. This
// avoids stale-lock failures after crashes and unlink/recreate races between
// writers. The file contents are diagnostic only.
func AcquireLock(configPath string) (func(), error) {
	lockPath := configPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open configuration lock %q: %w", lockPath, err)
	}

	locked, err := tryLockFile(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to acquire configuration lock %q: %w", lockPath, err)
	}
	if !locked {
		details := readLockDetails(f)
		_ = f.Close()
		return nil, ErrLockHeld{Path: lockPath, Details: details}
	}

	info := fmt.Sprintf("pid=%d time=%s", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if err := writeLockDetails(f, info); err != nil {
		_ = unlockFile(f)
		_ = f.Close()
		return nil, fmt.Errorf("failed to write configuration lock metadata %q: %w", lockPath, err)
	}

	var unlockOnce sync.Once
	unlock := func() {
		unlockOnce.Do(func() {
			_ = unlockFile(f)
			_ = f.Close()
		})
	}
	return unlock, nil
}

func readLockDetails(f *os.File) string {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeLockDetails(f *os.File, info string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.WriteString(info); err != nil {
		return err
	}
	return f.Sync()
}

// WriteConfigFileAtomic writes data to path atomically using a temporary file in the same directory,
// protected by an exclusive lock file and optional CAS expectedDigest check.
func WriteConfigFileAtomic(path string, data []byte, expectedDigest string) error {
	if len(data) > MaxConfigFileSize {
		return ErrConfigFileTooLarge
	}
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
		actualDigest, err := ComputeFileDigest(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrDigestMismatch{Expected: expectedDigest, Actual: "none (file does not exist)"}
			}
			return fmt.Errorf("failed to digest existing file for CAS check: %w", err)
		}
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

	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to secure temp file permissions: %w", err)
	}
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
	if err := syncParentDir(dir); err != nil {
		return fmt.Errorf("failed to sync configuration directory %q: %w", dir, err)
	}
	return nil
}
