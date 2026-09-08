# MCP Hub configuration

MCP Hub reads one strict JSON configuration file. The file is the persistent source of truth; the Hub does not create a database or `state.json`.

## Minimal shape

```json
{
  "version": 1,
  "hub": { "listen": "127.0.0.1:8080" },
  "defaults": {
    "startupTimeout": "20s",
    "callTimeout": "60s",
    "maxConcurrency": 8
  },
  "mcpServers": {
    "my-server": {
      "type": "stdio",
      "command": "node",
      "args": ["server.js"]
    }
  }
}
```

`enabled` defaults to `true`. `type` is required in saved configuration and is either `stdio` or `streamable_http`.

## stdio servers

A stdio server requires `command`. It may also specify `args`, `cwd`, and `env`; it must not specify `url` or `headers`.

Relative `cwd` and command paths containing a path separator are resolved against the directory containing the configuration file. Arguments are passed as distinct process arguments without implicit shell parsing or rewriting.

stdio children do **not** inherit the Hub process environment wholesale. The implicit compatibility baseline is limited to runtime discovery and operating-system essentials such as `PATH`, home/profile directories, temp directories, locale/timezone variables, common CA trust-store paths, and platform variables needed by common runtimes.

Ambient credentials, proxy variables, SSH agent sockets, cloud-provider variables, and arbitrary Hub process variables are excluded by default.

Use the server `env` map for explicit opt-in:

```json
{
  "env": {
    "GITHUB_TOKEN": "${GITHUB_TOKEN}"
  }
}
```

Explicit values override the compatibility baseline. Hub MCP/Admin authentication values are additionally filtered from implicit inheritance as defense in depth.

## Streamable HTTP servers

A `streamable_http` server requires `url`. It may specify `headers`, but not `command`, `args`, `cwd`, or `env`.

Remote URLs require HTTPS. Plain HTTP is restricted to literal loopback hosts. URL userinfo and fragments are rejected. Redirects are not followed.

Transport-owned or hop/session framing headers cannot be configured, including values such as `Host`, `Content-Length`, `Connection`, `Mcp-Session-Id`, and `Mcp-Protocol-Version`.

## Environment expansion

Header and stdio environment values support one-pass `${NAME}` expansion.

- `${NAME}` resolves from the Hub process environment;
- `$${NAME}` produces literal `${NAME}`;
- missing required variables fail validation;
- expansion never writes resolved secrets back to disk;
- expansion never invokes a shell.

Required public MCP and Admin tokens must remain non-empty after expansion, satisfy the configured minimum length policy, and differ from one another in public mode.

## Timeouts and limits

- `startupTimeout`: positive duration, at most 24 hours; default `20s`.
- `callTimeout`: positive duration, at most 24 hours; default `60s`.
- `maxConcurrency`: integer 1-64; default `8`; there is no waiting queue.
- Maximum config size: 1 MiB.
- Maximum configured servers: 32.
- `tools.disabled` contains original downstream tool names.
- Valid newly discovered tools are otherwise publishable by default.

## Hot reload semantics

Connection-level downstream changes include command, args, cwd, env, URL, headers, transport, startup timeout, or concurrency.

Those changes use break-before-make semantics: the old generation is drained/closed and the new generation connects before its new catalog becomes active.

A call already admitted remains on the generation from which it obtained a lease.

A call-timeout-only change affects new calls without requiring a downstream generation replacement.

Startup-bound Hub settings require a process restart, including listener address, public mode/URL, allowed hosts, trusted proxies, MCP bearer token, Admin enablement/token, and Admin session timeout.

A successful downstream hot reload does **not** imply startup-bound credentials have been revoked. When `restartRequired` is reported, restart the Hub to apply those settings fully.

## Import

```bash
mcp-hub import --from source.json --config config.json --dry-run
mcp-hub import --from source.json --config config.json --yes
```

Import handles common `mcpServers` files but does not start servers.

A URL without an explicit type requires the supported remote-type choice; old `sse` input is not silently converted into Streamable HTTP.

Import previews expose environment/header names rather than existing secret values. Overwriting an existing server ID is rejected unless the command explicitly supports and requests that behavior.

## Persistence and concurrency

Configuration reads and digests are bounded by the 1 MiB limit.

Writes use a same-directory temporary file with restrictive permissions, flush/sync, close, atomic replacement, and parent-directory durability sync where supported.

An OS-backed advisory lock plus pre-write digest/ETag comparison prevents concurrent writers from silently overwriting a changed file.

The file remains the persistent source of truth even when changes originate from Browser Admin or Remote Admin CLI.

## Browser Admin and Remote Admin writes

Browser Admin and `mcp-hub admin` both use the authenticated Admin API and the same configuration transaction model.

For normal downstream CRUD, prefer those paths instead of bypassing them with direct file editing.

Before persisting a new or connection-changed enabled downstream, the Hub:

1. authenticates and checks mutation preconditions;
2. reads the bounded request body;
3. enters the shared configuration transaction;
4. loads the current raw config and verifies ETag/CAS;
5. strictly decodes and validates the mutation;
6. creates an isolated temporary downstream session;
7. lists and validates the discovered tool catalog;
8. atomically persists the new config;
9. applies the validated runtime snapshot;
10. rolls back persistence and restores the previous runtime snapshot if the live apply fails.

Network request bodies are read before the shared transaction lock is acquired so a slow client upload cannot stall unrelated runtime file-poll work.

Remote CLI usage and `server.json` format are documented in [REMOTE-ADMIN.md](REMOTE-ADMIN.md).

## External file edits

The runtime controller observes configuration file changes and only applies validated snapshots.

An invalid external edit leaves the last valid runtime configuration active and records the rejection in diagnostics.

Direct file editing can still be appropriate for bootstrap, startup-bound Hub settings, or offline recovery, but do not concurrently bypass Browser/CLI Admin CAS unless you intentionally accept conflict/reload behavior.

## Validate and serve

```bash
mcp-hub validate --config config.json
mcp-hub serve --config config.json
```

`validate` performs strict decoding, validation, environment expansion, and path resolution only. It does **not** start stdio processes or connect to remote downstreams.

`serve` validates startup policy, binds the Hub listener, and then starts downstream runtime coordination.

## Related documentation

- [Remote Admin CLI](REMOTE-ADMIN.md)
- [Security model](SECURITY.md)
- [Compatibility](COMPATIBILITY.md)
- [VPS/public deployment](VPS.md)
- [Implementation status](IMPLEMENTATION-STATUS.md)
