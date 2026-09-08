---
name: mcp-manager-deployer
description: Deploy, configure, upgrade, repair, verify, or remove MCP Manager for server-side hosting or client-side access. Use when an agent needs to install online111111/mcp-manager (or migrate an existing online111111/mcp-hub installation), deploy it to Linux/macOS/Windows or a VPS, expose it safely through HTTPS, configure downstream MCP services, connect HTTP or stdio-only clients, use Remote Admin, upgrade an existing installation, or recover a broken deployment.
---

# MCP Manager Deployer

Deploy and maintain MCP Manager as a repeatable, verified operation. Treat existing installations as state that must be preserved unless the user explicitly requests replacement or deletion.

## Operating rules

- Inspect OS, architecture, privilege level, current binary/config/service, ports, and reverse proxy before changing anything.
- Prefer a stable GitHub Release and verify `SHA256SUMS`. Build from source only when needed.
- Use `scripts/install_release.py` when Python 3 is available.
- Never expose port `8080` directly to the public Internet. Prefer loopback MCP Manager plus a trusted HTTPS reverse proxy.
- Use separate MCP and Admin tokens of at least 32 characters. Never print token values in the final report.
- New deployments use `MCP_MANAGER_TOKEN` and `MCP_MANAGER_ADMIN_TOKEN`. Existing `MCP_HUB_TOKEN` and `MCP_HUB_ADMIN_TOKEN` installations are legacy-compatible during the rename migration; do not rotate them merely to rename the product.
- Run persistent server deployments as a dedicated non-root account unless the user requires another model.
- Back up an existing binary, config, environment file, service unit, and reverse-proxy snippet before mutation.
- Run `mcp-manager validate` before restart; run `status` and `doctor` after changes. Roll back if a previously healthy deployment becomes unhealthy.
- Preserve existing downstream `mcpServers` unless the user asks to add, edit, or remove entries.
- Prefer `mcp-manager admin` over SSH-editing a remote config when Remote Admin is available.
- Do not claim full MCP 2026-07-28 compatibility merely because the project uses Go SDK v1.7.0.

## Choose the workflow

1. **Server/VPS**: install one persistent MCP Manager and aggregate downstream services.
2. **Local all-in-one**: run MCP Manager on the same machine as the client.
3. **HTTP client**: connect directly to `https://<host>/mcp`.
4. **stdio-only client**: install the local binary and bridge to the remote `/mcp` endpoint.
5. **Remote Admin client**: manage downstream services through the authenticated Admin plane.
6. **Upgrade/repair**: preserve the working deployment until the replacement passes verification.

Read `references/deployment-playbook.md` for execution order, `references/config-recipes.md` for configuration, and `references/verification-and-recovery.md` before declaring success.

## Server deployment baseline

Use this order:

1. Install/upgrade `mcp-manager` with checksum verification.
2. Create/preserve a dedicated service account and persistent directories.
3. Generate or preserve credentials.
4. Create or preserve `config.json`.
5. Run `mcp-manager validate --config <path>`.
6. Start/restart the service.
7. Verify loopback health before configuring public HTTPS.
8. Run `mcp-manager status` and `mcp-manager doctor`.
9. Report endpoints, version, paths, service state, verification, and rollback location without secrets.

For public VPS deployments prefer:

- `hub.listen`: `127.0.0.1:8080`
- `hub.publicMode`: `true`
- `hub.publicUrl`: `https://<domain>`
- narrow `allowedHosts`
- only actual proxy addresses in `trustedProxies`
- `${MCP_MANAGER_TOKEN}` and `${MCP_MANAGER_ADMIN_TOKEN}`
- only ports 80/443 exposed publicly

## Client access

### Streamable HTTP

Use the MCP endpoint:

```text
https://<host>/mcp
```

When authentication is enabled, provide the MCP bearer token through the client's secret facility. Use `mcp-manager export` when the target client is supported instead of inventing a config shape.

### stdio bridge

```bash
MCP_MANAGER_TOKEN='...' mcp-manager stdio --connect https://mcp.example.com/mcp
```

### Remote Admin

```bash
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin list --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin get <id> --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin add <id> --file ./server.json --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin edit <id> --file ./server.json --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin delete <id> --endpoint https://mcp.example.com --yes
```

Remote Admin reuses the authenticated Admin session, CSRF protection, ETag/CAS conflict checks, strict validation, preflight, atomic persistence, rollback, and hot reload. Remote plaintext HTTP is rejected except for loopback development.

## Downstream safety

- `stdio` downstreams require `command`; use explicit `env` for credentials that must reach the child.
- `streamable_http` downstreams require `url`; non-loopback remote targets require HTTPS.
- Do not silently convert legacy SSE endpoints to Streamable HTTP.
- Remember that stdio downstreams execute with the MCP Manager service user's privileges; this product is not a sandbox.

## Rename migration

When upgrading an existing MCP Hub installation to MCP Manager:

1. Back up the old binary/config/service files.
2. Keep the existing JSON schema and `/mcp`/`/admin` endpoints; they do not need migration.
3. Install the new `mcp-manager` binary alongside or atomically replace the old binary after validation.
4. Existing `MCP_HUB_*` token variables may remain during the compatibility window. Prefer `MCP_MANAGER_*` for newly written service files.
5. Rename service names/directories only when operationally useful; do not break a working deployment just for cosmetics.
6. Verify with the new binary before removing any old compatibility artifact.

## Upgrade and rollback

Before an upgrade record the installed version, binary path/checksum, config path, service status, and reverse-proxy state. Validate the new binary against the existing config before replacing the running binary. If post-restart `status`/`doctor` or endpoint checks fail, restore the previous binary/config and restart it.

## Completion report

Return:

- mode and target OS/host
- installed MCP Manager version
- binary/config/service paths
- MCP endpoint and Admin URL if enabled
- verification results
- HTTPS/reverse-proxy state
- backup/rollback location for changed installations
- any remaining manual client step

Never include credential values.
