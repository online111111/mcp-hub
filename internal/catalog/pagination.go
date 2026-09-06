package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrPaginationLimitExceeded indicates pagination exceeded 64 pages.
	ErrPaginationLimitExceeded = errors.New("downstream pagination exceeded limit of 64 pages")

	// ErrCursorCycleDetected indicates a cursor cycle was detected.
	ErrCursorCycleDetected = errors.New("downstream cursor loop detected")

	// ErrDuplicateDownstreamTool indicates duplicate tool name from downstream.
	ErrDuplicateDownstreamTool = errors.New("duplicate tool name from downstream server")
)

// ToolPageFetcher fetches a single page of tools from downstream using a pagination cursor.
type ToolPageFetcher interface {
	FetchToolPage(ctx context.Context, cursor string) (tools []*mcp.Tool, nextCursor string, err error)
}

// ToolPageFetcherFunc allows a function to be used as a ToolPageFetcher.
type ToolPageFetcherFunc func(ctx context.Context, cursor string) (tools []*mcp.Tool, nextCursor string, err error)

// FetchToolPage implements ToolPageFetcher.
func (f ToolPageFetcherFunc) FetchToolPage(ctx context.Context, cursor string) ([]*mcp.Tool, string, error) {
	return f(ctx, cursor)
}

// CollectDownstreamTools paginates through a downstream tool list up to MaxPaginationPages (64).
// It validates that cursors do not loop and downstream does not return duplicate tool names.
func CollectDownstreamTools(ctx context.Context, fetcher ToolPageFetcher) ([]*mcp.Tool, error) {
	if fetcher == nil {
		return nil, errors.New("nil fetcher")
	}

	var allTools []*mcp.Tool
	seenCursors := make(map[string]bool)
	seenToolNames := make(map[string]bool)
	cursor := ""

	for page := 1; page <= MaxPaginationPages; page++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		tools, nextCursor, err := fetcher.FetchToolPage(ctx, cursor)
		if err != nil {
			return nil, fmt.Errorf("page %d fetch error: %w", page, err)
		}

		for _, t := range tools {
			if t == nil {
				continue
			}
			if t.Name == "" {
				return nil, ErrEmptyToolName
			}
			if seenToolNames[t.Name] {
				return nil, fmt.Errorf("%w: %q", ErrDuplicateDownstreamTool, t.Name)
			}
			seenToolNames[t.Name] = true
			allTools = append(allTools, t)
		}

		if nextCursor == "" {
			return allTools, nil
		}

		// Detect cursor loop
		if seenCursors[nextCursor] {
			return nil, fmt.Errorf("%w: cursor %q repeated", ErrCursorCycleDetected, nextCursor)
		}
		seenCursors[nextCursor] = true
		cursor = nextCursor
	}

	return nil, ErrPaginationLimitExceeded
}
