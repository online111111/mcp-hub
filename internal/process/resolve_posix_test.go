//go:build !windows

package process

import (
	"errors"
	"os"
	"testing"
)

func TestResolveCommand_POSIXNpxUsesExecutableDirectly(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "npx" {
			return "/usr/bin/npx", nil
		}
		return "", errors.New("not found")
	}
	stat := func(string) (os.FileInfo, error) {
		return nil, errors.New("must not inspect Windows npm layout on POSIX")
	}

	got, err := ResolveCommandWithFS(Spec{Command: "npx", Args: []string{"-y", "example"}}, lookPath, stat)
	if err != nil {
		t.Fatalf("ResolveCommandWithFS: %v", err)
	}
	if got.Spec.Command != "/usr/bin/npx" {
		t.Fatalf("command = %q, want /usr/bin/npx", got.Spec.Command)
	}
	if len(got.Spec.Args) != 2 || got.Spec.Args[0] != "-y" || got.Spec.Args[1] != "example" {
		t.Fatalf("args changed unexpectedly: %#v", got.Spec.Args)
	}
}
