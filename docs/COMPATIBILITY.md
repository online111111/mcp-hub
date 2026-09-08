# MCP Manager compatibility

## Current baseline

The production module and independent SDK probe use `github.com/modelcontextprotocol/go-sdk v1.7.0`. Go 1.25 remains the declared compatibility floor; CI exercises Go 1.25.8 and Go 1.27.1 on Linux, Windows, and macOS.

MCP Manager keeps a stateful/session-oriented compatibility model centered on the 2025-11-25 generation of Streamable HTTP behavior while using SDK v1.7.0. Discovery contains compatibility handling for older peers. The project does **not** claim complete native support for every MCP 2026-07-28 capability exposed by the SDK.

Supported downstream transports:

- managed stdio/IO;
- remote Streamable HTTP over HTTPS, with loopback HTTP for local development.

The product focuses on tool aggregation/routing. Resources, prompts, sampling, roots, elicitation, task extensions, arbitrary extension methods, and a general legacy HTTP+SSE proxy remain outside the supported proxy contract unless explicitly documented.

## Client modes

HTTP-capable clients connect to:

```text
https://mcp.example.com/mcp
```

stdio-only clients use:

```bash
MCP_MANAGER_TOKEN='...' mcp-manager stdio --connect https://mcp.example.com/mcp
```

During the rename compatibility window, `MCP_HUB_TOKEN` remains accepted by the CLI. New generated configurations use `MCP_MANAGER_TOKEN`.

Use `mcp-manager export` for supported client formats rather than copying stale examples.

## Downstream behavior

stdio:

- arguments stay distinct; ordinary commands are not shell-interpolated;
- children inherit only a safe compatibility environment baseline;
- additional secrets/values require explicit server `env` entries;
- explicitly choosing a shell opts into that shell's semantics.

Streamable HTTP:

- non-loopback plaintext HTTP is rejected;
- redirects are rejected;
- configured headers cannot replace MCP/session framing headers;
- request/response handling is bounded;
- normal diagnostics sanitize external transport/protocol errors.

## Automated evidence

Current automated evidence includes SDK v1.7.0 probes, root tests, current-Go race tests across Linux/Windows/macOS, POSIX process-group and Windows Job Object lifecycle tests, publication/pagination/cancellation/routing/hot-reload/drain regressions, stdio bridge authentication/synchronization, Admin CSRF/ETag/preflight/rollback/secret tests, Remote Admin CRUD tests, Chromium UI regressions, a real-MCP-Manager browser smoke test, and `govulncheck`.

This is evidence for tested contracts, not universal certification of every MCP client/server, reverse proxy, OS release, or network provider.

## Manual matrix

| Client/source | Exact version | Connection | Result |
|---|---|---|---|
| Cursor | not recorded | stdio | NOT_RUN |
| Cursor | not recorded | Streamable HTTP | NOT_RUN |
| Claude Desktop | not recorded | stdio | NOT_RUN |
| User stdio MCP | varies | stdio | NOT_RUN |
| User remote MCP | varies | Streamable HTTP | NOT_RUN |

Manual acceptance should record exact version/OS and verify initialize, `tools/list`, a normal call, tool-directory change behavior, and shutdown/reconnect behavior.

## Rename compatibility

The MCP Hub -> MCP Manager rename intentionally preserves the wire/config contract:

- `/mcp`, `/admin/`, and `/api/...` paths do not change;
- JSON `hub.*` fields do not change;
- old `MCP_HUB_*` CLI token variables remain accepted during migration;
- official new binary/Release naming is `mcp-manager`;
- internal Go module path remains `mcp-manager` during v0.4 to avoid a repository-wide import rewrite unrelated to runtime compatibility.

## Related

- [Configuration](CONFIG.md)
- [Security](SECURITY.md)
- [VPS deployment](VPS.md)
- [Remote Admin](REMOTE-ADMIN.md)
- [Implementation status](IMPLEMENTATION-STATUS.md)
- [SDK probe](../verification/sdkprobe/README.md)
