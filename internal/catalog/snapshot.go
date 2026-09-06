package catalog

import (
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UnpublishedTool records a tool that could not be published, along with the reason.
type UnpublishedTool struct {
	ServerID     string `json:"serverId"`
	OriginalName string `json:"originalName"`
	PublicName   string `json:"publicName,omitempty"`
	Reason       string `json:"reason"`
}

// Snapshot is an immutable view of the Hub's tool catalog and routing table.
type Snapshot struct {
	Revision          int64                 `json:"revision"`
	Tools             []*mcp.Tool           `json:"tools"`             // Stably sorted by tool.Name
	Routes            map[string]RouteEntry `json:"routes"`            // publicName -> RouteEntry
	ServerTools       map[string][]string   `json:"serverTools"`       // serverID -> sorted slice of publicNames
	PublishedCounts   map[string]int        `json:"publishedCounts"`   // serverID -> count
	UnpublishedCounts map[string]int        `json:"unpublishedCounts"` // serverID -> count
	Unpublished       []UnpublishedTool     `json:"unpublished"`       // list of unpublished tools with reasons
	TotalCatalogBytes int                   `json:"totalCatalogBytes"` // total size of tool definitions
}

// LookupRoute finds the routing entry for the given public tool name.
func (s *Snapshot) LookupRoute(publicName string) (RouteEntry, bool) {
	if s == nil || s.Routes == nil {
		return RouteEntry{}, false
	}
	r, ok := s.Routes[publicName]
	return r, ok
}

// GetTool retrieves a tool by its public name from the snapshot.
func (s *Snapshot) GetTool(publicName string) (*mcp.Tool, bool) {
	if s == nil {
		return nil, false
	}
	// Binary search in stably sorted Tools slice
	idx := sort.Search(len(s.Tools), func(i int) bool {
		return s.Tools[i].Name >= publicName
	})
	if idx < len(s.Tools) && s.Tools[idx].Name == publicName {
		return s.Tools[idx], true
	}
	return nil, false
}

// ToolsList returns a shallow copy of the sorted tools slice.
func (s *Snapshot) ToolsList() []*mcp.Tool {
	if s == nil || len(s.Tools) == 0 {
		return nil
	}
	out := make([]*mcp.Tool, len(s.Tools))
	copy(out, s.Tools)
	return out
}

// TotalPublished returns the total number of published tools across all servers.
func (s *Snapshot) TotalPublished() int {
	if s == nil {
		return 0
	}
	return len(s.Tools)
}

// PublishedCount returns the number of published tools for the given serverID.
func (s *Snapshot) PublishedCount(serverID string) int {
	if s == nil || s.PublishedCounts == nil {
		return 0
	}
	return s.PublishedCounts[serverID]
}

// UnpublishedCount returns the number of unpublished tools for the given serverID.
func (s *Snapshot) UnpublishedCount(serverID string) int {
	if s == nil || s.UnpublishedCounts == nil {
		return 0
	}
	return s.UnpublishedCounts[serverID]
}

// ServerToolNames returns the public tool names belonging to the specified server, sorted.
func (s *Snapshot) ServerToolNames(serverID string) []string {
	if s == nil || s.ServerTools == nil {
		return nil
	}
	names := s.ServerTools[serverID]
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	return out
}
