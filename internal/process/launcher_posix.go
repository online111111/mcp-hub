//go:build !windows

package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type posixProcess struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	gracePeriod time.Duration

	closeOnce sync.Once
	waitOnce  sync.Once
	waitErr   error
	doneChan  chan struct{}
}

func (pp *posixProcess) Reader() io.ReadCloser {
	return &pipeReader{Reader: pp.stdout, proc: pp}
}

func (pp *posixProcess) Writer() io.WriteCloser {
	return &pipeWriter{Writer: pp.stdin, proc: pp}
}

func (pp *posixProcess) Pid() int {
	if pp.cmd != nil && pp.cmd.Process != nil {
		return pp.cmd.Process.Pid
	}
	return 0
}

func (pp *posixProcess) waitInternal() error {
	pp.waitOnce.Do(func() {
		pp.waitErr = pp.cmd.Wait()
		close(pp.doneChan)
	})
	return pp.waitErr
}

func (pp *posixProcess) Wait() error {
	<-pp.doneChan
	return pp.waitErr
}

func (pp *posixProcess) Close() error {
	var err error
	pp.closeOnce.Do(func() {
		// 1. Close protocol input first
		if pp.stdin != nil {
			_ = pp.stdin.Close()
		}

		pid := pp.Pid()
		pgid := pid // Process group id equals child pid because Setpgid: true

		// 2. Wait up to gracePeriod for graceful exit
		waitDone := make(chan struct{})
		go func() {
			_ = pp.waitInternal()
			close(waitDone)
		}()

		select {
		case <-waitDone:
			// Terminated gracefully within grace period
		case <-time.After(pp.gracePeriod):
			// 3. Send SIGTERM to process group
			if pgid > 0 {
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			}
			select {
			case <-waitDone:
			case <-time.After(pp.gracePeriod):
				// 4. Send SIGKILL to process group if SIGTERM didn't work
				if pgid > 0 {
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
				}
				<-waitDone
			}
		}

		// 5. Close stdout pipe
		if pp.stdout != nil {
			_ = pp.stdout.Close()
		}

		err = pp.waitErr
	})
	return err
}

func startPlatform(ctx context.Context, spec Spec, opts LaunchOptions) (Process, error) {
	resolved, err := ResolveCommandWithFS(spec, opts.LookPath, opts.Stat)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, resolved.Spec.Command, resolved.Spec.Args...)
	cmd.Dir = resolved.Spec.Dir
	if len(resolved.Spec.Env) > 0 {
		cmd.Env = resolved.Spec.Env
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}

	// New process group for isolated tree management on POSIX
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		return nil, fmt.Errorf("start process %q: %w", resolved.Spec.Command, err)
	}

	pp := &posixProcess{
		cmd:         cmd,
		stdin:       stdinPipe,
		stdout:      stdoutPipe,
		gracePeriod: opts.GracePeriod,
		doneChan:    make(chan struct{}),
	}

	go func() {
		_ = pp.waitInternal()
	}()

	return pp, nil
}
