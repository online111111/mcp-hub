# VPS 与公网管理台部署

MCP Hub 可以通过内嵌管理页面部署到 VPS。推荐拓扑是 **Caddy/Nginx 终止 HTTPS，Hub 仍监听 127.0.0.1**。管理配置 stdio 服务等同于远程执行进程，务必让 Hub 运行在专用非 root 用户下。

## 1. 环境变量

生成两个独立的高强度随机 Token（建议至少 32 随机字节）：

```bash
export MCP_HUB_TOKEN='给 MCP 客户端使用的 Token'
export MCP_HUB_ADMIN_TOKEN='只给管理员登录使用的不同 Token'
```

不要把 Token 直接提交到 JSON 或 Git。使用 `config.vps.example.json` 中的 `${...}` 引用。

## 2. 配置

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
  "mcpServers": {}
}
```

`publicMode` 会强制要求 HTTPS。只有来自 `trustedProxies` 的连接才能使用 `X-Forwarded-Proto: https` 证明 TLS 已由代理终止。不要把公网网段加入可信代理。

## 3. Caddy 示例

```caddyfile
mcp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy 默认传递原始 Host，并设置 `X-Forwarded-Proto`。访问：

- 管理页面：`https://mcp.example.com/admin/`
- MCP 地址：`https://mcp.example.com/mcp`

## 4. MCP 客户端认证

直接 HTTP 客户端发送：

```http
Authorization: Bearer <MCP_HUB_TOKEN>
```

stdio bridge：

```bash
MCP_HUB_TOKEN='...' mcp-hub stdio --connect https://mcp.example.com/mcp
```

也可使用 `--token`，但命令行参数可能被同机用户的进程列表看到，优先使用环境变量。

## 5. 管理台安全机制

- 管理 Token 与 MCP Token 分离；
- 登录尝试按可信来源 IP 限制为每 15 分钟 5 次；
- 随机会话和 CSRF Token；
- `HttpOnly`、`Secure`、`SameSite=Strict` Cookie；
- 严格同源验证和 JSON-only 变更请求；
- 最多 16 个管理会话；
- 配置写入使用 SHA-256 ETag/CAS、锁文件和原子替换；
- Header、环境变量和 Hub Token 在浏览器中只显示占位符，原值不会回显；
- CSP、禁止 iframe、nosniff、Permissions-Policy。

## 6. 防火墙和权限

- 只对公网开放 80/443；不要开放 Hub 的 8080 端口；
- Hub、Caddy 使用不同的非 root 服务账户；
- 限制配置文件和环境文件权限为 0600；
- stdio MCP 只允许执行你信任的命令；
- 定期更新 Go 工具链并运行 `govulncheck`。
