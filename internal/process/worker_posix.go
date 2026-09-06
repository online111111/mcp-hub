//go:build !windows

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

const (
	WorkerFlag   = "_worker"
	WorkerEnvVar = "_MCP_HUB_WORKER_MODE"
)

func IsWorkerArg(args []string) bool {
	return len(args) > 1 && args[1] == WorkerFlag
}

func RunWorker() error {
	return RunWorkerWithIO(os.Stdin, os.Stdout, os.Stderr)
}

func RunWorkerWithIO(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	msg, br, err := ReadBootstrap(stdin, DefaultBootstrapTimeout)
	if err != nil {
		return fmt.Errorf("worker bootstrap failed: %w", err)
	}

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

	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		defer func() {
			_ = downstreamStdin.Close()
		}()
		_, _ = io.Copy(downstreamStdin, br)
	}()

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		_, _ = io.Copy(stdout, downstreamStdout)
	}()

	<-stdoutDone
	_ = downstreamStdin.Close()
	err = doWait()

	return err
}
