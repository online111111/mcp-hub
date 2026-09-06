package catalog

import "github.com/modelcontextprotocol/go-sdk/mcp"

const (
	// ReasonDisabled indicates a tool was disabled by server configuration.
	ReasonDisabled = "disabled by configuration"
)

// FilterDisabledTools separates a list of tools into enabled and disabled slices
// based on whether their original name is present in the disabled list.
func FilterDisabledTools(tools []*mcp.Tool, disabled []string) (enabled []*mcp.Tool, disabledTools []*mcp.Tool) {
	if len(disabled) == 0 {
		return tools, nil
	}

	disabledSet := make(map[string]bool, len(disabled))
	for _, d := range disabled {
		disabledSet[d] = true
	}

	enabled = make([]*mcp.Tool, 0, len(tools))
	disabledTools = make([]*mcp.Tool, 0)

	for _, t := range tools {
		if t == nil {
			continue
		}
		if disabledSet[t.Name] {
			disabledTools = append(disabledTools, t)
		} else {
			enabled = append(enabled, t)
		}
	}

	return enabled, disabledTools
}
