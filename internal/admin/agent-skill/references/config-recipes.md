# Configuration recipes

MCP Hub uses strict JSON. Keep `version: 1` and run `mcp-hub validate` after every generated change.

## Local/private baseline

```json
{
  "version": 1,
  "hub": { "listen": "127.0.0.1:8080" },
  "defaults": {
    "startupTimeout": "20s",
    "callTimeout": "60s",
    "maxConcurrency": 8
  },
  "mcpServers": {}
}
```

## Public VPS baseline

```json
{
  "version": 1,
  "hub": {
    "listen": "127.0.0.1:8080",
    "publicMode": true,
    "publicUrl": "https://mcp.example.com",
    "allowedHosts": ["mcp.example.com"],
    "trustedProxies": ["127.0.0.1/32", "::1/128"],
    "auth": { "bearerToken": "${MCP_HUB_TOKEN}" },
    "admin": {
      "enabled": true,
      "token": "${MCP_HUB_ADMIN_TOKEN}",
      "sessionTimeout": "30m"
    }
  },
  "defaults": {
    "startupTimeout": "20s",
    "callTimeout": "60s",
    "maxConcurrency": 8
  },
  "mcpServers": {}
}
```

Public requirements:

- HTTPS `publicUrl`
- expected hostname only in `allowedHosts`
- trusted proxy CIDRs only for the real proxy hop
- MCP/Admin tokens distinct and at least 32 characters after environment expansion

## stdio downstream

```json
"memory": {
  "enabled": true,
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-memory"],
  "env": {},
  "tools": { "disabled": [] }
}
```

For required credentials, explicitly opt in:

```json
"env": {
  "GITHUB_TOKEN": "${GITHUB_TOKEN}"
}
```

Hub intentionally does not pass arbitrary ambient credentials to stdio children.

## Remote Streamable HTTP downstream

```json
"remote-tools": {
  "enabled": true,
  "type": "streamable_http",
  "url": "https://provider.example/mcp",
  "headers": {
    "Authorization": "Bearer ${REMOTE_TOKEN}"
  },
  "tools": { "disabled": [] }
}
```

Remote downstreams require HTTPS. Plain HTTP is only acceptable for literal loopback targets. Do not put URL credentials in userinfo/query strings.

## Safe edit procedure

1. Read and preserve existing JSON.
2. Modify the smallest intended subtree.
3. Preserve unrelated downstreams and secret `${ENV}` references.
4. Write to a temporary/staged file when operating outside the Admin API.
5. Run `mcp-hub validate --config <staged>`.
6. Back up the live config.
7. Atomically replace when possible.
8. Verify runtime after reload/restart.

Connection-changing Admin saves have runtime preflight. CLI `validate` performs strict decode/resolution but does not start downstream processes or make downstream network calls.
