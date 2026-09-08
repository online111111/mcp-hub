package catalog

import (
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
)

func TestSDKHeaderAnnotationIsValidatedBeforePublication(t *testing.T) {
	tool := &mcp.Tool{Name: "invalid_header", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "number", "x-mcp-header": "Value"}}}}
	if err := ValidateTool(tool); err == nil {
		t.Fatal("SDK-incompatible definition passed validation")
	}
	catalog := NewCatalog()
	_, _, snapshot, err := catalog.UpdateServerTools("audit", []*mcp.Tool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) != 0 || snapshot.UnpublishedCount("audit") != 1 {
		t.Fatal("invalid definition entered live catalog")
	}
}

func TestPublicNameCountsTowardDefinitionSize(t *testing.T) {
	tool := &mcp.Tool{Name: "x", Description: "x", InputSchema: map[string]any{"type": "object"}}
	initial, _ := json.Marshal(tool)
	tool.Description = strings.Repeat("x", MaxToolDefinitionBytes-len(initial)+1)
	raw, _ := json.Marshal(tool)
	if len(raw) != MaxToolDefinitionBytes {
		t.Fatal("incorrect boundary fixture")
	}
	_, _, err := CloneAndAssignPublicName(tool, strings.Repeat("n", 64))
	if !errors.Is(err, ErrToolSizeExceeded) {
		t.Fatalf("renamed oversized tool accepted: %v", err)
	}
}
