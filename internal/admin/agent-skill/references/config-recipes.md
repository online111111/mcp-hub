# MCP Manager configuration recipes

The JSON schema keeps the historical top-level `hub` object during the product rename. Renaming the product does not require rewriting that schema or the `/mcp` and `/admin` endpoint paths.

## Local-only Manager

```json
{
  "version": 1,
  "hub": { "listen": "127.0.0.1:8080" },
  "mcpServers": {}
}
```

## Public Manager behind HTTPS reverse proxy

```json
{
  "version": 1,
  "hub": {
    "listen": "127.0.0.1:8080",
    "publicMode": true,
    "publicUrl": "https://mcp.example.com",
    "allowedHosts": ["mcp.example.com"],
    "trustedProxies": ["127.0.0.1/32", "::1/128"],
    "auth": { "bearerToken": "${MCP_MANAGER_TOKEN}" },
    "admin": {
      "enabled": true,
      "token": "${MCP_MANAGER_ADMIN_TOKEN}",
      "sessionTimeout": "30m"
    }
  },
  "mcpServers": {}
}
```

Existing deployments may keep `${MCP_HUB_TOKEN}` and `${MCP_HUB_ADMIN_TOKEN}` while migrating. Do not silently rotate credentials just to adopt the new product name.

## stdio downstream

```json
{
  "enabled": true,
  "type": "stdio",
  "command": "/usr/bin/node",
  "args": ["/opt/tool/server.js"],
  "cwd": "/opt/tool",
  "env": {
    "GITHUB_TOKEN": "${GITHUB_TOKEN}"
  }
}
```

Use explicit `env` entries for credentials; ambient credentials are intentionally not inherited by default.

## Streamable HTTP downstream

```json
{
  "enabled": true,
  "type": "streamable_http",
  "url": "https://tools.example.com/mcp",
  "headers": {
    "Authorization": "Bearer ${REMOTE_TOKEN}"
  }
}
```

Remote non-loopback HTTP must use HTTPS. Redirects are rejected to avoid credential forwarding.

## Remote Admin file

`mcp-manager admin add/edit --file` expects exactly one server object, not the full root configuration:

```json
{
  "enabled": true,
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/srv/data"]
}
```
