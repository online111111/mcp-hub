package cli

import "os"

const (
	managerAdminTokenEnv   = "MCP_MANAGER_ADMIN_TOKEN"
	legacyAdminTokenEnv    = "MCP_HUB_ADMIN_TOKEN"
)

func init() {
	// Remote Admin historically reads MCP_HUB_ADMIN_TOKEN directly. During the
	// product rename, prefer the new MCP_MANAGER_ADMIN_TOKEN without breaking
	// existing deployments. Do not overwrite an explicitly configured legacy
	// value; callers that pass --token still take precedence over both.
	if os.Getenv(legacyAdminTokenEnv) == "" {
		if token := os.Getenv(managerAdminTokenEnv); token != "" {
			_ = os.Setenv(legacyAdminTokenEnv, token)
		}
	}
}
