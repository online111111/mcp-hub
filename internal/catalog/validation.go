package catalog

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrInputSchemaInvalid indicates invalid input schema.
	ErrInputSchemaInvalid = errors.New("input schema invalid")

	// ErrOutputSchemaInvalid indicates invalid output schema.
	ErrOutputSchemaInvalid = errors.New("output schema invalid")

	// ErrToolSizeExceeded indicates tool definition exceeds 256 KiB limit.
	ErrToolSizeExceeded = errors.New("tool definition size exceeds limit")
)

// validateSchemaObject verifies that schema marshals to a JSON object with top-level type == "object".
func validateSchemaObject(schema any, schemaName string) error {
	if schema == nil {
		return fmt.Errorf("%s cannot be nil", schemaName)
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("%s cannot be marshaled to JSON: %w", schemaName, err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("%s must be a JSON object: %w", schemaName, err)
	}
	if m == nil {
		return fmt.Errorf("%s must be a non-null JSON object", schemaName)
	}

	typ, ok := m["type"]
	if !ok || typ != "object" {
		return fmt.Errorf(`%s must have type "object" (got %v)`, schemaName, typ)
	}

	return nil
}

// ValidateTool validates the schema and size bounds of an *mcp.Tool.
// It checks:
// 1. Tool is non-nil and Name is non-empty.
// 2. InputSchema is a valid JSON object with top-level type == "object".
// 3. OutputSchema (if present) is also a JSON object with top-level type == "object".
//    This deliberately matches the contract enforced by the pinned MCP Go SDK's
//    Server.AddTool implementation, which panics for non-object output schemas.
// 4. JSON-encoded size does not exceed MaxToolDefinitionBytes (256 KiB).
func ValidateTool(tool *mcp.Tool) error {
	if tool == nil {
		return errors.New("tool is nil")
	}
	if tool.Name == "" {
		return ErrEmptyToolName
	}

	if err := validateSchemaObject(tool.InputSchema, "input schema"); err != nil {
		return fmt.Errorf("%w: %v", ErrInputSchemaInvalid, err)
	}

	if tool.OutputSchema != nil {
		if err := validateSchemaObject(tool.OutputSchema, "output schema"); err != nil {
			return fmt.Errorf("%w: %v", ErrOutputSchemaInvalid, err)
		}
	}

	data, err := json.Marshal(tool)
	if err != nil {
		return fmt.Errorf("tool marshaling error: %w", err)
	}
	if len(data) > MaxToolDefinitionBytes {
		return fmt.Errorf("%w: size %d bytes > %d bytes", ErrToolSizeExceeded, len(data), MaxToolDefinitionBytes)
	}

	return nil
}

// CloneAndAssignPublicName validates the tool, marshals it to JSON to check size,
// and returns an immutable deep-cloned *mcp.Tool with its Name set to publicName.
func CloneAndAssignPublicName(tool *mcp.Tool, publicName string) (*mcp.Tool, int, error) {
	if err := ValidateTool(tool); err != nil {
		return nil, 0, err
	}

	data, err := json.Marshal(tool)
	if err != nil {
		return nil, 0, fmt.Errorf("tool marshaling error: %w", err)
	}

	size := len(data)
	if size > MaxToolDefinitionBytes {
		return nil, size, fmt.Errorf("%w: size %d bytes > %d bytes", ErrToolSizeExceeded, size, MaxToolDefinitionBytes)
	}

	var cloned mcp.Tool
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, size, fmt.Errorf("tool unmarshaling error: %w", err)
	}
	cloned.Name = publicName

	return &cloned, size, nil
}
