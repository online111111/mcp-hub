package downstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type legacyProbeFakeTransport struct{ conn *legacyProbeFakeConn }

func (t *legacyProbeFakeTransport) Connect(context.Context) (mcp.Connection, error) {
	return t.conn, nil
}

type legacyProbeFakeConn struct {
	writes chan jsonrpc.Message
	closed chan struct{}
	once   sync.Once
}

func newLegacyProbeFakeConn() *legacyProbeFakeConn {
	return &legacyProbeFakeConn{
		writes: make(chan jsonrpc.Message, 1),
		closed: make(chan struct{}),
	}
}

func (c *legacyProbeFakeConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-c.closed:
		return nil, mcp.ErrConnectionClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *legacyProbeFakeConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	select {
	case c.writes <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *legacyProbeFakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *legacyProbeFakeConn) SessionID() string { return "legacy-test" }

func TestLegacyStdioTransportHidesDiscoverProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	delegate := newLegacyProbeFakeConn()
	transport := &legacyStdioTransport{base: &legacyProbeFakeTransport{conn: delegate}}
	conn, err := transport.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	id, err := jsonrpc.MakeID(float64(1))
	if err != nil {
		t.Fatalf("MakeID: %v", err)
	}
	discover := &jsonrpc.Request{ID: id, Method: "server/discover"}
	if err := conn.Write(ctx, discover); err != nil {
		t.Fatalf("Write discover: %v", err)
	}

	select {
	case got := <-delegate.writes:
		t.Fatalf("server/discover leaked to legacy stdio peer: %#v", got)
	default:
	}

	msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read synthetic response: %v", err)
	}
	response, ok := msg.(*jsonrpc.Response)
	if !ok {
		t.Fatalf("synthetic response type = %T, want *jsonrpc.Response", msg)
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(response.Error, &rpcErr) || rpcErr.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("synthetic response error = %#v, want MethodNotFound", response.Error)
	}

	initialize := &jsonrpc.Request{ID: id, Method: "initialize"}
	if err := conn.Write(ctx, initialize); err != nil {
		t.Fatalf("Write initialize: %v", err)
	}
	select {
	case got := <-delegate.writes:
		req, ok := got.(*jsonrpc.Request)
		if !ok || req.Method != "initialize" {
			t.Fatalf("delegated message = %#v, want initialize request", got)
		}
	case <-ctx.Done():
		t.Fatal("initialize was not delegated")
	}
}
