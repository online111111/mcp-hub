package router

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRouteToolRejectsUnsafeNameBeforeRecording(t *testing.T) {
	r := NewRouter(nil, nil)
	recorded := false
	r.SetCallRecorder(func(CallRecord) { recorded = true })

	for _, name := range []string{
		strings.Repeat("a", 65),
		"bad\x1b]0;title\x07",
		"bad/name",
	} {
		_, err := r.RouteTool(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: name}})
		if err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
	if recorded {
		t.Fatal("invalid tool name was retained in diagnostics")
	}
}
