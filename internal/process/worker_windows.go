//go:build windows

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

const (
	// WorkerFlag is the default command-line argument identifying worker mode.
	WorkerFlag = "_worker"
	// WorkerEnvVar is the environment variable set to trigger worker mode.
	WorkerEnvVar = "_MCP_HUB_WORKER_MODE"
)

// IsWorkerArg checks if the argument list indicates worker mode.
func IsWorkerArg(args []string) bool {
	return len(args) > 1 && args[1] == WorkerFlag
}

// RunWorker runs the worker loop using standard os.Stdin, os.Stdout, and os.Stderr.
func RunWorker() error {
	return RunWorkerWithIO(os.Stdin, os.Stdout, os.Stderr)
}

// RunWorkerWithIO runs the worker loop using the provided IO streams.
// It reads the bootstrap JSON line first (with 5s/1 MiB limit),
// starts the downstream process, and forwards protocol bytes.
// Worker stdout contains strictly downstream protocol bytes.
func RunWorkerWithIO(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	// 1. Read bootstrap line with 5s timeout and 1 MiB limit.
	// Must preserve pre-read bytes in the returned buffered reader.
	msg, br, err := ReadBootstrap(stdin, DefaultBootstrapTimeout)
	if err != nil {
		return fmt.Errorf("worker bootstrap failed: %w", err)
	}

	// 2. Prepare downstream command.
	cmd := exec.Command(msg.Command, msg.Args...)
	cmd.Dir = msg.Dir
	if len(msg.Env) > 0 {
		cmd.Env = msg.Env
	}
	cmd.Stderr = stderr

	downstreamStdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create downstream stdin pipe: %w", err)
	}

	downstreamStdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = downstreamStdin.Close()
		return fmt.Errorf("create downstream stdout pipe: %w", err)
	}

	// 3. Start downstream process.
	if err := cmd.Start(); err != nil {
		_ = downstreamStdin.Close()
		_ = downstreamStdout.Close()
		return fmt.Errorf("start downstream process %q: %w", msg.Command, err)
	}

	var waitErr error
	var waitOnce sync.Once
	doWait := func() error {
		waitOnce.Do(func() {
			waitErr = cmd.Wait()
		})
		return waitErr
	}

	// 4. Stdin forwarding goroutine: forwards buffered & remaining stdin to downstream.
	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		defer func() {
			_ = downstreamStdin.Close()
		}()
		_, _ = io.Copy(downstreamStdin, br)
	}()

	// 5. Stdout forwarding goroutine: forwards downstream stdout to worker stdout.
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		_, _ = io.Copy(stdout, downstreamStdout)
	}()

	// Wait for stdout forwarding to finish (downstream stdout closes when downstream exits)
	<-stdoutDone

	// Unblock stdin pipe if downstream exited before stdin reached EOF
	_ = downstreamStdin.Close()

	// Wait for downstream process exit (cmd.Wait called only once)
	err = doWait()

	return err
}
