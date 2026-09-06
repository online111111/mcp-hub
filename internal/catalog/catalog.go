package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registeredTool holds internal metadata for a published tool.
type registeredTool struct {
	tool         *mcp.Tool
	serverID     string
	originalName string
	publicName   string
	jsonBytes    []byte
	size         int
}

// Catalog manages the multi-server tool catalog, name resolution, limits, and immutable snapshots.
type Catalog struct {
	mu          sync.Mutex
	revision    int64
	servers     map[string]map[string]*registeredTool // serverID -> publicName -> registeredTool
	unpublished map[string][]UnpublishedTool          // serverID -> list of unpublished tools
	snapshot    atomic.Pointer[Snapshot]
}

// NewCatalog creates an empty Catalog initialized with revision 0.
func NewCatalog() *Catalog {
	c := &Catalog{
		servers:     make(map[string]map[string]*registeredTool),
		unpublished: make(map[string][]UnpublishedTool),
	}
	emptySnap := &Snapshot{
		Revision:          0,
		Tools:             []*mcp.Tool{},
		Routes:            make(map[string]RouteEntry),
		ServerTools:       make(map[string][]string),
		PublishedCounts:   make(map[string]int),
		UnpublishedCounts: make(map[string]int),
		Unpublished:       []UnpublishedTool{},
	}
	c.snapshot.Store(emptySnap)
	return c
}

// Snapshot returns the current immutable snapshot of the catalog.
func (c *Catalog) Snapshot() *Snapshot {
	return c.snapshot.Load()
}

// Revision returns the current catalog revision.
func (c *Catalog) Revision() int64 {
	snap := c.snapshot.Load()
	if snap == nil {
		return 0
	}
	return snap.Revision
}

// LookupRoute resolves a public tool name to its RouteEntry.
func (c *Catalog) LookupRoute(publicName string) (RouteEntry, bool) {
	snap := c.snapshot.Load()
	if snap == nil {
		return RouteEntry{}, false
	}
	return snap.LookupRoute(publicName)
}

