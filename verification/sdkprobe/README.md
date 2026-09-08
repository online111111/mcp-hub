# Go SDK probe

This independent module verifies the MCP SDK assumptions that MCP Hub depends on. It is intentionally separate from the production module so SDK behavior can be exercised without depending on Hub internals.

## Current baseline

- SDK: `github.com/modelcontextprotocol/go-sdk v1.7.0`
- Go directive: `go 1.25.0`
- Current CI: Go 1.27.1 with `-race` on Linux, Windows, and macOS

The probe is a compatibility/evidence module, not a second implementation of the Hub.

MCP Hub uses SDK v1.7.0 but intentionally retains its documented stateful/session-oriented compatibility model rather than claiming complete native support for every MCP 2026-07-28 capability. See [`docs/COMPATIBILITY.md`](../../docs/COMPATIBILITY.md) for the product boundary.

## What the probe covers

The probe exercises SDK behavior used by the production gateway, including:

- Streamable HTTP client/server sessions;
- IO/stdio-style transports;
- raw tool argument forwarding;
- structured, text, image, and error result semantics that are expressible through the SDK;
- pagination;
- cancellation propagation;
- dynamic `AddTool` / `RemoveTools` publication and list-change notifications;
- multiple client sessions;
- publication synchronization assumptions;
- current and compatibility protocol negotiation paths used by the Hub.

The root module adds the production-specific contracts around those primitives: process ownership, configuration transactions, routing, preflight, security policy, bridge lifecycle, Admin API, and release engineering.

## Run it

From this directory:

```bash
go test -count=1 -timeout 120s ./...
go test -race -count=1 -timeout 120s ./...
```

The repository CI runs the race suite on current Go for Linux, Windows, and macOS.

## Evidence boundary

A passing SDK probe means the SDK behavior relied on by the tested production paths is available in the pinned SDK version. It does **not** prove:

- compatibility with every third-party MCP server;
- compatibility with a specific Cursor or Claude Desktop release;
- complete MCP 2026-07-28 feature coverage;
- production reverse-proxy/TLS behavior;
- Hub process ownership or secret handling;
- end-to-end Admin persistence.

Those contracts are tested or tracked in the root module and in [`docs/IMPLEMENTATION-STATUS.md`](../../docs/IMPLEMENTATION-STATUS.md).

## Upgrade rule

When the MCP Go SDK is changed:

1. update the production root module and this probe together;
2. review protocol/transport behavior that changed between SDK versions;
3. add or update regressions for behavior the Hub relies on;
4. run the probe normally and with `-race`;
5. run the full root-module matrix and `govulncheck`;
6. update `docs/COMPATIBILITY.md` and `docs/IMPLEMENTATION-STATUS.md` before release.

Do not treat an SDK version bump as an automatic expansion of the product's advertised protocol boundary.
