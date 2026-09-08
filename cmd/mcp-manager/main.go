package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/online111111/mcp-manager/internal/cli"
	managerprocess "github.com/online111111/mcp-manager/internal/process"
)

func main() {
	if os.Getenv(managerprocess.WorkerEnvVar) == "1" || managerprocess.IsWorkerArg(os.Args) {
		if err := managerprocess.RunWorker(); err != nil {
			fmt.Fprintf(os.Stderr, "mcp-manager worker failed: %v\n", err)
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				os.Exit(exitErr.ExitCode())
			}
			os.Exit(1)
		}
		os.Exit(0)
	}

	exitCode := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(exitCode)
}
