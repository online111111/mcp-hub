# MCP Hub compatibility

## Current baseline

The production module and the independent SDK probe both use `github.com/modelcontextprotocol/go-sdk v1.7.0` with Go 1.25 as the declared compatibility floor. CI currently exercises Go 1.25.8 and Go 1.27.1 on Linux, Windows, and macOS.

MCP Hub intentionally keeps a stateful/session-oriented compatibility model centered on the 2025-11-25 generation of Streamable HTTP behavior while using SDK v1.7.0. Discovery contains compatibility handling for older peers. This repository does **not** claim complete native implementation of every MCP 2026-07-28 capability exposed by the SDK.

Supported downstream transports are:

- managed stdio / IO transports;
- remote Streamable HTTP over HTTPS, with loopback HTTP allowed for local development.

The gateway currently focuses on tool aggregation and routing. Resources, prompts, sampling, roots, elicitation, task extensions, arbitrary extension methods, and a general legacy HTTP+SSE proxy are outside the supported proxy contract unless explicitly documented otherwise.

## Client connection modes

HTTP-capable clients should connect to the Hub `/mcp` endpoint:

```text
https://mcp.example.com/mcp
```

Clients that only support stdio can use the built-in bridge:

```bash
MCP_HUB_TOKEN='...' mcp-hub stdio --connect https://mcp.example.com/mcp
```

The bridge keeps one upstream Hub session for the client, mirrors the Hub tool directory, forwards calls without launching duplicate downstream processes, and exits when the Hub connection is no longer usable.

Use `mcp-hub export --help` and the current `export` command when a supported client configuration format is needed. Do not hand-author a client format merely because an older example happened to work.

## Downstream compatibility behavior

For stdio downstreams:

- arguments are passed as distinct process arguments; ordinary commands are not shell-interpolated;
- only a safe compatibility environment baseline is inherited;
- secrets and other extra values must be explicitly forwarded through the server `env` map;
- explicit use of `cmd.exe`, `cmd`, or another shell opts into that shell's semantics and risks.

For remote Streamable HTTP downstreams:

- non-loopback plaintext HTTP is rejected;
- redirects are rejected;
- configured headers cannot replace MCP/session framing headers;
- response and request sizes are bounded;
- external error details are sanitized before they reach normal diagnostics.

## Automated evidence

The repository currently has automated evidence for:

- SDK v1.7.0 feasibility and protocol/transport regressions in `verification/sdkprobe`;
- root-module unit and integration tests;
- race tests on current Go for Linux, Windows, and macOS;
- native POSIX process-group and Windows Job Object lifecycle behavior;
- dynamic tool publication, pagination, cancellation, routing, hot reload, and graceful drain;
- stdio bridge synchronization and authenticated Hub access;
- Admin API authentication, CSRF, ETag/CAS persistence, preflight, rollback, and secret placeholders;
- remote Admin CLI `list/get/add/edit/delete` behavior;
- Chromium management-console interaction/design regressions and a real-Hub Chromium smoke test on Linux;
- `govulncheck` on the production module.

Passing those checks is evidence for the tested contracts, not a universal certification of every MCP client, downstream server, reverse proxy, operating-system release, or provider network.

## Manual compatibility matrix

The following client rows remain `NOT_RUN` until an exact installed version is exercised end to end.

| Client / source | Exact version | Connection | list/call | list_changed | Result |
|---|---|---|---|---|---|
| Cursor | not recorded | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| Cursor | not recorded | Streamable HTTP | NOT_RUN | NOT_RUN | NOT_RUN |
| Claude Desktop | not recorded | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| User-provided stdio MCP | varies | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| User-provided remote MCP | varies | Streamable HTTP | NOT_RUN | NOT_RUN | NOT_RUN |

Recommended manual procedure:

1. Record the client/server version and OS.
2. Start a Hub with one trusted fixture or test downstream.
3. Connect through the intended transport.
4. Verify initialize, `tools/list`, at least one normal call, a tool-directory change, and clean shutdown/reconnect behavior.
5. Record any client-specific refresh/reconnect requirements in this file and `IMPLEMENTATION-STATUS.md`.

## Related documentation

- [Configuration](CONFIG.md)
- [Security](SECURITY.md)
- [VPS/public deployment](VPS.md)
- [Remote Admin CLI](REMOTE-ADMIN.md)
- [Implementation and verification status](IMPLEMENTATION-STATUS.md)
- [SDK probe](../verification/sdkprobe/README.md)
