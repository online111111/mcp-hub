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

var testAssignJobHook func(job windows.Handle, process windows.Handle) error

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

func (wp *windowsProcess) Reader() io.ReadCloser  { return &pipeReader{Reader: wp.stdout, proc: wp} }
func (wp *windowsProcess) Writer() io.WriteCloser { return &pipeWriter{Writer: wp.stdin, proc: wp} }

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
		if wp.stdin != nil {
			_ = wp.stdin.Close()
		}

		waitDone := make(chan struct{})
		go func() {
			_ = wp.waitInternal()
			close(waitDone)
		}()

		select {
		case <-waitDone:
		case <-time.After(wp.gracePeriod):
			_ = windows.TerminateJobObject(wp.job, 1)
			<-waitDone
		}

		if wp.job != 0 && wp.job != windows.InvalidHandle {
			_ = windows.CloseHandle(wp.job)
			wp.job = windows.InvalidHandle
		}
		if wp.stdout != nil {
			_ = wp.stdout.Close()
		}
		err = wp.waitErr
	})
	return err
}

func startPlatform(ctx context.Context, spec Spec, opts LaunchOptions) (Process, error) {
	resolved, err := ResolveCommandWithFS(spec, opts.LookPath, opts.Stat)
	if err != nil {
		return nil, err
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
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

	// Do not bind exec.Cmd directly to ctx. CommandContext only terminates the
	// immediate worker, which can leave descendants alive inside a still-open Job
	// Object. Context cancellation is handled through windowsProcess.Close below.
	cmd := exec.Command(workerExe, workerArgs...)
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}

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
	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("start worker process: %w", err)
	}

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
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("%w: nested job or permission failure: %v", ErrJobAssignFailed, assignErr)
	}

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
	go func() { _ = wp.waitInternal() }()
	go func() {
		select {
		case <-ctx.Done():
			_ = wp.Close()
		case <-wp.doneChan:
		}
	}()
	return wp, nil
}
