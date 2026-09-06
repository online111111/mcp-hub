package catalog

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFilterDisabledTools(t *testing.T) {
	tools := []*mcp.Tool{
		{Name: "tool1"},
		{Name: "tool2"},
		{Name: "tool3"},
		{Name: "sensitive_tool"},
	}

	// 1. Empty disabled list
	enabled, disabled := FilterDisabledTools(tools, nil)
	if len(enabled) != 4 || len(disabled) != 0 {
		t.Fatalf("expected 4 enabled, 0 disabled; got %d, %d", len(enabled), len(disabled))
	}

	// 2. Filter specific tools
	enabled, disabled = FilterDisabledTools(tools, []string{"sensitive_tool", "tool2"})
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled, got %d", len(enabled))
	}
	if len(disabled) != 2 {
		t.Fatalf("expected 2 disabled, got %d", len(disabled))
	}

	if enabled[0].Name != "tool1" || enabled[1].Name != "tool3" {
		t.Errorf("unexpected enabled tools: %+v", enabled)
	}
	if disabled[0].Name != "tool2" || disabled[1].Name != "sensitive_tool" {
		t.Errorf("unexpected disabled tools: %+v", disabled)
	}

	// 3. Case sensitivity: "Tool1" does not match "tool1"
	enabled, disabled = FilterDisabledTools(tools, []string{"TOOL1", "Tool1"})
	if len(enabled) != 4 || len(disabled) != 0 {
		t.Errorf("filtering should be case-sensitive; got %d enabled, %d disabled", len(enabled), len(disabled))
	}
}
