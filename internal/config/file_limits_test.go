package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mcp-hub/internal/config"
)

func TestLoadFileRejectsOversizedConfigBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, make([]byte, config.MaxConfigFileSize+1), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := config.LoadFile(path)
	if !errors.Is(err, config.ErrConfigFileTooLarge) {
		t.Fatalf("expected ErrConfigFileTooLarge, got %v", err)
	}
	if _, err := config.ComputeFileDigest(path); !errors.Is(err, config.ErrConfigFileTooLarge) {
		t.Fatalf("digest should enforce config limit, got %v", err)
	}
}

func TestAtomicWriteRejectsOversizedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	err := config.WriteConfigFileAtomic(path, make([]byte, config.MaxConfigFileSize+1), "")
	if !errors.Is(err, config.ErrConfigFileTooLarge) {
		t.Fatalf("expected ErrConfigFileTooLarge, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("oversized write created target: %v", statErr)
	}
}
