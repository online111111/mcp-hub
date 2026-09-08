package cli

import (
	"os"
	"strings"
)

const (
	managerAdminTokenEnv = "MCP_MANAGER_ADMIN_TOKEN"
	legacyAdminTokenEnv  = "MCP_HUB_ADMIN_TOKEN"
)

func adminTokenFromEnv() string {
	if token := strings.TrimSpace(os.Getenv(managerAdminTokenEnv)); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv(legacyAdminTokenEnv))
}
