package config_test

import (
	"mcp-hub/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteCreatesMissingParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	data := []byte(`{"version":1,"mcpServers":{}}`)
	if err := config.WriteConfigFileAtomic(path, data, ""); err != nil {
		t.Fatalf("create config in new directory: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(data) {
		t.Fatalf("readback: %q, %v", got, err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock not cleaned up: %v", err)
	}
}
