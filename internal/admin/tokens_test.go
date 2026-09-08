package admin

import (
	"strings"
	"testing"

	"github.com/online111111/mcp-manager/internal/config"
)

func TestResolvedTokenDTOsPreserveLegacyFirstAndExpandEnvironment(t *testing.T) {
	primary := strings.Repeat("p", config.MinAuthTokenLength)
	secondary := strings.Repeat("s", config.MinAuthTokenLength)
	t.Setenv("PRIMARY_MCP_TOKEN", primary)

	tokens, err := resolvedTokenDTOs(config.HubAuthConfig{
		BearerToken:  "${PRIMARY_MCP_TOKEN}",
		BearerTokens: []string{secondary},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected two tokens, got %#v", tokens)
	}
	if tokens[0].Index != 0 || !tokens[0].Legacy || tokens[0].Token != primary {
		t.Fatalf("unexpected primary token DTO: %#v", tokens[0])
	}
	if tokens[1].Index != 1 || tokens[1].Legacy || tokens[1].Token != secondary {
		t.Fatalf("unexpected additional token DTO: %#v", tokens[1])
	}
}
