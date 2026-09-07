//go:build !windows

package config

import "os"

func replaceFile(from, to string) error {
	return os.Rename(from, to)
}

func syncParentDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
