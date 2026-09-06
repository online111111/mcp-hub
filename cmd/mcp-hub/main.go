package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"mcp-hub/internal/cli"
	hubprocess "mcp-hub/internal/process"
)

func main() {
	if os.Getenv(hubprocess.WorkerEnvVar) == "1" || hubprocess.IsWorkerArg(os.Args) {
		if err := hubprocess.RunWorker(); err != nil {
			fmt.Fprintf(os.Stderr, "mcp-hub worker failed: %v\n", err)
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
