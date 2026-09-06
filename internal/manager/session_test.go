package manager

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcp-hub/internal/config"
	"mcp-hub/internal/process"
)

type mockProcess struct {
	reader   io.ReadCloser
	writer   io.WriteCloser
	closeErr error
	closed   atomic.Bool
}

func (p *mockProcess) Reader() io.ReadCloser  { return p.reader }
func (p *mockProcess) Writer() io.WriteCloser { return p.writer }
func (p *mockProcess) Wait() error            { return nil }
func (p *mockProcess) Close() error {
	p.closed.Store(true)
	if p.reader != nil {
		_ = p.reader.Close()
	}
	if p.writer != nil {
		_ = p.writer.Close()
	}
	return p.closeErr
}
func (p *mockProcess) Pid() int { return 12345 }

func TestDefaultSessionFactory_UnsupportedType(t *testing.T) {
	factory := NewDefaultSessionFactory()
	srv := config.ResolvedServer{
		ID:   "srv",
		Type: config.ServerType("unknown"),
	}

	_, _, err := factory.CreateSession(context.Background(), srv, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported server transport type") {
		t.Fatalf("expected unsupported server transport type error, got %v", err)
	}
}

func TestDefaultSessionFactory_StdioLaunchFailure(t *testing.T) {
	factory := &DefaultSessionFactory{
		ProcessLauncher: func(ctx context.Context, spec process.Spec, opts ...process.Option) (process.Process, error) {
			return nil, errors.New("cannot execute binary")
		},
	}

	srv := config.ResolvedServer{
		ID:      "srv",
		Type:    config.ServerTypeStdio,
		Command: "nonexistent-cmd",
		Args:    []string{"--arg1"},
		Cwd:     "/tmp",
		Env:     map[string]string{"K": "V"},
	}

	_, _, err := factory.CreateSession(context.Background(), srv, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot execute binary") {
		t.Fatalf("expected binary execution error, got %v", err)
	}
}

func TestDefaultSessionFactory_StdioDialFailureClosesProcess(t *testing.T) {
	r, w := io.Pipe()
	mockProc := &mockProcess{
		reader: r,
		writer: w,
	}

	var launchedSpec process.Spec
	factory := &DefaultSessionFactory{
		ProcessLauncher: func(ctx context.Context, spec process.Spec, opts ...process.Option) (process.Process, error) {
			launchedSpec = spec
			return mockProc, nil
		},
	}

	srv := config.ResolvedServer{
		ID:             "srv",
		Type:           config.ServerTypeStdio,
		Command:        "node",
		Args:           []string{"server.js"},
		Cwd:            "C:\\project",
		Env:            map[string]string{"FOO": "BAR"},
		StartupTimeout: 50 * time.Millisecond,
	}

	// DialIO will fail because mockProc is an empty pipe with no MCP server response
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Close writer so DialIO hits EOF or times out promptly
	_ = w.Close()

	_, _, err := factory.CreateSession(ctx, srv, nil)
	if err == nil {
		t.Fatal("expected error on empty pipe, got nil")
	}

	// Verify launcher received correct spec
	if launchedSpec.Command != "node" || len(launchedSpec.Args) != 1 || launchedSpec.Args[0] != "server.js" {
		t.Fatalf("unexpected launched spec: %+v", launchedSpec)
	}
	if launchedSpec.Dir != "C:\\project" {
		t.Fatalf("unexpected dir in spec: %s", launchedSpec.Dir)
	}

	// Verify process was cleaned up on dial failure
	if !mockProc.closed.Load() {
		t.Fatal("expected process to be closed when dial failed")
	}
}
