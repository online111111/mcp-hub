//go:build windows

package process

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockFileInfo implements os.FileInfo for virtual directory testing.
type mockFileInfo struct {
	name  string
	isDir bool
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return 100 }
func (m mockFileInfo) Mode() os.FileMode  { return 0755 }
func (m mockFileInfo) ModTime() time.Time { return time.Now() }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() any           { return nil }

func TestResolveCommand_LookPathResolvesToBatchRejected(t *testing.T) {
	mockLookPath := func(file string) (string, error) {
		if file == "mvn" {
			return `C:\tools\maven\bin\mvn.cmd`, nil
		}
		return "", os.ErrNotExist
	}

	spec := Spec{Command: "mvn", Args: []string{"clean", "install"}}
	_, err := ResolveCommandWithFS(spec, mockLookPath, nil)
	if err == nil {
		t.Fatalf("expected error when command resolves to .cmd, got nil")
	}
	if !errors.Is(err, ErrCommandRejected) {
		t.Fatalf("expected ErrCommandRejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "mvn.cmd") {
		t.Errorf("expected error mentioning mvn.cmd, got: %v", err)
	}
}

func TestResolveCommand_ExplicitCmdExeAllowedWithWarning(t *testing.T) {
	cmds := []string{"cmd.exe", "cmd", "CMD.EXE", `C:\Windows\System32\cmd.exe`}
	for _, c := range cmds {
		spec := Spec{Command: c, Args: []string{"/c", "dir"}}
		res, err := ResolveCommand(spec)
		if err != nil {
			t.Fatalf("expected cmd.exe to be allowed, got error: %v", err)
		}
		if res.Warning == "" {
			t.Errorf("expected warning for explicit cmd.exe %q, got empty warning", c)
		}
		if res.Spec.Command != c {
			t.Errorf("expected command preserved as %q, got %q", c, res.Spec.Command)
		}
	}
}

func TestResolveCommand_UvxAcceptedAsNativeExe(t *testing.T) {
	mockLookPath := func(file string) (string, error) {
		if file == "uvx" {
			return `C:\Users\tester\.local\bin\uvx.exe`, nil
		}
		return "", os.ErrNotExist
	}

	spec := Spec{Command: "uvx", Args: []string{"ruff", "check"}}
	res, err := ResolveCommandWithFS(spec, mockLookPath, nil)
	if err != nil {
		t.Fatalf("expected uvx to be accepted, got error: %v", err)
	}
	if !strings.HasSuffix(res.Spec.Command, "uvx.exe") {
		t.Errorf("expected resolved command to be uvx.exe, got: %q", res.Spec.Command)
	}
	if len(res.Spec.Args) != 2 || res.Spec.Args[0] != "ruff" {
		t.Errorf("args not preserved, got: %v", res.Spec.Args)
	}
}

func TestResolveCommand_NpxStandardLayout(t *testing.T) {
	nodeDir := filepath.Join("C:", "nodejs")
	nodeExe := filepath.Join(nodeDir, "node.exe")
	npxCmd := filepath.Join(nodeDir, "npx.cmd")
	npxCli := filepath.Join(nodeDir, "node_modules", "npm", "bin", "npx-cli.js")

	mockFS := map[string]bool{
		nodeExe: true,
		npxCmd:  true,
		npxCli:  true,
	}

	mockLookPath := func(file string) (string, error) {
		if file == "npx.cmd" || file == "npx" {
			return npxCmd, nil
		}
		return "", os.ErrNotExist
	}

	mockStat := func(path string) (os.FileInfo, error) {
		if mockFS[path] {
			return mockFileInfo{name: filepath.Base(path)}, nil
		}
		return nil, os.ErrNotExist
	}

	spec := Spec{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
	}

	res, err := ResolveCommandWithFS(spec, mockLookPath, mockStat)
	if err != nil {
		t.Fatalf("expected standard npx layout resolution to succeed, got: %v", err)
	}

	if res.Spec.Command != nodeExe {
		t.Errorf("expected command normalized to node.exe (%q), got %q", nodeExe, res.Spec.Command)
	}

	expectedArgs := []string{npxCli, "-y", "@modelcontextprotocol/server-memory"}
	if len(res.Spec.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d: %v", len(expectedArgs), len(res.Spec.Args), res.Spec.Args)
	}
	for i, arg := range expectedArgs {
		if res.Spec.Args[i] != arg {
			t.Errorf("arg[%d]: expected %q, got %q", i, arg, res.Spec.Args[i])
		}
	}
}

func TestResolveCommand_NpxNonStandardLayout_GivesRemediationAdvice(t *testing.T) {
	nodeDir := filepath.Join("C:", "custom-node")
	nodeExe := filepath.Join(nodeDir, "node.exe")
	npxCmd := filepath.Join(nodeDir, "npx.cmd")
	// npx-cli.js is missing!

	mockFS := map[string]bool{
		nodeExe: true,
		npxCmd:  true,
	}

	mockLookPath := func(file string) (string, error) {
		if file == "npx.cmd" || file == "npx" {
			return npxCmd, nil
		}
		return "", os.ErrNotExist
	}

	mockStat := func(path string) (os.FileInfo, error) {
		if mockFS[path] {
			return mockFileInfo{name: filepath.Base(path)}, nil
		}
		return nil, os.ErrNotExist
	}

	spec := Spec{Command: "npx", Args: []string{"some-tool"}}
	_, err := ResolveCommandWithFS(spec, mockLookPath, mockStat)
	if err == nil {
		t.Fatalf("expected error for non-standard npx layout, got nil")
	}
	if !errors.Is(err, ErrCommandRejected) {
		t.Fatalf("expected ErrCommandRejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "non-standard Node/npx installation layout") {
		t.Errorf("expected error mentioning non-standard layout, got: %v", err)
	}
	if !strings.Contains(err.Error(), "please configure node.exe directly") {
		t.Errorf("expected error giving remediation suggestion, got: %v", err)
	}
}
