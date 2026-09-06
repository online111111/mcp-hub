package process

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveCommand_BatchFilesRejected(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"direct bat", "run.bat"},
		{"direct cmd", "setup.cmd"},
		{"absolute bat", `C:\scripts\deploy.bat`},
		{"uppercase BAT", "BUILD.BAT"},
		{"uppercase CMD", "START.CMD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := Spec{Command: tc.command}
			_, err := ResolveCommand(spec)
			if err == nil {
				t.Fatalf("expected error for batch file %q, got nil", tc.command)
			}
			if !errors.Is(err, ErrCommandRejected) {
				t.Fatalf("expected ErrCommandRejected, got %v", err)
			}
			if !strings.Contains(err.Error(), "native interpreter") {
				t.Errorf("error message should advise specifying a native interpreter, got: %v", err)
			}
		})
	}
}

func TestResolveCommand_EmptyCommand(t *testing.T) {
	spec := Spec{Command: ""}
	_, err := ResolveCommand(spec)
	if err == nil {
		t.Fatalf("expected error for empty command, got nil")
	}
}
