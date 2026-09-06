// Package buildinfo owns the product identity exposed by every transport.
// Keeping this in one package prevents CLI, HTTP, and bridge versions from
// drifting apart during releases.
package buildinfo

const (
	Name    = "mcp-hub"
	Version = "0.3.0"
)
