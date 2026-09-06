package catalog

const (
	// MaxToolsPerServer is the maximum number of published tools allowed per server.
	MaxToolsPerServer = 512

	// MaxTotalTools is the maximum total number of published tools across all servers.
	MaxTotalTools = 2048

	// MaxToolDefinitionBytes is the maximum JSON-encoded size of a single tool definition (256 KiB).
	MaxToolDefinitionBytes = 256 * 1024

	// MaxCatalogJSONBytes is the maximum total JSON-encoded size of the entire tool catalog (8 MiB).
	MaxCatalogJSONBytes = 8 * 1024 * 1024

	// MaxPaginationPages is the maximum number of pagination pages to fetch from downstream.
	MaxPaginationPages = 64

	// MaxPublicToolNameLength is the maximum length of a public tool name.
	MaxPublicToolNameLength = 64

	// MaxServerIDLength is the maximum length of a server ID.
	MaxServerIDLength = 32
)
