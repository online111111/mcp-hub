package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestExpandEnv_Success(t *testing.T) {
	envMap := map[string]string{
		"USER":  "alice",
		"TOKEN": "secret-12345",
	}
	lookup := func(k string) (string, bool) {
		v, ok := envMap[k]
		return v, ok
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer ${TOKEN}", "Bearer secret-12345"},
		{"Hello ${USER}, welcome!", "Hello alice, welcome!"},
		{"Literal $${TOKEN}", "Literal ${TOKEN}"},
		{"Double dollar $$", "Double dollar $"},
		{"No env references here", "No env references here"},
		{"$${ESCAPE} and ${USER}", "${ESCAPE} and alice"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			res, err := config.ExpandEnv(tc.input, lookup)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if res != tc.expected {
				t.Errorf("got %q, want %q", res, tc.expected)
			}
		})
	}
}

func TestExpandEnv_MissingVar(t *testing.T) {
	envMap := map[string]string{
		"SECRET": "super-secret-token",
	}
	lookup := func(k string) (string, bool) {
		v, ok := envMap[k]
		return v, ok
	}

	input := "Bearer ${MISSING_TOKEN} and ${SECRET}"
	_, err := config.ExpandEnv(input, lookup)
	if err == nil {
		t.Fatal("expected error on missing env var, got nil")
	}

	var missingErr config.ErrMissingEnvVar
	if !errors.As(err, &missingErr) {
		t.Fatalf("expected ErrMissingEnvVar, got %T: %v", err, err)
	}
	if missingErr.Name != "MISSING_TOKEN" {
		t.Errorf("expected missing var name 'MISSING_TOKEN', got %q", missingErr.Name)
	}

	// Ensure error string does not leak secret values
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("error leaked secret token: %s", err.Error())
	}
}

func TestExpandEnv_LiteralEscapeDoesNotFailOnMissingVar(t *testing.T) {
	lookup := func(k string) (string, bool) {
		return "", false // No variables exist
	}

	input := "Literal $${NOT_EXIST_VAR}"
	res, err := config.ExpandEnv(input, lookup)
	if err != nil {
		t.Fatalf("unexpected error for literal escape: %v", err)
	}
	if res != "Literal ${NOT_EXIST_VAR}" {
		t.Errorf("got %q, want %q", res, "Literal ${NOT_EXIST_VAR}")
	}
}

func TestExpandEnv_SyntaxErrors(t *testing.T) {
	tests := []string{
		"Unclosed ${VAR",
		"Empty ${}",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := config.ExpandEnv(input, func(string) (string, bool) { return "val", true })
			if err == nil {
				t.Errorf("expected error for syntax error %q, got nil", input)
			}
		})
	}
}

func TestMergeEnv_WindowsCaseInsensitive(t *testing.T) {
	base := []string{
		"Path=C:\\Windows;C:\\Windows\\System32",
		"NODE_ENV=development",
		"TEMP=C:\\Temp",
	}
	explicit := map[string]string{
		"PATH":     "D:\\CustomNode\\bin",
		"node_env": "production",
	}

	merged := config.MergeEnv(base, explicit, true)

	// In Windows, PATH should override Path and node_env should override NODE_ENV
	if len(merged) != 3 {
		t.Errorf("expected 3 merged entries, got %d: %v", len(merged), merged)
	}

	if _, hasOldPath := merged["Path"]; hasOldPath {
		t.Errorf("expected old case 'Path' to be replaced, but it exists: %v", merged)
	}
	if merged["PATH"] != "D:\\CustomNode\\bin" {
		t.Errorf("expected PATH='D:\\CustomNode\\bin', got %q", merged["PATH"])
	}

	if _, hasOldNodeEnv := merged["NODE_ENV"]; hasOldNodeEnv {
		t.Errorf("expected old case 'NODE_ENV' to be replaced, but it exists: %v", merged)
	}
	if merged["node_env"] != "production" {
		t.Errorf("expected node_env='production', got %q", merged["node_env"])
	}
}

func TestMergeEnv_POSIXCaseSensitive(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"USER=alice",
	}
	explicit := map[string]string{
		"path": "/opt/bin", // Distinct key in POSIX
	}

	merged := config.MergeEnv(base, explicit, false)

	// In POSIX, PATH and path should both exist
	if merged["PATH"] != "/usr/bin" {
		t.Errorf("expected PATH='/usr/bin', got %q", merged["PATH"])
	}
	if merged["path"] != "/opt/bin" {
		t.Errorf("expected path='/opt/bin', got %q", merged["path"])
	}
}
