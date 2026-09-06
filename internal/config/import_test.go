package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-hub/internal/config"
)

func TestImport_Success(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "config.json")

	sourceJSON := []byte(`{
		"mcpServers": {
			"imported-fs": {
				"command": "node",
				"args": ["server.js"],
				"env": {
					"SECRET_KEY": "super-secret"
				}
			},
			"imported-search": {
				"type": "http",
				"url": "https://api.example.com/mcp",
				"headers": {
					"Authorization": "Bearer token123"
				}
			}
		}
	}`)

	// 1. Dry run
	preview, err := config.ImportServers(sourceJSON, targetPath, config.ImportOptions{
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("unexpected dry-run error: %v", err)
	}

	if preview.NewCount != 2 {
		t.Errorf("expected 2 new servers in preview, got %d", preview.NewCount)
	}

	// Verify target file was NOT created during dry-run
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("target file should not exist after dry-run")
	}

	// Verify preview redacts secret values
	for _, s := range preview.ServersToAdd {
		if s.ID == "imported-search" {
			if len(s.HeaderKeys) != 1 || s.HeaderKeys[0] != "Authorization" {
				t.Errorf("expected header name 'Authorization', got %v", s.HeaderKeys)
			}
		}
		if s.ID == "imported-fs" {
			if len(s.EnvKeys) != 1 || s.EnvKeys[0] != "SECRET_KEY" {
				t.Errorf("expected env key name 'SECRET_KEY', got %v", s.EnvKeys)
			}
		}
	}

	// 2. Import without --yes must return error
	_, err = config.ImportServers(sourceJSON, targetPath, config.ImportOptions{
		DryRun: false,
		Yes:    false,
	})
	if err == nil {
		t.Fatal("expected confirmation error when --yes is false, got nil")
	}

	// 3. Import with --yes must succeed and write file
	preview, err = config.ImportServers(sourceJSON, targetPath, config.ImportOptions{
		DryRun: false,
		Yes:    true,
	})
	if err != nil {
		t.Fatalf("import with --yes failed: %v", err)
	}

	// Read and verify saved file
	var savedCfg config.Config
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if err := config.DecodeStrict(data, &savedCfg); err != nil {
		t.Fatalf("saved file failed strict decode: %v", err)
	}
	if len(savedCfg.MCPServers) != 2 {
		t.Errorf("expected 2 servers in saved file, got %d", len(savedCfg.MCPServers))
	}
	if savedCfg.MCPServers["imported-search"].Type != config.ServerTypeStreamableHTTP {
		t.Errorf("expected type streamable_http, got %s", savedCfg.MCPServers["imported-search"].Type)
	}
}

func TestImport_URLWithoutType(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "config.json")

	sourceJSON := []byte(`{
		"mcpServers": {
			"remote-no-type": {
				"url": "https://api.example.com/mcp"
			}
		}
	}`)

	// Without --remote-type: must fail
	_, err := config.ImportServers(sourceJSON, targetPath, config.ImportOptions{
		DryRun: true,
	})
	if err == nil {
		t.Fatal("expected error when URL has no type and --remote-type is missing, got nil")
	}
	if !strings.Contains(err.Error(), "--remote-type streamable_http") {
		t.Errorf("expected error instructing to specify --remote-type streamable_http, got: %v", err)
	}

	// With --remote-type streamable_http: must succeed
	preview, err := config.ImportServers(sourceJSON, targetPath, config.ImportOptions{
		DryRun:     true,
		RemoteType: "streamable_http",
	})
	if err != nil {
		t.Fatalf("import failed with --remote-type streamable_http: %v", err)
	}
	if preview.ServersToAdd[0].Type != config.ServerTypeStreamableHTTP {
		t.Errorf("expected normalized type streamable_http, got %s", preview.ServersToAdd[0].Type)
	}
}

func TestImport_DuplicateIDConflict(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "config.json")

	// Target already has server "existing-tool"
	initialTarget := []byte(`{
		"version": 1,
		"hub": {"listen": "127.0.0.1:8080"},
		"mcpServers": {
			"existing-tool": {
				"type": "stdio",
				"command": "node"
			}
		}
	}`)
	if err := os.WriteFile(targetPath, initialTarget, 0600); err != nil {
		t.Fatalf("failed to write initial target: %v", err)
	}

	// Try to import server with same ID "existing-tool"
	importJSON := []byte(`{
		"mcpServers": {
			"existing-tool": {
				"command": "python",
				"args": ["server.py"]
			}
		}
	}`)

	_, err := config.ImportServers(importJSON, targetPath, config.ImportOptions{
		Yes: true,
	})
	if err == nil {
		t.Fatal("expected duplicate server ID conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "already exists in target config") {
		t.Errorf("expected conflict rejection error, got: %v", err)
	}
}

func TestImport_MutuallyExclusiveFlags(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "config.json")

	sourceJSON := []byte(`{"mcpServers": {"s": {"command": "node"}}}`)
	_, err := config.ImportServers(sourceJSON, targetPath, config.ImportOptions{
		DryRun: true,
		Yes:    true,
	})
	if err == nil {
		t.Fatal("expected error when both --dry-run and --yes are true, got nil")
	}
}