// UpdateServerTools processes the discovered tools for a server against disabled original names.
// It applies validation, priority naming, collision detection, and capacity bounds.
// It returns:
// - toAdd: newly published or updated *mcp.Tool objects to register with SDK
// - toRemove: public tool names that are no longer published and must be removed from SDK
// - snap: the updated catalog snapshot
// - err: any fatal server-level error (e.g. invalid server ID)
func (c *Catalog) UpdateServerTools(serverID string, rawTools []*mcp.Tool, disabled []string) (toAdd []*mcp.Tool, toRemove []string, snap *Snapshot, err error) {
	if err := ValidateServerID(serverID); err != nil {
		return nil, nil, c.Snapshot(), err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Separate disabled tools
	enabledTools, disabledList := FilterDisabledTools(rawTools, disabled)

	var unpublishedList []UnpublishedTool
	for _, d := range disabledList {
		unpublishedList = append(unpublishedList, UnpublishedTool{
			ServerID:     serverID,
			OriginalName: d.Name,
			Reason:       ReasonDisabled,
		})
	}

	// 2. Collect claimed names from ALL OTHER servers
	claimedNames := make(map[string]bool)
	otherServersByteTotal := 0
	otherServersToolCount := 0
	for sID, sMap := range c.servers {
		if sID == serverID {
			continue
		}
		for pName, rt := range sMap {
			claimedNames[pName] = true
			otherServersByteTotal += rt.size
			otherServersToolCount++
		}
	}

	// 3. Resolve candidate names with two-pass priority
	origNames := make([]string, len(enabledTools))
	for i, t := range enabledTools {
		origNames[i] = t.Name
	}
	nameCandidates := ResolveNamesWithPriority(serverID, origNames, claimedNames)

	// 4. Validate schemas, size bounds, and capacity limits
	newServerTools := make(map[string]*registeredTool)
	currentServerByteTotal := 0

	for i, cand := range nameCandidates {
		raw := enabledTools[i]

		// Check naming candidate error (e.g. collision or empty name)
		if cand.Err != nil {
			unpublishedList = append(unpublishedList, UnpublishedTool{
				ServerID:     serverID,
				OriginalName: cand.OriginalName,
				PublicName:   cand.PublicName,
				Reason:       cand.Err.Error(),
			})
			continue
		}

		// Pre-validate schema and clone tool definition
		cloned, size, valErr := CloneAndAssignPublicName(raw, cand.PublicName)
		if valErr != nil {
			unpublishedList = append(unpublishedList, UnpublishedTool{
				ServerID:     serverID,
				OriginalName: raw.Name,
				PublicName:   cand.PublicName,
				Reason:       valErr.Error(),
			})
			continue
		}

		// Check per-server tool limit
		if len(newServerTools) >= MaxToolsPerServer {
			unpublishedList = append(unpublishedList, UnpublishedTool{
				ServerID:     serverID,
				OriginalName: raw.Name,
				PublicName:   cand.PublicName,
				Reason:       fmt.Sprintf("per-server tool limit exceeded (max %d)", MaxToolsPerServer),
			})
			continue
		}

		// Check total tools limit
		if otherServersToolCount+len(newServerTools) >= MaxTotalTools {
			unpublishedList = append(unpublishedList, UnpublishedTool{
				ServerID:     serverID,
				OriginalName: raw.Name,
				PublicName:   cand.PublicName,
				Reason:       fmt.Sprintf("catalog total tool limit exceeded (max %d)", MaxTotalTools),
			})
			continue
		}

		// Check total catalog JSON bytes limit
		if otherServersByteTotal+currentServerByteTotal+size > MaxCatalogJSONBytes {
			unpublishedList = append(unpublishedList, UnpublishedTool{
				ServerID:     serverID,
				OriginalName: raw.Name,
				PublicName:   cand.PublicName,
				Reason:       fmt.Sprintf("catalog total size limit exceeded (max %d bytes)", MaxCatalogJSONBytes),
			})
			continue
		}

		// Tool is accepted
		rawBytes, _ := json.Marshal(cloned)
		newServerTools[cand.PublicName] = &registeredTool{
			tool:         cloned,
			serverID:     serverID,
			originalName: raw.Name,
			publicName:   cand.PublicName,
			jsonBytes:    rawBytes,
			size:         size,
		}
		currentServerByteTotal += size
	}

	// 5. Compare against existing tools for this server to determine added/removed/updated
	oldServerTools := c.servers[serverID]
	if oldServerTools == nil {
		oldServerTools = make(map[string]*registeredTool)
	}

	for oldName := range oldServerTools {
		if _, exists := newServerTools[oldName]; !exists {
			toRemove = append(toRemove, oldName)
		}
	}

	for newName, newRT := range newServerTools {
		oldRT, exists := oldServerTools[newName]
		if !exists || !bytes.Equal(oldRT.jsonBytes, newRT.jsonBytes) {
			toAdd = append(toAdd, newRT.tool)
		}
	}

	// Check if unpublished list changed
	oldUnpublished := c.unpublished[serverID]
	unpublishedChanged := len(oldUnpublished) != len(unpublishedList)
	if !unpublishedChanged {
		for idx := range oldUnpublished {
			if oldUnpublished[idx] != unpublishedList[idx] {
				unpublishedChanged = true
				break
			}
		}
	}

	// Check if any change occurred
	hasChanges := len(toAdd) > 0 || len(toRemove) > 0 || unpublishedChanged || (len(newServerTools) == 0 && len(oldServerTools) > 0)

	if hasChanges {
		c.servers[serverID] = newServerTools
		c.unpublished[serverID] = unpublishedList
		c.revision++
		c.rebuildSnapshot()
	}

	return toAdd, toRemove, c.Snapshot(), nil
}

// RemoveServer removes all published and unpublished tools associated with the serverID.
func (c *Catalog) RemoveServer(serverID string) (toRemove []string, snap *Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldTools := c.servers[serverID]
	_, hasUnpublished := c.unpublished[serverID]

	if len(oldTools) == 0 && !hasUnpublished {
		return nil, c.Snapshot()
	}

	for publicName := range oldTools {
		toRemove = append(toRemove, publicName)
	}

	delete(c.servers, serverID)
	delete(c.unpublished, serverID)
	c.revision++
	c.rebuildSnapshot()

	return toRemove, c.Snapshot()
}

// rebuildSnapshot builds and stores a new immutable Snapshot. Caller must hold c.mu.
func (c *Catalog) rebuildSnapshot() {
	var allTools []*mcp.Tool
	routes := make(map[string]RouteEntry)
	serverTools := make(map[string][]string)
	publishedCounts := make(map[string]int)
	unpublishedCounts := make(map[string]int)
	var allUnpublished []UnpublishedTool
	totalBytes := 0

	for sID, sMap := range c.servers {
		names := make([]string, 0, len(sMap))
		for pName, rt := range sMap {
			allTools = append(allTools, rt.tool)
			routes[pName] = RouteEntry{
				PublicName:   pName,
				ServerID:     sID,
				OriginalName: rt.originalName,
			}
			names = append(names, pName)
			totalBytes += rt.size
		}
		sort.Strings(names)
		serverTools[sID] = names
		publishedCounts[sID] = len(names)
	}

	for sID, unpub := range c.unpublished {
		unpublishedCounts[sID] = len(unpub)
		allUnpublished = append(allUnpublished, unpub...)
	}

	// Stable sort of all tools by Name
	sort.Slice(allTools, func(i, j int) bool {
		return allTools[i].Name < allTools[j].Name
	})

	// Sort unpublished tools stably by ServerID then OriginalName
	sort.Slice(allUnpublished, func(i, j int) bool {
		if allUnpublished[i].ServerID != allUnpublished[j].ServerID {
			return allUnpublished[i].ServerID < allUnpublished[j].ServerID
		}
		return allUnpublished[i].OriginalName < allUnpublished[j].OriginalName
	})

	snap := &Snapshot{
		Revision:          c.revision,
		Tools:             allTools,
		Routes:            routes,
		ServerTools:       serverTools,
		PublishedCounts:   publishedCounts,
		UnpublishedCounts: unpublishedCounts,
		Unpublished:       allUnpublished,
		TotalCatalogBytes: totalBytes,
	}

	c.snapshot.Store(snap)
}
