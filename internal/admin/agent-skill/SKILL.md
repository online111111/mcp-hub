---
name: mcp-hub-deployer
description: Deploy, configure, upgrade, repair, verify, or remove online111111/mcp-hub for either server-side Hub hosting or client-side access. Use when an agent must install MCP Hub on Linux, macOS, or Windows; deploy it to a VPS; expose it safely through HTTPS; configure local or remote downstream MCP servers; connect HTTP-capable clients; create a stdio bridge for stdio-only clients; migrate or upgrade an existing installation; diagnose a broken deployment; or produce exact deployment commands when direct shell/SSH access is unavailable.
---

# MCP Hub Deployer

Deploy `https://github.com/online111111/mcp-hub` as a repeatable, verified operation. Prefer a working, supportable deployment over the shortest command sequence.

## Operating rules

- Treat the target machine and current installation as the source of truth. Inspect before changing.
- Prefer the latest stable GitHub Release. Pin a version when reproducibility matters. Build from source only when no suitable release exists or the user requests it.
- Use `scripts/install_release.py` when Python 3 is available; it selects the correct OS/architecture archive and verifies `SHA256SUMS` before installing.
- Never expose Hub port `8080` directly to the public Internet. For public hosting, keep Hub on loopback and terminate TLS in Caddy/Nginx/another trusted reverse proxy.
- Never print or repeat secret token values in the final report. Prefer environment variables or a mode-0600 environment file over command-line `--token` arguments.
- Public MCP and Admin tokens must be distinct and at least 32 characters after expansion.
- Run a server deployment as a dedicated non-root account unless the user explicitly requires another model.
- Back up an existing binary, config, service unit, and reverse-proxy snippet before modifying them.
- Validate configuration before starting/restarting. After a change, verify process state and application health. Roll back if a previously working deployment becomes unhealthy.
- Do not delete configuration, credentials, or user data during uninstall/cleanup without explicit user approval.
- Do not invent a client configuration format. Prefer `mcp-hub export` or inspect that client's current MCP documentation/config if export does not support it.
- Keep status updates concise. Finish with endpoints, service state, installed version, config locations, and any remaining manual step; never expose secrets.

## Decision workflow

1. Determine the requested mode:
   - **Server / VPS**: host one Hub that aggregates downstream MCP servers.
   - **Local all-in-one**: run the Hub on the same machine as the client.
   - **HTTP client**: connect a client that natively supports Streamable HTTP to an existing Hub.
   - **stdio client / bridge**: install the binary locally and bridge stdio to an existing HTTP Hub.
   - **Upgrade / repair / uninstall**: preserve the existing deployment until the replacement is verified.
2. Determine execution capability:
   - If shell/SSH/remote execution is available, perform the deployment directly.
   - If not, produce exact commands and files for the user's target OS; do not claim they were executed.
3. Inspect OS, architecture, privilege level, service manager, existing `mcp-hub`, current config, listening ports, and reverse proxy before mutation.
4. Read `references/deployment-playbook.md` for the selected workflow.
5. Read `references/config-recipes.md` when creating or editing configuration.
6. Apply `references/verification-and-recovery.md` before declaring success.

## Server deployment

Use this order:

1. Install or upgrade the binary with checksum verification.
2. Create a dedicated service user and directories when running persistently on Linux.
3. Generate or preserve secrets. Never rotate existing secrets implicitly during an unrelated upgrade.
4. Create a minimal config first. Add downstream MCP servers only from explicit user requirements or existing config.
5. Run `mcp-hub validate --config <path>`.
6. Install/update the service unit and start the Hub.
7. If public access is required, configure HTTPS reverse proxy only after loopback Hub health passes.
8. Run `status`, `doctor`, and endpoint checks.
9. Report a concise deployment inventory.

For public VPS deployments, use the repository's security model:

- `hub.listen`: `127.0.0.1:8080`
- `hub.publicMode`: `true`
- `hub.publicUrl`: `https://<domain>`
- `allowedHosts`: only expected hostnames
- `trustedProxies`: only the actual local/private proxy addresses, never broad public CIDRs
- separate `${MCP_HUB_TOKEN}` and `${MCP_HUB_ADMIN_TOKEN}`
- only ports 80/443 exposed publicly

Do not enable public mode without a hostname/TLS plan. If the user has no domain, deploy loopback/private mode and state what is still needed for safe public access.

