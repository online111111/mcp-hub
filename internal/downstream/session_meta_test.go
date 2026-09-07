package downstream

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCloneForwardableMetaDropsHopIdentity(t *testing.T) {
	const customKey = "example.com/request-id"
	in := mcp.Meta{
		mcp.MetaKeyProtocolVersion:    "2026-07-28",
		mcp.MetaKeyClientInfo:         map[string]any{"name": "local-client"},
		mcp.MetaKeyClientCapabilities: map[string]any{"roots": map[string]any{}},
		customKey:                     "keep-me",
	}

	got := cloneForwardableMeta(in)
	if got[customKey] != "keep-me" {
		t.Fatalf("custom metadata was not preserved: %#v", got)
	}
	for _, key := range []string{mcp.MetaKeyProtocolVersion, mcp.MetaKeyClientInfo, mcp.MetaKeyClientCapabilities} {
		if _, ok := got[key]; ok {
			t.Fatalf("hop-local metadata %q leaked across sessions: %#v", key, got)
		}
	}
	if in[mcp.MetaKeyProtocolVersion] != "2026-07-28" {
		t.Fatal("input metadata was mutated")
	}
}
