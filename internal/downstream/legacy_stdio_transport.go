package downstream

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// legacyStdioTransport shields pre-2026-07-28 stdio servers from the SDK v1.7
// server/discover probe. Some otherwise usable legacy processes treat any first
// request other than initialize as fatal. Answering the probe locally with
// MethodNotFound makes Client.Connect take its documented legacy initialize
// fallback without ever exposing the probe to the subprocess.
type legacyStdioTransport struct {
	base mcp.Transport
}

type legacyStdioReadResult struct {
	msg jsonrpc.Message
	err error
}

type legacyStdioConn struct {
	delegate  mcp.Connection
	synthetic chan jsonrpc.Message
	incoming  chan legacyStdioReadResult
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (t *legacyStdioTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	delegate, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	c := &legacyStdioConn{
		delegate:  delegate,
		synthetic: make(chan jsonrpc.Message, 1),
		incoming:  make(chan legacyStdioReadResult, 1),
		done:      make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *legacyStdioConn) readLoop() {
	for {
		msg, err := c.delegate.Read(context.Background())
		select {
		case c.incoming <- legacyStdioReadResult{msg: msg, err: err}:
		case <-c.done:
			return
		}
		if err != nil {
			return
		}
	}
}

func (c *legacyStdioConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case msg := <-c.synthetic:
		return msg, nil
	default:
	}
	select {
	case msg := <-c.synthetic:
		return msg, nil
	case result := <-c.incoming:
		return result.msg, result.err
	case <-c.done:
		return nil, mcp.ErrConnectionClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *legacyStdioConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if req, ok := msg.(*jsonrpc.Request); ok && req.Method == "server/discover" && req.IsCall() {
		response := &jsonrpc.Response{
			ID: req.ID,
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeMethodNotFound,
				Message: "server/discover is hidden for legacy stdio compatibility",
			},
		}
		select {
		case c.synthetic <- response:
			return nil
		case <-c.done:
			return mcp.ErrConnectionClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.delegate.Write(ctx, msg)
}

func (c *legacyStdioConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.closeErr = c.delegate.Close()
	})
	return c.closeErr
}

func (c *legacyStdioConn) SessionID() string { return c.delegate.SessionID() }
