package catalog

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCollectDownstreamToolsSuccess(t *testing.T) {
	ctx := context.Background()

	// 3 pages of tools
	pages := map[string]struct {
		tools      []*mcp.Tool
		nextCursor string
	}{
		"": {
			tools: []*mcp.Tool{
				{Name: "tool1"},
				{Name: "tool2"},
			},
			nextCursor: "cursor_page2",
		},
		"cursor_page2": {
			tools: []*mcp.Tool{
				{Name: "tool3"},
			},
			nextCursor: "cursor_page3",
		},
		"cursor_page3": {
			tools: []*mcp.Tool{
				{Name: "tool4"},
			},
			nextCursor: "", // End of pages
		},
	}

	fetcher := ToolPageFetcherFunc(func(ctx context.Context, cursor string) ([]*mcp.Tool, string, error) {
		page, ok := pages[cursor]
		if !ok {
			return nil, "", fmt.Errorf("unknown cursor %q", cursor)
		}
		return page.tools, page.nextCursor, nil
	})

	allTools, err := CollectDownstreamTools(ctx, fetcher)
	if err != nil {
		t.Fatalf("CollectDownstreamTools failed: %v", err)
	}

	if len(allTools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(allTools))
	}
	expectedNames := []string{"tool1", "tool2", "tool3", "tool4"}
	for i, name := range expectedNames {
		if allTools[i].Name != name {
			t.Errorf("tool %d: expected %s, got %s", i, name, allTools[i].Name)
		}
	}
}

func TestCollectDownstreamToolsCursorCycle(t *testing.T) {
	ctx := context.Background()

	// Infinite loop: A -> B -> A
	fetcher := ToolPageFetcherFunc(func(ctx context.Context, cursor string) ([]*mcp.Tool, string, error) {
		if cursor == "" {
			return []*mcp.Tool{{Name: "toolA"}}, "cursorB", nil
		}
		if cursor == "cursorB" {
			return []*mcp.Tool{{Name: "toolB"}}, "cursorB", nil // Repeated cursor!
		}
		return nil, "", nil
	})

	_, err := CollectDownstreamTools(ctx, fetcher)
	if err == nil || !errors.Is(err, ErrCursorCycleDetected) {
		t.Fatalf("expected ErrCursorCycleDetected, got %v", err)
	}
}

func TestCollectDownstreamToolsDuplicateName(t *testing.T) {
	ctx := context.Background()

	// Tool with name "same_tool" returned across pages
	fetcher := ToolPageFetcherFunc(func(ctx context.Context, cursor string) ([]*mcp.Tool, string, error) {
		if cursor == "" {
			return []*mcp.Tool{{Name: "same_tool"}}, "p2", nil
		}
		return []*mcp.Tool{{Name: "same_tool"}}, "", nil
	})

	_, err := CollectDownstreamTools(ctx, fetcher)
	if err == nil || !errors.Is(err, ErrDuplicateDownstreamTool) {
		t.Fatalf("expected ErrDuplicateDownstreamTool, got %v", err)
	}
}

func TestCollectDownstreamToolsPageLimitExceeded(t *testing.T) {
	ctx := context.Background()

	// Generates 70 distinct pages
	fetcher := ToolPageFetcherFunc(func(ctx context.Context, cursor string) ([]*mcp.Tool, string, error) {
		var pageNum int
		if cursor != "" {
			fmt.Sscanf(cursor, "page_%d", &pageNum)
		}
		pageNum++
		tool := &mcp.Tool{Name: fmt.Sprintf("tool_%d", pageNum)}
		return []*mcp.Tool{tool}, fmt.Sprintf("page_%d", pageNum), nil
	})

	_, err := CollectDownstreamTools(ctx, fetcher)
	if err == nil || !errors.Is(err, ErrPaginationLimitExceeded) {
		t.Fatalf("expected ErrPaginationLimitExceeded, got %v", err)
	}
}
