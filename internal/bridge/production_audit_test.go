package bridge

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBridgeSurvivesStartupDeadline(t *testing.T) {
	hub := mcp.NewServer(testImpl("lifetime-hub"), nil)
	hub.AddTool(&mcp.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil
	})
	server := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return hub }, nil))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	transport, stdin, stdout, cleanup := makePipes()
	defer cleanup()
	done := make(chan error, 1)
	go func() {
		done <- RunWithOptions(ctx, Options{Endpoint: server.URL, Stdin: stdin, Stdout: stdout, Stderr: io.Discard, StartupTimeout: time.Second})
	}()
	defer func() {
		cancel()
		cleanup()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("bridge failed to stop")
		}
	}()
	client := mcp.NewClient(testImpl("lifetime-client"), nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	time.Sleep(1200 * time.Millisecond)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ping", Arguments: map[string]any{}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("bridge stopped after startup deadline: %v", err)
	}
}
