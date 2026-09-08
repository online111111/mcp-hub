# MCP Manager deployment playbook

## Server/VPS

1. Inspect OS/arch, current binary, config, service unit, listener, proxy, and firewall.
2. Back up the current binary/config/environment/service/proxy files.
3. Install `mcp-manager` from a verified Release or build it from source.
4. Preserve existing JSON configuration. New examples use `MCP_MANAGER_TOKEN` and `MCP_MANAGER_ADMIN_TOKEN`; legacy `MCP_HUB_*` variables remain valid during migration.
5. Run `mcp-manager validate --config <path>`.
6. Start or restart the service and verify loopback `/healthz` and `/readyz`.
7. If public access is needed, expose only 80/443 through Caddy/Nginx and keep Manager on loopback.
8. Run `mcp-manager status` and `mcp-manager doctor`.

## Existing MCP Hub migration

Treat this as an in-place product rename, not a new deployment:

- Keep the same config schema and `/mcp`/`/admin` URLs.
- Preserve tokens unless the user explicitly requests rotation.
- Install the new binary next to the old one or stage it as `.new`.
- Validate the existing config with the new binary before replacement.
- Update the service command from `mcp-manager` to `mcp-manager` only after validation.
- Keep a rollback copy of the old binary/service unit until post-restart checks pass.
- Service names/directories may remain legacy names temporarily; rename them separately if operationally useful.

## HTTP client

Prefer native Streamable HTTP:

```text
https://<manager-host>/mcp
```

For public authentication use the MCP bearer token. Do not use the Admin token for normal MCP calls.

## stdio-only client

```bash
MCP_MANAGER_TOKEN='...' mcp-manager stdio --connect https://mcp.example.com/mcp
```

## Remote downstream administration

```bash
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin list --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin get <id> --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin add <id> --file ./server.json --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin edit <id> --file ./server.json --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin delete <id> --endpoint https://mcp.example.com --yes
```

Use HTTPS for remote Admin. Mutations retain CSRF, ETag/CAS, strict validation, downstream preflight, atomic persistence, rollback, and hot reload.

## Upgrade

1. Record current version/checksum/path and service state.
2. Back up binary/config/service files.
3. Stage the new binary.
4. Validate the existing config.
5. Replace atomically where practical and restart.
6. Verify status, doctor, Admin login when enabled, and one known client path.
7. Roll back if the deployment becomes unhealthy.

## Windows/macOS

The same validation and backup rules apply. On Windows install `mcp-manager.exe`; on macOS use the matching Darwin release archive. Do not copy Linux service-manager commands verbatim to those platforms.
