# MCP Manager verification and recovery

## Verification gates

After install, compatibility migration, config edit, or upgrade:

1. `mcp-manager version`
2. `mcp-manager validate --config <path>`
3. service/process is active and listening only where intended
4. `mcp-manager status --endpoint <base-url>`
5. `mcp-manager doctor --endpoint <base-url>`
6. `/healthz` and `/readyz` return the expected status
7. if Admin is enabled, `/admin/` is reachable only through the intended origin and authentication succeeds
8. for public deployments, HTTPS is active and port 8080 is not publicly exposed
9. verify at least one representative client path when the client is available

Do not treat a green local browser smoke test as proof of every reverse proxy, GUI client, or third-party MCP server combination.

## Compatibility-upgrade verification

For an existing pre-v0.4 deployment, additionally verify:

- the service launches `mcp-manager` after its command is migrated;
- existing `${MCP_HUB_TOKEN}` / `${MCP_HUB_ADMIN_TOKEN}` configurations still authenticate during the compatibility window;
- newly generated client config uses `MCP_MANAGER_TOKEN` and the `mcp-manager` command;
- `/mcp`, `/admin/`, and the existing config schema remain unchanged;
- no previous binary is deleted until the new binary passes health and client checks.

## Recovery

If a previously healthy deployment fails after a change:

1. capture the failing service status and logs without exposing secrets;
2. stop the new process if necessary;
3. restore the previous binary and, if changed, previous config/service unit;
4. restart the previous version;
5. rerun health/status/doctor checks;
6. preserve failure evidence for diagnosis.

Do not delete configuration, environment files, backups, or downstream data as part of automatic recovery.
