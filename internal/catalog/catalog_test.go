package catalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func makeValidTool(name string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: "Tool " + name,
		InputSchema: map[string]any{"type": "object"},
	}
}

func TestCatalogBasicPublishAndSnapshot(t *testing.T) {
	cat := NewCatalog()
	if cat.Revision() != 0 {
		t.Fatalf("expected initial revision 0, got %d", cat.Revision())
	}

	tools := []*mcp.Tool{
		makeValidTool("read_file"),
		makeValidTool("write_file"),
	}

	added, removed, snap, err := cat.UpdateServerTools("filesystem", tools, nil)
	if err != nil {
		t.Fatalf("UpdateServerTools failed: %v", err)
	}

	if len(added) != 2 || len(removed) != 0 {
		t.Fatalf("expected 2 added, 0 removed; got %d added, %d removed", len(added), len(removed))
	}
	if snap.Revision != 1 {
		t.Errorf("expected revision 1, got %d", snap.Revision)
	}
	if snap.TotalPublished() != 2 {
		t.Errorf("expected 2 published tools, got %d", snap.TotalPublished())
	}

	// Verify routes
	routeRead, ok := snap.LookupRoute("filesystem__read_file")
	if !ok || routeRead.ServerID != "filesystem" || routeRead.OriginalName != "read_file" {
		t.Errorf("unexpected route for read_file: %+v", routeRead)
	}

	routeWrite, ok := snap.LookupRoute("filesystem__write_file")
	if !ok || routeWrite.ServerID != "filesystem" || routeWrite.OriginalName != "write_file" {
		t.Errorf("unexpected route for write_file: %+v", routeWrite)
	}

	// Stable sorting check: filesystem__read_file before filesystem__write_file
	snapTools := snap.ToolsList()
	if snapTools[0].Name != "filesystem__read_file" || snapTools[1].Name != "filesystem__write_file" {
		t.Errorf("expected sorted tools, got %s, %s", snapTools[0].Name, snapTools[1].Name)
	}
}

func TestCatalogStableSorting(t *testing.T) {
	cat := NewCatalog()

	// Add tools in unsorted order across multiple servers
	toolsSrvB := []*mcp.Tool{
		makeValidTool("z_tool"),
		makeValidTool("a_tool"),
	}
	toolsSrvA := []*mcp.Tool{
		makeValidTool("m_tool"),
	}

	_, _, _, err := cat.UpdateServerTools("srvb", toolsSrvB, nil)
	if err != nil {
		t.Fatalf("UpdateServerTools srvb failed: %v", err)
	}
	_, _, snap, err := cat.UpdateServerTools("srva", toolsSrvA, nil)
	if err != nil {
		t.Fatalf("UpdateServerTools srva failed: %v", err)
	}

	expectedOrder := []string{
		"srva__m_tool",
		"srvb__a_tool",
		"srvb__z_tool",
	}

	snapTools := snap.ToolsList()
	if len(snapTools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(snapTools))
	}
	for i, expectedName := range expectedOrder {
		if snapTools[i].Name != expectedName {
			t.Errorf("index %d: expected %s, got %s", i, expectedName, snapTools[i].Name)
		}
	}
}

func TestCatalogFilteringAndPartialIsolation(t *testing.T) {
	cat := NewCatalog()

	tools := []*mcp.Tool{
		makeValidTool("good_tool_1"),
		{Name: "bad_schema_tool", InputSchema: "invalid_string_schema"},
		makeValidTool("disabled_tool"),
		makeValidTool("good_tool_2"),
	}

	disabled := []string{"disabled_tool"}

	added, removed, snap, err := cat.UpdateServerTools("myserver", tools, disabled)
	if err != nil {
		t.Fatalf("UpdateServerTools failed: %v", err)
	}

	// Only good_tool_1 and good_tool_2 should be added!
	// bad_schema_tool and disabled_tool must be isolated without failing the server!
	if len(added) != 2 {
		t.Fatalf("expected 2 published tools, got %d", len(added))
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}

	if snap.PublishedCount("myserver") != 2 {
		t.Errorf("expected published count 2, got %d", snap.PublishedCount("myserver"))
	}
	if snap.UnpublishedCount("myserver") != 2 {
		t.Errorf("expected unpublished count 2, got %d", snap.UnpublishedCount("myserver"))
	}

	// Verify unpublished tool reasons
	var foundDisabled, foundBadSchema bool
	for _, unpub := range snap.Unpublished {
		if unpub.OriginalName == "disabled_tool" && unpub.Reason == ReasonDisabled {
			foundDisabled = true
		}
		if unpub.OriginalName == "bad_schema_tool" && strings.Contains(unpub.Reason, "input schema invalid") {
			foundBadSchema = true
		}
	}
	if !foundDisabled {
		t.Errorf("expected disabled_tool to be recorded in unpublished list with ReasonDisabled")
	}
	if !foundBadSchema {
		t.Errorf("expected bad_schema_tool to be recorded in unpublished list with schema error")
	}
}

