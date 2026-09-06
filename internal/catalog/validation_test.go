package catalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateToolInputSchema(t *testing.T) {
	// 1. Missing InputSchema
	toolNilSchema := &mcp.Tool{
		Name: "test_tool",
	}
	if err := ValidateTool(toolNilSchema); err == nil || !errors.Is(err, ErrInputSchemaInvalid) {
		t.Errorf("expected ErrInputSchemaInvalid for nil schema, got %v", err)
	}

	// 2. Non-object InputSchema (string, array, int)
	invalidSchemas := []any{
		"not an object",
		123,
		[]any{"item"},
		true,
	}
	for _, s := range invalidSchemas {
		tool := &mcp.Tool{
			Name:        "test_tool",
			InputSchema: s,
		}
		if err := ValidateTool(tool); err == nil || !errors.Is(err, ErrInputSchemaInvalid) {
			t.Errorf("expected ErrInputSchemaInvalid for %T, got %v", s, err)
		}
	}

	// 3. Object without type="object"
	toolNoType := &mcp.Tool{
		Name:        "test_tool",
		InputSchema: map[string]any{"type": "string"},
	}
	if err := ValidateTool(toolNoType); err == nil || !errors.Is(err, ErrInputSchemaInvalid) {
		t.Errorf("expected ErrInputSchemaInvalid for type=string, got %v", err)
	}

	// 4. Object with type="object" (valid)
	toolValid := &mcp.Tool{
		Name: "test_tool",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}
	if err := ValidateTool(toolValid); err != nil {
		t.Errorf("expected valid tool, got %v", err)
	}
}

func TestValidateToolOutputSchema(t *testing.T) {
	// 1. OutputSchema nil is valid
	toolNilOutput := &mcp.Tool{
		Name:         "test_tool",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: nil,
	}
	if err := ValidateTool(toolNilOutput); err != nil {
		t.Errorf("expected nil OutputSchema to be valid, got %v", err)
	}

	// 2. OutputSchema may describe a non-object JSON result.
	toolArrayOutput := &mcp.Tool{
		Name:         "test_tool",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	if err := ValidateTool(toolArrayOutput); err != nil {
		t.Errorf("expected array OutputSchema to be valid, got %v", err)
	}

	// 3. OutputSchema with type="object" (valid)
	toolValidOutput := &mcp.Tool{
		Name:        "test_tool",
		InputSchema: map[string]any{"type": "object"},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"result": map[string]any{"type": "string"},
			},
		},
	}
	if err := ValidateTool(toolValidOutput); err != nil {
		t.Errorf("expected valid OutputSchema, got %v", err)
	}
}

func TestValidateToolSizeBounds(t *testing.T) {
	// Normal size tool
	tool := &mcp.Tool{
		Name:        "normal_tool",
		Description: "Normal description",
		InputSchema: map[string]any{"type": "object"},
	}
	if err := ValidateTool(tool); err != nil {
		t.Errorf("expected normal tool to pass validation: %v", err)
	}

	// Extreme schema: huge description / schema exceeding 256 KiB
	hugeDesc := strings.Repeat("x", MaxToolDefinitionBytes+100)
	hugeTool := &mcp.Tool{
		Name:        "huge_tool",
		Description: hugeDesc,
		InputSchema: map[string]any{"type": "object"},
	}
	if err := ValidateTool(hugeTool); err == nil || !errors.Is(err, ErrToolSizeExceeded) {
		t.Errorf("expected ErrToolSizeExceeded for huge tool, got %v", err)
	}
}

func TestToolCloningImmutability(t *testing.T) {
	orig := &mcp.Tool{
		Name:        "orig_name",
		Description: "Initial description",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "number"},
			},
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

	// Mutate original tool
	orig.Description = "Mutated description"
	orig.Name = "mutated_name"
	origSchema := orig.InputSchema.(map[string]any)
	origSchema["mutated_key"] = true

	// Cloned tool must NOT be affected
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
