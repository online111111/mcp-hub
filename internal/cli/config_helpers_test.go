package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-hub/internal/cli"
)

func TestCLI_ValidateConfig(t *testing.T) {
	validPath := filepath.Join("..", "..", "testdata", "config", "valid.json")
	t.Setenv("SEARCH_TOKEN", "test-token")

	cfg, resolved, err := cli.ValidateConfig(validPath)
	if err != nil {
		t.Fatalf("ValidateConfig failed on valid fixture: %v", err)
	}

	if cfg == nil || resolved == nil {
		t.Fatal("expected non-nil cfg and resolved")
	}

	if resolved.Servers["search"].Headers["Authorization"] != "Bearer test-token" {
		t.Errorf("expected expanded auth header, got %q", resolved.Servers["search"].Headers["Authorization"])
	}
}

func TestCLI_ValidateConfig_MissingFile(t *testing.T) {
	_, _, err := cli.ValidateConfig("non-existent-config.json")
	if err == nil {
		t.Fatal("expected error on non-existent file, got nil")
	}
}

func TestCLI_ValidateConfig_MissingEnvVar(t *testing.T) {
	validPath := filepath.Join("..", "..", "testdata", "config", "valid.json")
	_ = os.Unsetenv("SEARCH_TOKEN")

	_, _, err := cli.ValidateConfig(validPath)
	if err == nil {
		t.Fatal("expected error on missing SEARCH_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "SEARCH_TOKEN") {
		t.Errorf("expected error mentioning SEARCH_TOKEN, got: %v", err)
	}
}

func TestCLI_RunImportAndFormatPreview(t *testing.T) {
	tempDir := t.TempDir()
	srcPath := filepath.Join("..", "..", "testdata", "config", "import_source.json")
	dstPath := filepath.Join(tempDir, "config.json")

	// 1. Dry run via CLI helper
	preview, err := cli.RunImport(srcPath, dstPath, "", true, false)
	if err != nil {
		t.Fatalf("CLI RunImport dry-run failed: %v", err)
	}

	formatted := cli.FormatPreview(preview)
	if !strings.Contains(formatted, "local-tool") || !strings.Contains(formatted, "remote-tool") {
		t.Errorf("formatted preview missing servers: %s", formatted)
	}
	// Check that secret value is NOT in the formatted preview
	if strings.Contains(formatted, "${SECRET_KEY}") || strings.Contains(formatted, "super-secret") {
		t.Errorf("preview contains secret references or values: %s", formatted)
	}

	// 2. Apply import with --yes
	preview, err = cli.RunImport(srcPath, dstPath, "", false, true)
	if err != nil {
		t.Fatalf("CLI RunImport apply failed: %v", err)
	}
	if preview.NewCount != 2 {
		t.Errorf("expected 2 new servers, got %d", preview.NewCount)
	}

	// Verify target file exists and is valid
	t.Setenv("SECRET_KEY", "dummy-secret")
	_, resolved, err := cli.ValidateConfig(dstPath)
	if err != nil {
		t.Fatalf("imported config validation failed: %v", err)
	}
	if len(resolved.Servers) != 2 {
		t.Errorf("expected 2 resolved servers, got %d", len(resolved.Servers))
	}
}
