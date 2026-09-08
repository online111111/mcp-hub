package downstream

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestPeerDisconnectClosesSessionWrapper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientIO, serverIO, cleanup := newTestPipePair()
	defer cleanup()
	server := mcp.NewServer(serverImpl("disconnect-audit"), nil)
	peer, err := server.Connect(ctx, serverIO, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	session, err := DialIO(ctx, IOOptions{Reader: clientIO.Reader, Writer: clientIO.Writer, StartupTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_ = peer.Close()
	until := time.Now().Add(time.Second)
	for !session.IsClosed() && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if !session.IsClosed() {
		t.Fatal("peer closed but wrapper remained ready")
	}
}
