package catalog

import "github.com/modelcontextprotocol/go-sdk/mcp"

// RouteEntry maps a public tool name back to its upstream server and original tool name.
type RouteEntry struct {
	PublicName   string `json:"publicName"`
	ServerID     string `json:"serverId"`
	OriginalName string `json:"originalName"`
}

// RouteLookup provides read-only route resolution.
type RouteLookup interface {
	LookupRoute(publicName string) (RouteEntry, bool)
}

// SnapshotProvider provides access to the latest catalog snapshot and revision.
type SnapshotProvider interface {
	Snapshot() *Snapshot
	Revision() int64
}

// Publisher defines the publisher contract for managing tool publication and SDK integration.
type Publisher interface {
	PublishServer(serverID string, rawTools []*mcp.Tool, disabled []string) (*Snapshot, error)
	RemoveServer(serverID string) (*Snapshot, error)
	LookupRoute(publicName string) (RouteEntry, bool)
	Snapshot() *Snapshot
	Revision() int64
	RLock()
	RUnlock()
}
