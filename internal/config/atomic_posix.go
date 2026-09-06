//go:build !windows

package config

import (
	"os"
)

func replaceFile(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	// On POSIX, enforce 0600 file permissions for configuration files
	return os.Chmod(to, 0600)
}
