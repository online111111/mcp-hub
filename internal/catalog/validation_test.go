package catalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateToolInputSchema(t *testing.T) {
	toolNilSchema := &mcp.Tool{Name: "test_tool"}
	if err := ValidateTool(toolNilSchema); err == nil || !errors.Is(err, ErrInputSchemaInvalid) {
		t.Errorf("expected ErrInputSchemaInvalid for nil schema, got %v", err)
	}

	invalidSchemas := []any{"not an object", 123, []any{"item"}, true}
	for _, s := range invalidSchemas {
		tool := &mcp.Tool{Name: "test_tool", InputSchema: s}
		if err := ValidateTool(tool); err == nil || !errors.Is(err, ErrInputSchemaInvalid) {
			t.Errorf("expected ErrInputSchemaInvalid for %T, got %v", s, err)
		}
	}

	toolNoType := &mcp.Tool{Name: "test_tool", InputSchema: map[string]any{"type": "string"}}
	if err := ValidateTool(toolNoType); err == nil || !errors.Is(err, ErrInputSchemaInvalid) {
		t.Errorf("expected ErrInputSchemaInvalid for type=string, got %v", err)
	}

	toolValid := &mcp.Tool{
		Name: "test_tool",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	}
	if err := ValidateTool(toolValid); err != nil {
		t.Errorf("expected valid tool, got %v", err)
	}
}

func TestValidateToolOutputSchema(t *testing.T) {
	toolNilOutput := &mcp.Tool{
		Name:         "test_tool",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: nil,
	}
	if err := ValidateTool(toolNilOutput); err != nil {
		t.Errorf("expected nil OutputSchema to be valid, got %v", err)
	}

	for _, schema := range []any{
		map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		map[string]any{"type": "string"},
		[]any{"not", "a", "schema", "object"},
	} {
		tool := &mcp.Tool{
			Name:         "test_tool",
			InputSchema:  map[string]any{"type": "object"},
			OutputSchema: schema,
		}
		if err := ValidateTool(tool); err == nil || !errors.Is(err, ErrOutputSchemaInvalid) {
			t.Errorf("expected ErrOutputSchemaInvalid for %T, got %v", schema, err)
		}
	}

	toolValidOutput := &mcp.Tool{
		Name:        "test_tool",
		InputSchema: map[string]any{"type": "object"},
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"result": map[string]any{"type": "string"}},
		},
	}
	if err := ValidateTool(toolValidOutput); err != nil {
		t.Errorf("expected valid OutputSchema, got %v", err)
	}
}

func TestValidateToolSizeBounds(t *testing.T) {
	tool := &mcp.Tool{Name: "normal_tool", Description: "Normal description", InputSchema: map[string]any{"type": "object"}}
	if err := ValidateTool(tool); err != nil {
		t.Errorf("expected normal tool to pass validation: %v", err)
	}

	hugeDesc := strings.Repeat("x", MaxToolDefinitionBytes+100)
	hugeTool := &mcp.Tool{Name: "huge_tool", Description: hugeDesc, InputSchema: map[string]any{"type": "object"}}
	if err := ValidateTool(hugeTool); err == nil || !errors.Is(err, ErrToolSizeExceeded) {
		t.Errorf("expected ErrToolSizeExceeded for huge tool, got %v", err)
	}
}

func TestToolCloningImmutability(t *testing.T) {
	orig := &mcp.Tool{
		Name:        "orig_name",
		Description: "Initial description",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"count": map[string]any{"type": "number"}},
		},
	}

	cloned, size, err := CloneAndAssignPublicName(orig, "public__orig_name")
	if err != nil {
		t.Fatalf("CloneAndAssignPublicName failed: %v", err)
	}
	if size <= 0 {
		t.Errorf("expected size > 0, got %d", size)
	}
	if cloned.Name != "public__orig_name" {
		t.Errorf("expected cloned name 'public__orig_name', got %s", cloned.Name)
	}

	orig.Description = "Mutated description"
	orig.Name = "mutated_name"
	origSchema := orig.InputSchema.(map[string]any)
	origSchema["mutated_key"] = true

	if cloned.Description != "Initial description" {
		t.Errorf("clone was affected by description mutation: %s", cloned.Description)
	}
	if cloned.Name != "public__orig_name" {
		t.Errorf("clone was affected by name mutation: %s", cloned.Name)
	}
	clonedSchema := cloned.InputSchema.(map[string]any)
	if _, ok := clonedSchema["mutated_key"]; ok {
		t.Errorf("clone was affected by input schema mutation")
	}
}
