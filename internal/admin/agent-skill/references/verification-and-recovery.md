# Verification and recovery

A deployment is complete only after the relevant gates pass.

## Binary

- executable exists at the reported path
- version/help executes successfully
- installed binary came from a verified release checksum or a locally tested source build

## Configuration

```bash
mcp-hub validate --config <config>
```

Validation must succeed before start/restart.

## Service

For systemd:

```bash
systemctl is-enabled mcp-hub
systemctl is-active mcp-hub
systemctl --no-pager --full status mcp-hub
journalctl -u mcp-hub -n 100 --no-pager
```

Do not dump environment variables or tokens into the report.

## Hub health

Local/private:

```bash
mcp-hub status --endpoint http://127.0.0.1:8080
mcp-hub doctor --endpoint http://127.0.0.1:8080
```

Authenticated public:

```bash
MCP_HUB_TOKEN='...' mcp-hub status --endpoint https://mcp.example.com
MCP_HUB_TOKEN='...' mcp-hub doctor --endpoint https://mcp.example.com
```

The MCP endpoint is `/mcp`; the Admin page, when enabled, is `/admin/`.

## Network/public checks

- Hub listens only on expected local/private address.
- Reverse proxy serves the expected hostname over HTTPS.
- Port 8080 is not publicly exposed.
- TLS certificate is valid for the hostname.
- Reverse proxy forwards the original Host and HTTPS scheme correctly.

## Downstream checks

- Confirm each required downstream reaches healthy/ready state.
- A broken optional downstream should be reported by name without exposing its credentials.
- For stdio processes, inspect command availability as the Hub service user, not only as root/admin.
- For environment-backed secrets, verify presence by variable name only; never print values.

## Client checks

- Verify `status`/`doctor` from the client machine before editing the client config.
- For stdio bridge, start the command in a controlled test and confirm it stays connected without emitting secret-bearing diagnostics.
- If available, use `mcp-hub export` to generate the exact client entry.

## Rollback

If an upgrade/change breaks a previously healthy deployment:

1. Save logs and the failing config/binary version information.
2. Stop the failing service.
3. Restore the last known-good binary and config backup.
4. Restore service/reverse-proxy file only if that file changed.
5. Reload the service manager/proxy as needed.
6. Start and run the full health gates again.
7. Report the failed change and the restored version.

Never delete the failed artifacts/logs before the cause is understood unless they contain exposed credentials; rotate any credential that was actually leaked.
