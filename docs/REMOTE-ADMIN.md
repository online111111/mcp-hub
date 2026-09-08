# Remote Admin CLI

MCP Hub clients can manage downstream `mcpServers` on a running Hub without SSH access or direct edits to the server's JSON file.

The CLI is a client of the same Admin management plane used by `/admin/`; it does not bypass server-side authentication, validation, preflight, persistence, or rollback safeguards.

## Prerequisites

- `hub.admin.enabled` is `true` on the server.
- The client can reach the Hub base URL.
- Remote hosts use HTTPS. Plain HTTP is accepted only for loopback/local development.
- The client has a compatible `mcp-hub` binary.
- The operator has the **Admin token**, not the public MCP token.

Prefer an environment variable:

```bash
export MCP_HUB_ADMIN_TOKEN='<ADMIN_TOKEN>'
```

`--token` can override the environment variable, but command-line arguments may be visible to other local users.

## Endpoint forms

The CLI accepts the Hub base URL, for example:

```text
https://mcp.example.com
```

It also normalizes the same host ending in `/mcp`, `/admin`, or `/admin/` back to the Hub base URL.

Userinfo, query strings, fragments, unsafe remote HTTP, and redirects are rejected.

## List servers

Human-readable output:

```bash
mcp-hub admin list --endpoint https://mcp.example.com
```

Redacted JSON output:

```bash
mcp-hub admin list --endpoint https://mcp.example.com --json
```

## Get one server

```bash
mcp-hub admin get github --endpoint https://mcp.example.com
```

The returned server representation is redacted. Existing sensitive values are represented through the Admin secret-placeholder mechanism rather than disclosed.

## Add a server

```bash
mcp-hub admin add filesystem \
  --file ./filesystem.json \
  --endpoint https://mcp.example.com
```

`add` refuses an ID that already exists.

## Edit a server

```bash
mcp-hub admin edit filesystem \
  --file ./filesystem.json \
  --endpoint https://mcp.example.com
```

`edit` refuses a missing ID.

## Delete a server

```bash
mcp-hub admin delete filesystem \
  --endpoint https://mcp.example.com \
  --yes
```

Remote deletion requires explicit `--yes`.

## `server.json` format

`--file` contains **one server object**, not a full Hub configuration.

### stdio example

```json
{
  "enabled": true,
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/srv/data"],
  "env": {
    "EXAMPLE_TOKEN": "${EXAMPLE_TOKEN}"
  }
}
```

### Streamable HTTP example

```json
{
  "enabled": true,
  "type": "streamable_http",
  "url": "https://tools.example.com/mcp",
  "headers": {
    "Authorization": "Bearer ${REMOTE_MCP_TOKEN}"
  }
}
```

The input is strictly decoded. Unknown fields, malformed JSON, invalid transport combinations, and unsafe remote HTTP targets are rejected. CLI input is capped at 1 MiB.

## Where `${NAME}` is resolved

Environment references in a downstream server definition are resolved on the **Hub server**, not on the client machine that runs `mcp-hub admin`.

For example:

```json
{
  "headers": {
    "Authorization": "Bearer ${REMOTE_MCP_TOKEN}"
  }
}
```

requires `REMOTE_MCP_TOKEN` to exist in the Hub service environment.

Do not copy a long-lived secret into a client-side JSON file merely to perform an Admin edit when a server-side environment reference can be used instead.

## Secret redaction

`admin get` and `admin list --json` use the redacted Admin representation.

Treat retrieved JSON as an inspection/editing view, not a secret-export mechanism.

If an edit needs a new secret:

1. add/update the secret in the Hub service environment through your normal secret-management path;
2. reference it as `${NAME}` in the server configuration;
3. restart the Hub only when the changed service environment requires it.

## Mutation transaction

Every `add`, `edit`, or `delete` follows the same server-side transaction model as the browser console:

1. authenticate with the Admin token;
2. receive an Admin session and CSRF token;
3. fetch the current redacted configuration and ETag;
4. submit the mutation with `If-Match`;
5. run strict configuration validation;
6. preflight new or connection-changing downstreams;
7. persist through the shared config lock and atomic-write path;
8. reload the validated runtime snapshot;
9. retain/restore the last healthy state if the transaction fails.

The raw configuration file remains the persistent source of truth.

## Concurrent edits

A concurrent Browser/CLI/file change can make the ETag stale.

The correct behavior is a conflict, not silent overwrite.

When that happens:

1. fetch the latest server/config state;
2. re-apply the intended change;
3. retry.

Do not bypass CAS by directly overwriting the config file while another administrator is using the management plane.

## Security properties

The remote Admin client intentionally:

- rejects non-loopback plaintext HTTP;
- rejects redirects;
- separates the Admin token from the MCP token;
- uses the server-issued session and CSRF token;
- preserves ETag/CAS conflict protection;
- consumes redacted reads;
- bounds configuration input;
- uses the server's validation/preflight/rollback path.

Anyone with the Admin token effectively has authority to change the Hub's downstream execution surface. Treat it as a privileged credential.

## Exit behavior

Common outcomes:

- exit `0`: command completed successfully;
- exit `2`: invalid parameters or a client/config/auth/conflict condition such as HTTP 400/401/403/404/409;
- exit `3`: transport failure, unavailable Admin service, or server-side runtime failure.

## Troubleshooting

If `admin list` fails:

1. verify the URL uses the Hub base host;
2. verify HTTPS and reverse-proxy routing;
3. verify `MCP_HUB_ADMIN_TOKEN` is the Admin credential, not `MCP_HUB_TOKEN`;
4. verify Admin is enabled;
5. check server logs if the failure is not safely described by the CLI.

If a mutation fails:

1. read the returned validation/preflight category;
2. verify server-side environment references exist;
3. verify the downstream command/URL is reachable **from the Hub server**;
4. retry conflicts only after refetching the latest state;
5. use `status` and `doctor` after significant changes.

## Browser vs CLI vs direct file editing

Use Browser Admin when interactive editing is convenient.

Use Remote Admin CLI for scripts, terminal workflows, and Agent automation.

Use direct file editing mainly for offline recovery, initial bootstrap, or changes to startup-bound Hub settings that are not exposed as a per-server Admin mutation.

For normal downstream CRUD, prefer Browser/CLI Admin so validation, conflict detection, preflight, and rollback remain active.

## Related documentation

- [Configuration](CONFIG.md)
- [Security model](SECURITY.md)
- [VPS deployment](VPS.md)
- [Compatibility](COMPATIBILITY.md)
- [Implementation status](IMPLEMENTATION-STATUS.md)
