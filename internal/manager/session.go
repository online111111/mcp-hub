package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/online111111/mcp-manager/internal/config"
	"github.com/online111111/mcp-manager/internal/downstream"
	"github.com/online111111/mcp-manager/internal/process"
)

// Session abstracts the downstream client session.
type Session interface {
	CallTool(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error)
	ListAllTools(ctx context.Context) ([]*mcp.Tool, error)
	Close() error
	IsClosed() bool
}

// SessionFactory creates downstream client sessions.
type SessionFactory interface {
	CreateSession(ctx context.Context, srv config.ResolvedServer, onToolListChanged func()) (Session, io.Closer, error)
}

// ProcessLauncherFn defines the process launcher signature.
type ProcessLauncherFn func(ctx context.Context, spec process.Spec, opts ...process.Option) (process.Process, error)

// DefaultSessionFactory produces real downstream sessions via HTTP or stdio worker processes.
type DefaultSessionFactory struct {
	ProcessLauncher ProcessLauncherFn
}

// NewDefaultSessionFactory creates a DefaultSessionFactory with standard process.Start.
func NewDefaultSessionFactory() *DefaultSessionFactory {
	return &DefaultSessionFactory{
		ProcessLauncher: process.Start,
	}
}

// CreateSession connects to the downstream server according to srv.Type.
func (f *DefaultSessionFactory) CreateSession(
	ctx context.Context,
	srv config.ResolvedServer,
	onToolListChanged func(),
) (Session, io.Closer, error) {
	switch srv.Type {
	case config.ServerTypeStreamableHTTP:
		opts := downstream.HTTPOptions{
			Endpoint:             srv.URL,
			Headers:              srv.Headers,
			StartupTimeout:       srv.StartupTimeout,
			DisableStandaloneSSE: true,
			OnToolListChanged:    onToolListChanged,
		}
		sess, err := downstream.DialHTTP(ctx, opts)
		if err != nil {
			return nil, nil, err
		}
		return sess, nil, nil

	case config.ServerTypeStdio:
		launcher := f.ProcessLauncher
		if launcher == nil {
			launcher = process.Start
		}

		// A resolved empty environment is intentionally empty, not inherited.
		envSlice := make([]string, 0, len(srv.Env))
		for k, v := range srv.Env {
			envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
		}

		spec := process.Spec{
			Command: srv.Command,
			Args:    srv.Args,
			Dir:     srv.Cwd,
			Env:     envSlice,
		}

		proc, err := launcher(ctx, spec)
		if err != nil {
			return nil, nil, fmt.Errorf("launch stdio process %q: %w", srv.Command, err)
		}

		ioOpts := downstream.IOOptions{
			Reader:            proc.Reader(),
			Writer:            proc.Writer(),
			StartupTimeout:    srv.StartupTimeout,
			OnToolListChanged: onToolListChanged,
		}

		sess, err := downstream.DialIO(ctx, ioOpts)
		if err != nil {
			_ = proc.Close()
			return nil, nil, fmt.Errorf("dial io for server %q: %w", srv.ID, err)
		}

		return sess, proc, nil

	default:
		return nil, nil, fmt.Errorf("unsupported server transport type: %q", srv.Type)
	}
}
