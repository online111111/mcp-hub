package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadFileLimited reads one configuration snapshot without ever buffering more
// than MaxConfigFileSize+1 bytes. The post-read length check protects against
// special files and size races that make a prior Stat check insufficient.
func ReadFileLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if info, statErr := f.Stat(); statErr == nil && info.Mode().IsRegular() && info.Size() > MaxConfigFileSize {
		return nil, ErrConfigFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxConfigFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxConfigFileSize {
		return nil, ErrConfigFileTooLarge
	}
	return data, nil
}

// LoadFile reads, strictly decodes, validates, and resolves a configuration.
// It is side-effect free: no listeners are bound and no downstream processes
// or network connections are started.
func LoadFile(configPath string) (*Config, *ResolvedConfig, error) {
	data, err := ReadFileLimited(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config file %q: %w", configPath, err)
	}
	return Parse(data, filepath.Dir(configPath))
}

// Parse strictly decodes, validates, and resolves one immutable byte snapshot.
// configDir is used only for relative command and working-directory paths.
func Parse(data []byte, configDir string) (*Config, *ResolvedConfig, error) {
	var raw Config
	if err := DecodeStrict(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("strict decode failed: %w", err)
	}
	if err := Validate(&raw); err != nil {
		return nil, nil, fmt.Errorf("validation failed: %w", err)
	}

	resolved, err := Resolve(&raw, configDir, os.LookupEnv)
	if err != nil {
		return &raw, nil, fmt.Errorf("environment resolution failed: %w", err)
	}
	return &raw, resolved, nil
}
