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

	// Explicit cmd.exe / cmd is an intentional shell opt-in.
	if baseName == "cmd.exe" || baseName == "cmd" {
		return ResolveResult{
			Spec:    spec,
			Warning: "explicit command 'cmd.exe' enables shell execution; arguments are passed directly without shell escaping guarantees",
		}, nil
	}

	// Windows npm installs commonly expose npx.cmd, which cannot be executed
	// safely through os/exec without an implicit shell. Normalize that layout to
	// node.exe + npx-cli.js. On POSIX, however, npx is normally a directly
	// executable shebang script and must not be forced into the Windows layout.
	if baseName == "npx" || baseName == "npx.cmd" {
		if runtime.GOOS != "windows" {
			resSpec := spec
			if !strings.ContainsAny(spec.Command, `/\\`) {
				resolved, err := lookPath(spec.Command)
				if err != nil {
					return ResolveResult{}, fmt.Errorf("%w: npx not found in PATH: %v", ErrCommandRejected, err)
				}
				resSpec.Command = resolved
			}
			return ResolveResult{Spec: resSpec}, nil
		}
		return resolveNpx(spec, lookPath, stat)
	}

	ext := strings.ToLower(filepath.Ext(spec.Command))
	if ext == ".bat" || ext == ".cmd" {
		return ResolveResult{}, fmt.Errorf(
			"%w: batch file %q cannot be executed directly; please configure the native interpreter (e.g. node.exe, python.exe) with absolute script path instead",
			ErrCommandRejected,
			spec.Command,
		)
	}

	targetPath := spec.Command
	if runtime.GOOS == "windows" && !strings.ContainsAny(spec.Command, `/\\`) {
		if resolved, err := lookPath(spec.Command); err == nil {
			targetPath = resolved
		}
	}

	targetBase := strings.ToLower(filepath.Base(targetPath))
	targetExt := strings.ToLower(filepath.Ext(targetPath))

	if targetBase == "npx" || targetBase == "npx.cmd" {
		return resolveNpx(spec, lookPath, stat)
	}

	if targetBase == "uvx" || targetBase == "uvx.exe" {
		resSpec := spec
		resSpec.Command = targetPath
		return ResolveResult{Spec: resSpec}, nil
	}

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

// resolveNpx resolves npx / npx.cmd against the standard Windows Node.js installation layout:
// requires <dir>/node.exe and <dir>/node_modules/npm/bin/npx-cli.js in the same installation directory.
func resolveNpx(
	spec Spec,
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
) (ResolveResult, error) {
	var npxPath string

	if strings.ContainsAny(spec.Command, `/\\`) {
		npxPath = spec.Command
	} else {
		if p, err := lookPath("npx.cmd"); err == nil {
			npxPath = p
		} else if p, err := lookPath("npx"); err == nil {
			npxPath = p
		} else if p, err := lookPath("node.exe"); err == nil {
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

	newArgs := make([]string, 0, 1+len(spec.Args))
	newArgs = append(newArgs, npxCli)
	newArgs = append(newArgs, spec.Args...)

	resSpec := spec
	resSpec.Command = nodeExe
	resSpec.Args = newArgs
	return ResolveResult{Spec: resSpec}, nil
}
