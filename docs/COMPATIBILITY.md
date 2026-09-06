# MCP Hub compatibility

## Protocol and transports

The production module uses `github.com/modelcontextprotocol/go-sdk v1.4.1` and its negotiated MCP protocol support, with the project baseline centered on 2025-06-18. Supported downstream transports in P0 are:

- stdio/IO transport managed by the Hub process owner;
- Streamable HTTP, including the SDK's stateful POST and optional standalone SSE behavior.

Legacy HTTP+SSE is P1 and is not guessed or silently downgraded. Resources, prompts, sampling, roots, elicitation, task extensions, and unknown extension methods are outside the P0 proxy contract.

## Client connection modes

HTTP-capable clients should connect to the running Hub's `/mcp` endpoint, for example `http://127.0.0.1:8080/mcp`. Clients that only support stdio can use the generated command:

```text
<mcp-hub absolute path> stdio --connect http://127.0.0.1:8080/mcp
```

The bridge maintains one Hub session for that client, mirrors the Hub tool directory with SDK `AddTool`/`RemoveTools`, forwards the public tool name, and does not launch downstream processes. Multiple bridges can share one Hub and its already-managed downstream sessions.

## Compatibility matrix

The following entries are intentionally `NOT_RUN` until the exact installed client version is manually exercised. Automated SDK and end-to-end fixture tests are not a substitute for this matrix.

| Client / source | OS and exact version | Connection | list/call | list_changed | Result |
|---|---|---|---|---|---|
| Cursor | not recorded | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| Cursor | not recorded | Streamable HTTP | NOT_RUN | NOT_RUN | NOT_RUN |
| Claude Desktop | not recorded | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| User file MCP | not recorded | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| User remote MCP | not recorded | Streamable HTTP | NOT_RUN | NOT_RUN | NOT_RUN |

Manual procedure:

1. Start a Hub with one known fixture or trusted local MCP server.
2. Run `mcp-hub export --client ... --transport stdio` and install the exact emitted entry, or configure the HTTP URL directly.
3. Verify initialize, `tools/list`, one normal call, a tool-directory change, and clean client shutdown.
4. Record OS, client version, connection mode, observed behavior, and any client-specific refresh/reconnect requirement here and in `docs/IMPLEMENTATION-STATUS.md`.

## Tested implementation evidence

The repository's automated tests cover SDK feasibility, HTTP and IO sessions, raw argument forwarding, pagination, dynamic tool publication, cancellation, generation lifecycle, Windows Job ownership tests on Windows, POSIX process-group code paths where run, the three-hop stdio bridge, two simultaneous bridges, list-change propagation, Hub failure exit, and stdout protocol purity. They do not prove compatibility with an untested GUI client or with arbitrary third-party MCP servers.
