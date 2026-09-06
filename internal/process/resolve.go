package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveResult represents the resolved command execution spec and any warnings.
type ResolveResult struct {
	Spec    Spec
	Warning string
}

// ResolveCommand validates and normalizes the given command spec for safe execution.
// It uses default OS lookup and stat functions.
func ResolveCommand(spec Spec) (ResolveResult, error) {
	return ResolveCommandWithFS(spec, exec.LookPath, os.Stat)
}

// ResolveCommandWithFS allows providing custom lookPath and stat functions,
// enabling pure in-memory testing of virtual directories and platform layouts.
func ResolveCommandWithFS(
	spec Spec,
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
) (ResolveResult, error) {
	if spec.Command == "" {
		return ResolveResult{}, fmt.Errorf("%w: command cannot be empty", ErrCommandRejected)
	}

	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if stat == nil {
		stat = os.Stat
	}

	baseName := strings.ToLower(filepath.Base(spec.Command))

	// 1. Explicit cmd.exe / cmd: allowed with explicit warning.
	// "用户显式 command=cmd.exe 属主动启用 shell，原样执行并警告；不自动生成 shell 字符串，也不承诺此时没有 shell 风险。"
	if baseName == "cmd.exe" || baseName == "cmd" {
		return ResolveResult{
			Spec:    spec,
			Warning: "explicit command 'cmd.exe' enables shell execution; arguments are passed directly without shell escaping guarantees",
		}, nil
	}

	// 2. Check if the raw command is npx or npx.cmd
	if baseName == "npx" || baseName == "npx.cmd" {
		return resolveNpx(spec, lookPath, stat)
	}

	// 3. Check for .bat or .cmd extension directly on command
	ext := strings.ToLower(filepath.Ext(spec.Command))
	if ext == ".bat" || ext == ".cmd" {
		return ResolveResult{}, fmt.Errorf(
			"%w: batch file %q cannot be executed directly; please configure the native interpreter (e.g. node.exe, python.exe) with absolute script path instead",
			ErrCommandRejected,
			spec.Command,
		)
	}

	// 4. On Windows, resolve command path if no directory separator
	targetPath := spec.Command
	if runtime.GOOS == "windows" && !strings.ContainsAny(spec.Command, `/\`) {
		if resolved, err := lookPath(spec.Command); err == nil {
			targetPath = resolved
		}
	}

	targetBase := strings.ToLower(filepath.Base(targetPath))
	targetExt := strings.ToLower(filepath.Ext(targetPath))

	// If resolved target is npx.cmd
	if targetBase == "npx" || targetBase == "npx.cmd" {
		return resolveNpx(spec, lookPath, stat)
	}

	// Check if uvx:
	// "- 普通 exe 直接启动，uvx 可能是 exe，不能按名字推断为批处理。"
	// "- 原生 uvx 不被误判。"
	if targetBase == "uvx" || targetBase == "uvx.exe" {
		// uvx is a native executable on Windows, allow directly
		resSpec := spec
		resSpec.Command = targetPath
		return ResolveResult{Spec: resSpec}, nil
	}

	// If resolved target has .bat or .cmd extension
	if targetExt == ".bat" || targetExt == ".cmd" {
		return ResolveResult{}, fmt.Errorf(
			"%w: command %q resolved to batch file %q which cannot be executed directly; please configure the native interpreter with absolute script path instead",
			ErrCommandRejected,
			spec.Command,
			targetPath,
		)
	}

	resSpec := spec
	resSpec.Command = targetPath
	return ResolveResult{Spec: resSpec}, nil
}

// resolveNpx resolves npx / npx.cmd against the standard Node.js installation layout:
// requires <dir>/node.exe and <dir>/node_modules/npm/bin/npx-cli.js in the same installation directory.
func resolveNpx(
	spec Spec,
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
) (ResolveResult, error) {
	var npxPath string

	if strings.ContainsAny(spec.Command, `/\`) {
		npxPath = spec.Command
	} else {
		// Try npx.cmd then npx
		if p, err := lookPath("npx.cmd"); err == nil {
			npxPath = p
		} else if p, err := lookPath("npx"); err == nil {
			npxPath = p
		} else if p, err := lookPath("node.exe"); err == nil {
			// If node.exe found in PATH, use node's directory
			npxPath = filepath.Join(filepath.Dir(p), "npx.cmd")
		} else if p, err := lookPath("node"); err == nil {
			npxPath = filepath.Join(filepath.Dir(p), "npx.cmd")
		} else {
			return ResolveResult{}, fmt.Errorf(
				"%w: npx not found in PATH; please install Node.js or configure node.exe with absolute script path directly",
				ErrCommandRejected,
			)
		}
	}

	nodeDir := filepath.Dir(npxPath)
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
	}
	nodeExe := filepath.Join(nodeDir, nodeName)
	npxCli := filepath.Join(nodeDir, "node_modules", "npm", "bin", "npx-cli.js")

	// Verify standard layout
	if _, err := stat(nodeExe); err != nil {
		return ResolveResult{}, fmt.Errorf(
			"%w: non-standard Node/npx installation layout in %q (missing %s); please configure node.exe directly with absolute script path",
			ErrCommandRejected,
			nodeDir,
			filepath.Base(nodeExe),
		)
	}

	if _, err := stat(npxCli); err != nil {
		return ResolveResult{}, fmt.Errorf(
			"%w: non-standard Node/npx installation layout in %q (missing %s); please configure node.exe directly with absolute script path",
			ErrCommandRejected,
			nodeDir,
			filepath.Join("node_modules", "npm", "bin", "npx-cli.js"),
		)
	}

	// Standard layout verified: normalize to node.exe + npx-cli.js + original args
	newArgs := make([]string, 0, 1+len(spec.Args))
	newArgs = append(newArgs, npxCli)
	newArgs = append(newArgs, spec.Args...)

	resSpec := spec
	resSpec.Command = nodeExe
	resSpec.Args = newArgs

	return ResolveResult{Spec: resSpec}, nil
}