func TestCatalogToolUpdateAndRemoval(t *testing.T) {
	cat := NewCatalog()

	// Initial publish
	initial := []*mcp.Tool{
		makeValidTool("tool1"),
		makeValidTool("tool2"),
	}
	added, removed, snap, err := cat.UpdateServerTools("srv", initial, nil)
	if err != nil {
		t.Fatalf("initial publish failed: %v", err)
	}
	if len(added) != 2 || len(removed) != 0 || snap.Revision != 1 {
		t.Fatalf("unexpected initial publish state: added=%d, removed=%d, rev=%d", len(added), len(removed), snap.Revision)
	}

	// Re-publish identical tools -> no-op, revision does NOT change!
	added2, removed2, snap2, err := cat.UpdateServerTools("srv", initial, nil)
	if err != nil {
		t.Fatalf("republish failed: %v", err)
	}
	if len(added2) != 0 || len(removed2) != 0 || snap2.Revision != 1 {
		t.Fatalf("expected no-op republish: added=%d, removed=%d, rev=%d", len(added2), len(removed2), snap2.Revision)
	}

	// Update: remove tool1, modify tool2 description, add tool3
	modifiedTool2 := &mcp.Tool{
		Name:        "tool2",
		Description: "Updated description for tool2",
		InputSchema: map[string]any{"type": "object"},
	}
	updated := []*mcp.Tool{
		modifiedTool2,
		makeValidTool("tool3"),
	}

	added3, removed3, snap3, err := cat.UpdateServerTools("srv", updated, nil)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if snap3.Revision != 2 {
		t.Errorf("expected revision 2, got %d", snap3.Revision)
	}

	// srv__tool1 removed
	if len(removed3) != 1 || removed3[0] != "srv__tool1" {
		t.Errorf("expected removed [srv__tool1], got %+v", removed3)
	}

	// srv__tool2 updated, srv__tool3 added
	if len(added3) != 2 {
		t.Errorf("expected 2 added (tool2 modified, tool3 new), got %d", len(added3))
	}

	// Now remove server entirely
	removedAll, snap4 := cat.RemoveServer("srv")
	if len(removedAll) != 2 {
		t.Errorf("expected 2 removed tools on RemoveServer, got %d", len(removedAll))
	}
	if snap4.Revision != 3 {
		t.Errorf("expected revision 3 after RemoveServer, got %d", snap4.Revision)
	}
	if snap4.TotalPublished() != 0 {
		t.Errorf("expected 0 published tools after RemoveServer, got %d", snap4.TotalPublished())
	}
}

func TestCatalogCapacityLimits(t *testing.T) {
	cat := NewCatalog()

	// 1. Exceed per-server limit of 512 tools
	tools := make([]*mcp.Tool, 520)
	for i := 0; i < 520; i++ {
		tools[i] = makeValidTool(fmt.Sprintf("tool_%04d", i))
	}

	added, _, snap, err := cat.UpdateServerTools("bigserver", tools, nil)
	if err != nil {
		t.Fatalf("UpdateServerTools failed: %v", err)
	}

	if len(added) != MaxToolsPerServer {
		t.Errorf("expected %d published tools, got %d", MaxToolsPerServer, len(added))
	}
	if snap.PublishedCount("bigserver") != MaxToolsPerServer {
		t.Errorf("expected published count %d, got %d", MaxToolsPerServer, snap.PublishedCount("bigserver"))
	}
	if snap.UnpublishedCount("bigserver") != 8 {
		t.Errorf("expected unpublished count 8, got %d", snap.UnpublishedCount("bigserver"))
	}
}

func TestCatalogSnapshotImmutability(t *testing.T) {
	cat := NewCatalog()
	tools := []*mcp.Tool{makeValidTool("t1")}
	_, _, snap, err := cat.UpdateServerTools("srv", tools, nil)
	if err != nil {
		t.Fatalf("UpdateServerTools failed: %v", err)
	}

	// ToolsList() returns a copy of the slice
	list := snap.ToolsList()
	list[0] = makeValidTool("mutated")

	// Snapshot's tool should still be srv__t1
	snapTool, ok := snap.GetTool("srv__t1")
	if !ok || snapTool.Name != "srv__t1" {
		t.Errorf("snapshot was corrupted by caller slice mutation")
	}
}