## Client deployment

### HTTP-capable client

Prefer native Streamable HTTP. The endpoint is normally:

`https://<hub-host>/mcp`

Use `Authorization: Bearer <MCP_HUB_TOKEN>` when the Hub requires authentication. Put the token in the client's secret/environment facility when available.

When the target client is supported by the CLI, run `mcp-hub export --help`, then generate its config with `mcp-hub export` instead of hand-writing undocumented JSON.

### Remote Admin client

When the user wants to manage downstream services from a client machine, prefer the built-in remote Admin CLI instead of SSH-editing the server config. Set `MCP_HUB_ADMIN_TOKEN` and use the Hub's HTTPS base URL:

```bash
MCP_HUB_ADMIN_TOKEN='...' mcp-hub admin list --endpoint https://mcp.example.com
MCP_HUB_ADMIN_TOKEN='...' mcp-hub admin get <id> --endpoint https://mcp.example.com
MCP_HUB_ADMIN_TOKEN='...' mcp-hub admin add <id> --file ./server.json --endpoint https://mcp.example.com
MCP_HUB_ADMIN_TOKEN='...' mcp-hub admin edit <id> --file ./server.json --endpoint https://mcp.example.com
MCP_HUB_ADMIN_TOKEN='...' mcp-hub admin delete <id> --endpoint https://mcp.example.com --yes
```

The remote CLI deliberately reuses the Admin session, CSRF, ETag/CAS, validation, preflight, atomic write, rollback, and hot-reload path. Prefer the environment variable over `--token`; do not print the Admin token. Remote plaintext HTTP must not be used.

### stdio-only client

Install the local `mcp-hub` binary and use the bridge:

```bash
MCP_HUB_TOKEN='...' mcp-hub stdio --connect https://mcp.example.com/mcp
```

Prefer an environment variable over `--token`. For local loopback Hub, omit authentication if the Hub is configured without it.

Verify the remote Hub before modifying the client's MCP config:

```bash
MCP_HUB_TOKEN='...' mcp-hub status --endpoint https://mcp.example.com
MCP_HUB_TOKEN='...' mcp-hub doctor --endpoint https://mcp.example.com
```

## Downstream configuration

- `stdio` downstreams require `command`; use `args`, `cwd`, and `env` only when needed.
- `streamable_http` downstreams require `url`; remote services must use HTTPS, while plain HTTP is only for literal loopback targets.
- Explicitly forward required environment variables with server `env`, for example `"GITHUB_TOKEN": "${GITHUB_TOKEN}"`. Do not depend on ambient credential inheritance.
- Never silently convert legacy SSE endpoints to Streamable HTTP.
- Before adding an unfamiliar third-party stdio MCP, explain that it executes with the Hub service user's privileges.
- Preserve existing `mcpServers` entries unless the user asks to replace/remove them.

## Upgrade workflow

1. Capture current binary version, config path, service status, and binary checksum/path.
2. Back up the current binary and config.
3. Install the requested/new stable version to a temporary path first.
4. Run `<new-binary> validate --config <existing-config>`.
5. Replace the binary atomically when practical and restart the service.
6. Run all verification gates.
7. If health fails, restore the old binary/config and restart; report the failure without deleting evidence/logs.

Do not automatically merge dependency updates or migrate config semantics merely because a newer binary exists.

## Repair workflow

Diagnose in this order:

1. `mcp-hub validate --config ...`
2. service/process state and recent logs
3. listening socket and reverse-proxy reachability
4. `mcp-hub status`
5. `mcp-hub doctor`
6. downstream-specific startup/connection failure

Fix the narrowest confirmed cause. Do not replace a valid config wholesale to solve a single broken downstream.

## Uninstall workflow

Stop and disable the service first, then remove only the binary/service artifacts the user approved. Preserve config, environment files, and backups by default. Ask before deleting `/etc/mcp-hub`, user configuration, or downstream data.

## Required completion report

Return:

- deployment mode and target host/OS
- installed MCP Hub version
- binary path
- config path
- service manager/service name if any
- MCP endpoint and Admin URL if enabled
- health verification results
- whether HTTPS/reverse proxy is active
- backup/rollback location when an existing install was changed
- any manual client-side step still required

Never include token values. If direct execution was unavailable, clearly label the result as a deployment plan rather than a completed deployment.
