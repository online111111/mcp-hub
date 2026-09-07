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
		if pp.stdin != nil {
			_ = pp.stdin.Close()
		}

		pid := pp.Pid()
		pgid := pid // Process group id equals child pid because Setpgid: true.

		// Give the root process a chance to exit naturally after stdin closes.
		// Even if it exits early, descendants may still be alive in the process
		// group; the group must therefore be checked independently of cmd.Wait.
		select {
		case <-pp.doneChan:
		case <-time.After(pp.gracePeriod):
		}

		if processGroupAlive(pgid) {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			if !waitForProcessGroupExit(pgid, pp.gracePeriod) {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				_ = waitForProcessGroupExit(pgid, pp.gracePeriod)
			}
		}

		// Reap the root even when group cleanup was required. waitInternal is
		// already running from startPlatform and caches the result.
		<-pp.doneChan

		if pp.stdout != nil {
			_ = pp.stdout.Close()
		}
		err = pp.waitErr
	})
	return err
}

func processGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

func waitForProcessGroupExit(pgid int, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processGroupAlive(pgid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !processGroupAlive(pgid)
}

func startPlatform(ctx context.Context, spec Spec, opts LaunchOptions) (Process, error) {
	resolved, err := ResolveCommandWithFS(spec, opts.LookPath, opts.Stat)
	if err != nil {
		return nil, err
	}

	// Do not bind exec.Cmd directly to ctx. CommandContext kills only the
	// immediate child, which can leave descendants in the process group alive.
	// Cancellation is handled below by Process.Close so the whole group is
	// terminated consistently.
	cmd := exec.Command(resolved.Spec.Command, resolved.Spec.Args...)
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

	go func() {
		select {
		case <-ctx.Done():
			_ = pp.Close()
		case <-pp.doneChan:
		}
	}()

	return pp, nil
}
