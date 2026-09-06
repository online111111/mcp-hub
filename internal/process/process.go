package process

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

var (
	// ErrProcessClosed is returned when an operation is attempted on an already closed process.
	ErrProcessClosed = errors.New("process: already closed")
	// ErrJobAssignFailed is returned when adding a process to the Job Object fails.
	ErrJobAssignFailed = errors.New("process: failed to assign process to Job Object")
	// ErrCommandRejected is returned when a command (e.g. raw .bat/.cmd) is rejected for security.
	ErrCommandRejected = errors.New("process: command rejected")
	// ErrTimeout is returned when a bootstrap read or process operation times out.
	ErrTimeout = errors.New("process: operation timed out")
)

// Spec describes a process execution specification.
type Spec struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
}

// Process represents a managed running process tree.
type Process interface {
	// Reader returns the stdout stream of the downstream process.
	Reader() io.ReadCloser
	// Writer returns the stdin stream of the downstream process.
	Writer() io.WriteCloser
	// Wait waits for the downstream process to exit and returns its exit error.
	// It is safe to call multiple times concurrently; the result is cached.
	Wait() error
	// Close performs graceful shutdown: closes stdin, waits for the grace period,
	// then forcefully terminates the process tree and reaps all resources.
	// Safe to call multiple times idempotently.
	Close() error
	// Pid returns the worker or root process ID.
	Pid() int
}

// LaunchOptions provides configuration for process launching.
type LaunchOptions struct {
	WorkerExe   string
	WorkerArgs  []string
	WorkerEnv   []string
	GracePeriod time.Duration
	Stderr      io.Writer
	LookPath    func(string) (string, error)
	Stat        func(string) (os.FileInfo, error)
}

// Option configures process launching.
type Option func(*LaunchOptions)

// WithWorkerBinary specifies custom worker executable path and arguments.
func WithWorkerBinary(exe string, args ...string) Option {
	return func(o *LaunchOptions) {
		o.WorkerExe = exe
		o.WorkerArgs = args
	}
}

// WithGracePeriod configures the graceful exit grace period before force killing.
func WithGracePeriod(d time.Duration) Option {
	return func(o *LaunchOptions) {
		o.GracePeriod = d
	}
}

// WithStderr configures the writer to which worker/downstream stderr is forwarded.
func WithStderr(w io.Writer) Option {
	return func(o *LaunchOptions) {
		o.Stderr = w
	}
}

// WithLookPath configures custom command lookup function (useful for tests).
func WithLookPath(fn func(string) (string, error)) Option {
	return func(o *LaunchOptions) {
		o.LookPath = fn
	}
}

// WithStat configures custom file stat function (useful for tests).
func WithStat(fn func(string) (os.FileInfo, error)) Option {
	return func(o *LaunchOptions) {
		o.Stat = fn
	}
}

// defaultLaunchOptions returns default options.
func defaultLaunchOptions() LaunchOptions {
	return LaunchOptions{
		GracePeriod: 2 * time.Second,
	}
}

// Start launches a managed process tree according to the platform-specific implementation.
func Start(ctx context.Context, spec Spec, opts ...Option) (Process, error) {
	options := defaultLaunchOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return startPlatform(ctx, spec, options)
}

// pipeReader wraps an io.Reader and delegates Close() to Process.Close().
type pipeReader struct {
	io.Reader
	proc Process
	once sync.Once
}

func (pr *pipeReader) Close() error {
	var err error
	pr.once.Do(func() {
		err = pr.proc.Close()
	})
	return err
}

// pipeWriter wraps an io.Writer and delegates Close() to Process.Close().
type pipeWriter struct {
	io.Writer
	proc Process
	once sync.Once
}

func (pw *pipeWriter) Close() error {
	var err error
	pw.once.Do(func() {
		err = pw.proc.Close()
	})
	return err
}
