# MCP Hub configuration

MCP Hub reads one strict JSON configuration file. The file is the source of truth; the Hub does not create a database or `state.json`.

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

`enabled` defaults to `true`. `type` is required in the saved configuration and is either `stdio` or `streamable_http`.

## stdio servers

A stdio server requires `command`. It may also specify `args`, `cwd`, and `env`; it must not specify `url` or `headers`.

Relative `cwd` and command paths containing a path separator are resolved against the directory containing the configuration file. Arguments are passed without shell parsing or rewriting.

Stdio children do **not** inherit the Hub process environment wholesale. The implicit compatibility baseline is limited to runtime discovery and operating-system essentials: `PATH`, `HOME`, temporary-directory variables, locale/timezone variables, common CA trust-store paths, and the Windows system/profile variables needed by common Node/Python/npm-style runtimes. Ambient credentials, proxy variables, SSH agent sockets, cloud-provider variables, and arbitrary Hub process variables are excluded by default.

Use the server `env` map for explicit opt-in. For example, `"GITHUB_TOKEN": "${GITHUB_TOKEN}"` deliberately forwards that variable to the trusted downstream process. Explicit values override the compatibility baseline. Hub MCP/Admin authentication-token values are additionally filtered from implicit inheritance as defense in depth.

## Streamable HTTP servers

A `streamable_http` server requires `url`. It may specify `headers`, but not `command`, `args`, `cwd`, or `env`.

HTTPS URLs are accepted for remote services. Plain HTTP is restricted to literal loopback hosts. URL userinfo and fragments are rejected. Redirects are not followed. Transport-owned headers such as `Host`, `Content-Length`, `Connection`, `Mcp-Session-Id`, and `Mcp-Protocol-Version` cannot be configured.

Header and stdio environment values support one-pass `${NAME}` expansion. `$${NAME}` produces the literal `${NAME}`. Missing variables fail `validate`/`serve`; expansion never writes secrets back to the file. Required Hub MCP and Admin tokens must remain non-empty after expansion, must each be at least 32 characters, and must differ from one another.

## Timeouts and limits

- `startupTimeout`: positive duration, at most 24 hours; default `20s`.
- `callTimeout`: positive duration, at most 24 hours; default `60s`.
- `maxConcurrency`: integer 1–64; default `8`. There is no waiting queue.
- Maximum config size: 1 MiB.
- Maximum configured servers: 32.
- `tools.disabled` is a list of original downstream names. New tools are open by default.

Connection-level changes (command, args, cwd, env, URL, headers, transport, startup timeout, or concurrency) use break-before-make: the old generation drains and closes before the new one connects. A call already admitted remains on its original generation. A call-timeout-only change affects new calls without changing the generation. Startup-bound settings (listen address, public mode/URL, allowed hosts, trusted proxies, MCP bearer token, admin enablement/token/session timeout) require a process restart and are not hot-swapped. A successful downstream reload does **not** revoke the old authentication tokens; restart the Hub to apply credential changes.

## Import and writes

```powershell
mcp-hub import --from source.json --config config.json --dry-run
mcp-hub import --from source.json --config config.json --yes
```

Import handles common `mcpServers` files, but does not start servers. A URL without an explicit type requires `--remote-type streamable_http`; old `sse` is not silently converted. Import previews show header/environment names, not values, and overwriting an existing server ID is rejected.

Configuration reads and digests are bounded by the 1 MiB configuration limit. Writes use a mode-`0600` same-directory temporary file, flush/sync, close, atomic replacement, and parent-directory durability sync where the operating system supports it. An OS-backed advisory lock plus a pre-write digest check prevents concurrent writers from overwriting a changed file without leaving a crash-stale lock condition.

Before an Admin save persists a new or connection-changed enabled downstream, the Hub creates an isolated temporary session, lists its tools, and validates the discovered catalog. A failed preflight returns an error without changing the file or live routing table. If persistence succeeds but the subsequent live reload fails, the file is atomically rolled back and the previous runtime snapshot is restored. An invalid external edit leaves the last valid runtime configuration in place and reports the rejection in diagnostics.

## Validate and serve

```powershell
mcp-hub validate --config config.json
mcp-hub serve --config config.json
```

`validate` performs strict decoding, validation, and in-memory environment/path resolution only. It does not start child processes or make downstream network calls. `serve` binds the loopback listener before starting downstream coordinators.
