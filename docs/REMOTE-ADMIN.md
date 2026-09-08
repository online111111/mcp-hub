# MCP Manager Remote Admin CLI

MCP Manager clients can manage downstream `mcpServers` on a running Manager without SSH access or direct edits to the server JSON file. The CLI uses the same authenticated Admin management plane as `/admin/`; it does not bypass validation, preflight, persistence, CAS, or rollback safeguards.

## Prerequisites

- `hub.admin.enabled` is `true`.
- The client can reach the Manager base URL.
- Remote hosts use HTTPS; plaintext HTTP is accepted only for loopback development.
- The client has `mcp-manager`.
- The operator has the Admin token, not the public MCP bearer token.

New deployments use:

```bash
export MCP_MANAGER_ADMIN_TOKEN='<ADMIN_TOKEN>'
```

Legacy `MCP_HUB_ADMIN_TOKEN` remains accepted during the rename compatibility window. `--token` overrides environment variables, but command-line arguments can be visible to other local users.

## Commands

```bash
mcp-manager admin list --endpoint https://mcp.example.com
mcp-manager admin list --endpoint https://mcp.example.com --json
mcp-manager admin get github --endpoint https://mcp.example.com
mcp-manager admin add filesystem --file ./filesystem.json --endpoint https://mcp.example.com
mcp-manager admin edit filesystem --file ./filesystem.json --endpoint https://mcp.example.com
mcp-manager admin delete filesystem --endpoint https://mcp.example.com --yes
```

The endpoint may be the base URL or the same host ending in `/mcp`, `/admin`, or `/admin/`; the CLI normalizes these forms. Userinfo, query strings, fragments, unsafe remote HTTP, and redirects are rejected.

## `server.json`

`--file` contains exactly one downstream server object, not the full root configuration.

stdio example:

```json
{
  "enabled": true,
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/srv/data"],
  "env": { "EXAMPLE_TOKEN": "${EXAMPLE_TOKEN}" }
}
```

Streamable HTTP example:

```json
{
  "enabled": true,
  "type": "streamable_http",
  "url": "https://tools.example.com/mcp",
  "headers": { "Authorization": "Bearer ${REMOTE_MCP_TOKEN}" }
}
```

Input is strict-decoded and capped at 1 MiB. Environment references are resolved on the **MCP Manager server**, not on the client machine running the Admin command.

## Secret handling

`admin get` and `admin list --json` consume the redacted Admin representation. They are inspection/editing views, not secret-export mechanisms. Prefer server-side environment references for long-lived credentials.

## Mutation transaction

Every add/edit/delete:

1. authenticates to the Admin plane;
2. receives a server-side session and CSRF token;
3. reads the current redacted configuration and ETag;
4. submits the mutation with `If-Match`;
5. runs strict validation;
6. preflights new or connection-changing downstreams;
7. persists through the shared config lock and atomic-write path;
8. reloads the validated runtime snapshot;
9. restores/retains the previous healthy state if the transaction fails.

Concurrent Browser/CLI/file changes can make the ETag stale. Refetch and re-apply the intended change; do not bypass CAS with a direct overwrite.

## Security properties

The Remote Admin client intentionally rejects non-loopback plaintext HTTP and redirects, separates MCP/Admin credentials, uses CSRF and ETag/CAS, consumes redacted reads, bounds input, and reuses the server validation/preflight/rollback path.

Anyone with the Admin token can change the downstream execution surface. Treat it as a privileged credential.

## Exit behavior

- `0`: success
- `2`: invalid parameters or client/config/auth/conflict conditions such as 400/401/403/404/409
- `3`: transport failure, unavailable Admin service, or server-side runtime failure

## Troubleshooting

1. Verify HTTPS/reverse-proxy routing and Admin enablement.
2. Verify `MCP_MANAGER_ADMIN_TOKEN` is the Admin credential, not `MCP_MANAGER_TOKEN`.
3. Verify server-side `${NAME}` references exist in the Manager service environment.
4. Verify downstream commands/URLs are reachable from the Manager host.
5. Retry conflicts only after refetching current state.
6. Run `mcp-manager status` and `mcp-manager doctor` after significant changes.

Use Browser Admin for interactive editing, Remote Admin CLI for terminal/scripts/Agent automation, and direct file editing mainly for bootstrap, offline recovery, or startup-bound settings.

## Related documentation

- [Configuration](CONFIG.md)
- [Security model](SECURITY.md)
- [VPS deployment](VPS.md)
- [Compatibility](COMPATIBILITY.md)
- [Implementation status](IMPLEMENTATION-STATUS.md)
