package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestDecodeStrict_Valid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", "valid.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var cfg config.Config
	if err := config.DecodeStrict(data, &cfg); err != nil {
		t.Fatalf("unexpected error decoding valid config: %v", err)
	}

	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.MCPServers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(cfg.MCPServers))
	}
}

func TestDecodeStrict_UTF8BOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"version":1,"mcpServers":{}}`)...)
	var cfg config.Config
	if err := config.DecodeStrict(data, &cfg); err != nil {
		t.Fatalf("expected UTF-8 BOM config to decode, got %v", err)
	}
	if cfg.Version != 1 {
		t.Fatalf("expected version 1, got %d", cfg.Version)
	}
}

func TestDecodeStrict_DuplicateKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", "duplicate_key.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var cfg config.Config
	err = config.DecodeStrict(data, &cfg)
	if err == nil {
		t.Fatal("expected duplicate key error, got nil")
	}
}

func TestDecodeStrict_NestedDuplicateKey(t *testing.T) {
	nestedJSON := []byte(`{
		"version": 1,
		"mcpServers": {
			"srv": {
				"type": "stdio",
				"command": "node",
				"env": {
					"KEY": "val1",
					"KEY": "val2"
				}
			}
		}
	}`)

	var cfg config.Config
	err := config.DecodeStrict(nestedJSON, &cfg)
	if err == nil {
		t.Fatal("expected error on duplicate key in nested object, got nil")
	}
}

func TestDecodeStrict_UnknownField(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", "unknown_field.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var cfg config.Config
	err = config.DecodeStrict(data, &cfg)
	if err == nil {
		t.Fatal("expected unknown field error, got nil")
	}
}

func TestDecodeStrict_ServerUnknownField(t *testing.T) {
	jsonWithUnknown := []byte(`{
		"version": 1,
		"mcpServers": {
			"fs": {
				"type": "stdio",
				"command": "node",
				"unexpectedField": "bad"
			}
		}
	}`)

	var cfg config.Config
	err := config.DecodeStrict(jsonWithUnknown, &cfg)
	if err == nil {
		t.Fatal("expected unknown field error in server config, got nil")
	}
}

func TestDecodeStrict_TrailingJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", "trailing_json.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var cfg config.Config
	err = config.DecodeStrict(data, &cfg)
	if err == nil {
		t.Fatal("expected trailing JSON error, got nil")
	}
	if !errors.Is(err, config.ErrTrailingJSON) {
		t.Logf("got error: %v", err)
	}
}

func TestDecodeStrict_TrailingTokens(t *testing.T) {
	trailingGarbage := []byte(`{
		"version": 1,
		"mcpServers": {}
	} some extra tokens`)

	var cfg config.Config
	err := config.DecodeStrict(trailingGarbage, &cfg)
	if err == nil {
		t.Fatal("expected error on trailing garbage, got nil")
	}
}

func TestDecodeStrict_TooLarge(t *testing.T) {
	largeData := make([]byte, config.MaxConfigFileSize+10)
	var cfg config.Config
	err := config.DecodeStrict(largeData, &cfg)
	if !errors.Is(err, config.ErrConfigFileTooLarge) {
		t.Fatalf("expected ErrConfigFileTooLarge, got %v", err)
	}
}
