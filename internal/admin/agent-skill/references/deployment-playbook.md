# Deployment playbook

## 1. Preflight inventory

Collect only what materially affects deployment:

- OS and version; `uname -m` / Windows architecture
- whether the agent can execute locally or over SSH
- current user and elevation method
- existing `mcp-hub` path and `mcp-hub version`/help output if installed
- target mode: server, local, HTTP client, stdio bridge, upgrade, repair, uninstall
- for server: service manager, desired listen address, domain/public access, reverse proxy, firewall
- existing config/service/reverse-proxy files

Avoid asking the user questions that inspection can answer.

## 2. Install binary

Preferred:

```bash
python3 <skill-dir>/scripts/install_release.py
```

Useful options:

```bash
python3 <skill-dir>/scripts/install_release.py --version 0.4.0 --install-dir /usr/local/bin
python3 <skill-dir>/scripts/install_release.py --dry-run
```

The script downloads the matching release archive plus `SHA256SUMS`, verifies SHA-256, safely extracts `mcp-hub`, and installs it. If root is required for the chosen directory, either run through the available elevation mechanism or install into a user-writable directory and use that exact path in service/client configuration.

If Python 3 is unavailable, download the exact asset `mcp-hub-<version>-<linux|windows|darwin>-<amd64|arm64>.(tar.gz|zip)` and verify it against release `SHA256SUMS` before extracting.

If there is no suitable stable Release, source build fallback:

```bash
git clone https://github.com/online111111/mcp-hub.git
cd mcp-hub
git checkout <requested-ref>
go test ./...
go build -trimpath -o mcp-hub ./cmd/mcp-hub
```

Do not call an untagged build a stable release.

## 3. Linux persistent server baseline

Suggested paths:

- binary: `/usr/local/bin/mcp-hub`
- config: `/etc/mcp-hub/config.json`
- secrets: `/etc/mcp-hub/mcp-hub.env`
- service user: `mcp-hub`
- working/data directory: `/var/lib/mcp-hub`
- systemd unit: `/etc/systemd/system/mcp-hub.service`

Generate secrets without logging them. Example:

```bash
python3 - <<'PY'
import secrets
print('MCP_HUB_TOKEN=' + secrets.token_urlsafe(32))
print('MCP_HUB_ADMIN_TOKEN=' + secrets.token_urlsafe(32))
PY
```

Write the environment file with mode 0600. Never commit it.

A conservative service unit:

```ini
[Unit]
Description=MCP Hub
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mcp-hub
Group=mcp-hub
WorkingDirectory=/var/lib/mcp-hub
EnvironmentFile=/etc/mcp-hub/mcp-hub.env
ExecStart=/usr/local/bin/mcp-hub serve --config /etc/mcp-hub/config.json
Restart=on-failure
RestartSec=2s
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

Do not add aggressive `ProtectSystem`, `ProtectHome`, or namespace restrictions until the configured stdio downstreams' filesystem/runtime requirements are known.

Apply:

```bash
mcp-hub validate --config /etc/mcp-hub/config.json
systemctl daemon-reload
systemctl enable --now mcp-hub
systemctl --no-pager --full status mcp-hub
```

## 4. Public server

First prove `http://127.0.0.1:8080` works locally. Then add TLS reverse proxy.

Caddy baseline:

```caddyfile
mcp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Back up an existing Caddy/Nginx config before editing. Validate proxy syntax before reload. Keep firewall exposure to 80/443; do not publish 8080.

Endpoints:

- MCP: `https://mcp.example.com/mcp`
- Admin: `https://mcp.example.com/admin/`

## 5. Local all-in-one

Use a user-owned directory and loopback config. Minimal operation:

```bash
mcp-hub validate --config ./config.json
mcp-hub serve --config ./config.json
```

Default endpoint: `http://127.0.0.1:8080/mcp`.

For persistence use the native OS service mechanism only when requested. Do not force system-wide installation for a developer-only local Hub.

## 6. Client connection

HTTP-capable client: configure `https://host/mcp` and bearer auth directly.

stdio-only client:

```bash
MCP_HUB_TOKEN='...' mcp-hub stdio --connect https://host/mcp
```

If the client name is known, inspect `mcp-hub export --help` and prefer generated config. Keep secrets outside checked-in client config where supported.

## 7. Remote downstream administration

When a client machine has the MCP Hub binary and Admin credentials, manage the server-side `mcpServers` collection through the built-in remote Admin CLI rather than editing the server filesystem directly:

```bash
export MCP_HUB_ADMIN_TOKEN='<ADMIN_TOKEN>'
mcp-hub admin list --endpoint https://host
mcp-hub admin get <id> --endpoint https://host
mcp-hub admin add <id> --file ./server.json --endpoint https://host
mcp-hub admin edit <id> --file ./server.json --endpoint https://host
mcp-hub admin delete <id> --endpoint https://host --yes
```

`server.json` is one `ServerConfig` object, not the whole Hub configuration. Retrieved secrets are redacted; editing an existing service may preserve those redacted secret placeholders through the server-side merge logic. Deletion is intentionally confirmation-gated. Use HTTPS for remote hosts.

## 8. Upgrade

- Never overwrite the only working binary without a backup.
- Validate existing config with the new binary before restart.
- Preserve environment files and tokens unless rotation was explicitly requested.
- After restart, verify both local service health and the externally routed endpoint.

## 9. Windows/macOS notes

- Release OS names are `windows` and `darwin`; architectures are `amd64` and `arm64`.
- Windows release archives contain `mcp-hub.exe`; macOS/Linux contain `mcp-hub`.
- For a client-only bridge, a user-local install is preferred over a system service.
- On Windows, keep tokens in process/user secret environment or the client application's secret facility rather than embedding them in a shared JSON file.
