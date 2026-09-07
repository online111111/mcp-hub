package inbound

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCP_SDK17NegotiatesStatefulLegacyProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv, cleanup := setupTestServer(t, nil, nil)
	defer cleanup()

	client := mcp.NewClient(&mcp.Implementation{Name: "sdk17-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL() + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer session.Close()

	got := session.InitializeResult()
	if got == nil {
		t.Fatal("Connect returned without an initialize/discover result")
	}
	if got.ProtocolVersion != latestStatefulProtocol {
		t.Fatalf("negotiated protocol = %q, want %q", got.ProtocolVersion, latestStatefulProtocol)
	}

	count := 0
	for range srv.publisher.Server().Sessions() {
		count++
	}
	if count != 1 {
		t.Fatalf("active SDK sessions = %d, want exactly 1 (modern discovery probe must not leak a session)", count)
	}
}

func TestMCP_SDK17BodyLimitMatchesHubLimit(t *testing.T) {
	srv, cleanup := setupTestServer(t, nil, nil)
	defer cleanup()

	// v1.7.0 defaults StreamableHTTPOptions.MaxRequestBodyBytes to 4 MiB.
	// Hub's documented hard limit is 8 MiB, so a body just above 4 MiB must
	// reach MCP parsing rather than being rejected by the SDK with 413.
	body := bytes.Repeat([]byte{' '}, (4<<20)+1024)
	req, err := http.NewRequest(http.MethodPost, srv.URL()+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		t.Fatalf("SDK applied its 4 MiB default instead of Hub's %d-byte limit", MaxBodySize)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 from MCP parsing", resp.StatusCode)
	}
}
