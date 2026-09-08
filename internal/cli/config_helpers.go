package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/online111111/mcp-manager/internal/config"
)

func ValidateConfig(configPath string) (*config.Config, *config.ResolvedConfig, error) {
	return config.LoadFile(configPath)
}

func RunImport(fromPath, targetConfigPath string, remoteType string, dryRun, yes bool) (*config.ImportPreview, error) {
	data, err := os.ReadFile(fromPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read import source file %q: %w", fromPath, err)
	}
	opts := config.ImportOptions{RemoteType: remoteType, DryRun: dryRun, Yes: yes}
	return config.ImportServers(data, targetConfigPath, opts)
}

// FormatPreview formats an ImportPreview for terminal display without exposing
// imported secret values. Argument values are never printed, and HTTP URLs are
// reduced to their origin by the config package before reaching this function.
func FormatPreview(preview *config.ImportPreview) string {
	if preview == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Target Config: %s\n", preview.TargetConfigPath))
	sb.WriteString(fmt.Sprintf("Existing Servers: %d | New Servers to Add: %d\n\n", preview.ExistingCount, preview.NewCount))

	for i, s := range preview.ServersToAdd {
		sb.WriteString(fmt.Sprintf("[%d] Server ID: %s (Type: %s, Enabled: %v)\n", i+1, s.ID, s.Type, s.Enabled))
		if s.Type == config.ServerTypeStdio {
			sb.WriteString(fmt.Sprintf("    Command: %s\n", s.Command))
			if s.ArgCount > 0 {
				sb.WriteString(fmt.Sprintf("    Args: <redacted: %d argument(s)>\n", s.ArgCount))
			}
			if s.Cwd != "" {
				sb.WriteString(fmt.Sprintf("    Cwd: %s\n", s.Cwd))
			}
			if len(s.EnvKeys) > 0 {
				sb.WriteString(fmt.Sprintf("    Environment Keys: %s\n", strings.Join(s.EnvKeys, ", ")))
			}
		} else if s.Type == config.ServerTypeStreamableHTTP {
			sb.WriteString(fmt.Sprintf("    URL Origin: %s\n", s.URL))
			if len(s.HeaderKeys) > 0 {
				sb.WriteString(fmt.Sprintf("    Configured Header Names: %s\n", strings.Join(s.HeaderKeys, ", ")))
			}
		}
	}
	return sb.String()
}
