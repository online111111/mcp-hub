//go:build windows

package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Hook for testing Job Object assignment failures without altering system permissions.
var testAssignJobHook func(job windows.Handle, process windows.Handle) error

// windowsProcess manages a running process tree enclosed in a Windows Job Object.
type windowsProcess struct {
	cmd         *exec.Cmd
	job         windows.Handle
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	gracePeriod time.Duration

	closeOnce sync.Once
	waitOnce  sync.Once
	waitErr   error
	doneChan  chan struct{}
}

func (wp *windowsProcess) Reader() io.ReadCloser {
	return &pipeReader{Reader: wp.stdout, proc: wp}
}

func (wp *windowsProcess) Writer() io.WriteCloser {
	return &pipeWriter{Writer: wp.stdin, proc: wp}
}

func (wp *windowsProcess) Pid() int {
	if wp.cmd != nil && wp.cmd.Process != nil {
		return wp.cmd.Process.Pid
	}
	return 0
}

func (wp *windowsProcess) waitInternal() error {
	wp.waitOnce.Do(func() {
		wp.waitErr = wp.cmd.Wait()
		close(wp.doneChan)
	})
	return wp.waitErr
}

func (wp *windowsProcess) Wait() error {
	<-wp.doneChan
	return wp.waitErr
}

func (wp *windowsProcess) Close() error {
	var err error
	wp.closeOnce.Do(func() {
		// 1. Close protocol input first to signal downstream EOF
		if wp.stdin != nil {
			_ = wp.stdin.Close()
		}

		// 2. Wait up to gracePeriod (default 2s) for graceful shutdown
		waitDone := make(chan struct{})
		go func() {
			_ = wp.waitInternal()
			close(waitDone)
		}()

		select {
		case <-waitDone:
			// Terminated gracefully within grace period
		case <-time.After(wp.gracePeriod):
			// 3. Grace period expired: forcefully terminate the Job Object
			_ = windows.TerminateJobObject(wp.job, 1)
			<-waitDone
		}

		// 4. Close Job Object handle
		if wp.job != 0 && wp.job != windows.InvalidHandle {
			_ = windows.CloseHandle(wp.job)
			wp.job = windows.InvalidHandle
		}

		// 5. Close stdout pipe to unblock any pending reader
		if wp.stdout != nil {
			_ = wp.stdout.Close()
		}

		err = wp.waitErr
	})
	return err
}

func startPlatform(ctx context.Context, spec Spec, opts LaunchOptions) (Process, error) {
	// 0. Resolve command and arguments
	resolved, err := ResolveCommandWithFS(spec, opts.LookPath, opts.Stat)
	if err != nil {
		return nil, err
	}

	// 1. Create KILL_ON_JOB_CLOSE Job Object, handle not inheritable
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}

	// Explicitly ensure handle is not inheritable
	_ = windows.SetHandleInformation(job, windows.HANDLE_FLAG_INHERIT, 0)

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("set job object limit KILL_ON_JOB_CLOSE: %w", err)
	}

	// 2. Determine worker executable and arguments
	workerExe := opts.WorkerExe
	if workerExe == "" {
		workerExe, err = os.Executable()
		if err != nil {
			_ = windows.CloseHandle(job)
			return nil, fmt.Errorf("get executable path for worker: %w", err)
		}
	}

	workerArgs := opts.WorkerArgs
	if len(workerArgs) == 0 {
		workerArgs = []string{WorkerFlag}
	}

	cmd := exec.CommandContext(ctx, workerExe, workerArgs...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}

	// Set worker environment
	env := os.Environ()
	if len(opts.WorkerEnv) > 0 {
		env = append(env, opts.WorkerEnv...)
	}
	env = append(env, WorkerEnvVar+"=1")
	cmd.Env = env

	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("create worker stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("create worker stdout pipe: %w", err)
	}

	// Start worker process
	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("start worker process: %w", err)
	}

	// 3. Assign worker process to Job Object before writing bootstrap
	hProcess, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("open worker process: %w", err)
	}

	assignErr := func() error {
		if testAssignJobHook != nil {
			return testAssignJobHook(job, hProcess)
		}
		return windows.AssignProcessToJobObject(job, hProcess)
	}()
	_ = windows.CloseHandle(hProcess)

	if assignErr != nil {
		// Strictly prohibited from running without Job Object ("禁止无 Job 降级")
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("%w: nested job or permission failure: %v", ErrJobAssignFailed, assignErr)
	}

	// 4. Worker is successfully in Job Object; now send bootstrap line
	bootstrapMsg := &BootstrapMessage{
		Command: resolved.Spec.Command,
		Args:    resolved.Spec.Args,
		Dir:     resolved.Spec.Dir,
		Env:     resolved.Spec.Env,
	}

	if err := WriteBootstrap(stdinPipe, bootstrapMsg); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("send bootstrap to worker: %w", err)
	}

	wp := &windowsProcess{
		cmd:         cmd,
		job:         job,
		stdin:       stdinPipe,
		stdout:      stdoutPipe,
		gracePeriod: opts.GracePeriod,
		doneChan:    make(chan struct{}),
	}

	// Start background waiter so doneChan is closed upon natural exit
	go func() {
		_ = wp.waitInternal()
	}()

	return wp, nil
}
