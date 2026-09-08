# VPS 与公网部署

MCP Hub 推荐部署为：

```text
Internet -> Caddy/Nginx HTTPS -> 127.0.0.1:8080 MCP Hub
```

Hub 本身保持监听回环地址，由反向代理终止 TLS。管理 stdio downstream 等同于允许远程触发受信进程，因此服务应运行在专用非 root 用户下。

## 1. 准备两个独立 Token

```bash
export MCP_HUB_TOKEN='给 MCP 客户端使用的 Token'
export MCP_HUB_ADMIN_TOKEN='只给管理面使用的不同 Token'
```

建议使用高熵随机值。公网模式要求 MCP Token 和 Admin Token 满足最小长度策略且不能相同。

不要把真实 Token 提交到 JSON、Git、Issue、截图或 PR。配置文件使用 `${...}` 引用，真实值由 systemd/launchd/服务环境提供。

## 2. 公网基线配置

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

先验证：

```bash
mcp-hub validate --config /etc/mcp-hub/config.json
```

`publicMode` 会强制公网 HTTPS 语义。只有来自 `trustedProxies` 的连接才允许使用 `X-Forwarded-Proto: https` 证明 TLS 已在代理层终止。不要把互联网网段加入可信代理。

## 3. 推荐文件布局

Linux 服务器可使用：

```text
/usr/local/bin/mcp-hub
/etc/mcp-hub/config.json
/etc/mcp-hub/mcp-hub.env
/var/lib/mcp-hub/
/etc/systemd/system/mcp-hub.service
```

建议：

- service user：`mcp-hub`；
- config/env 权限：0600；
- binary 由 root 安装但由非 root service user 运行；
- 不把 Token 写到 systemd `ExecStart` 参数。

## 4. systemd 示例

```ini
[Unit]
Description=MCP Hub
After=network-online.target
Wants=network-online.target

[Service]
User=mcp-hub
Group=mcp-hub
EnvironmentFile=/etc/mcp-hub/mcp-hub.env
ExecStart=/usr/local/bin/mcp-hub serve --config /etc/mcp-hub/config.json
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

不要在不了解 downstream 文件访问需求时盲目增加过强的 `ProtectHome`/`ProtectSystem`，否则可能让本来正常的 stdio MCP 无法读取其所需路径。加固策略应结合实际 downstream 权限模型验证。

## 5. Caddy 示例

```caddyfile
mcp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy 默认保留原始 Host，并传递正确的代理协议 Header。

主要地址：

- MCP：`https://mcp.example.com/mcp`
- Admin：`https://mcp.example.com/admin/`
- 状态基址：`https://mcp.example.com`

Nginx 也可以使用，但必须保留正确 Host，并确保只有受信代理路径产生用于 HTTPS 判断的 forwarded proto。

## 6. MCP 客户端

原生 HTTP MCP 客户端连接：

```text
https://mcp.example.com/mcp
```

认证：

```http
Authorization: Bearer <MCP_HUB_TOKEN>
```

仅支持 stdio 的客户端使用 bridge：

```bash
MCP_HUB_TOKEN='...' mcp-hub stdio --connect https://mcp.example.com/mcp
```

优先使用环境变量或客户端 Secret 存储，不要把 Token 固化到可公开同步的配置中。

## 7. 远程诊断

```bash
MCP_HUB_TOKEN='...' mcp-hub status --endpoint https://mcp.example.com
MCP_HUB_TOKEN='...' mcp-hub doctor --endpoint https://mcp.example.com
```

`status` / `doctor` 使用 MCP Token，不使用 Admin Token。

## 8. 远程管理 downstream

客户端不需要 SSH 到 VPS 才能增删改下游 MCP。

```bash
export MCP_HUB_ADMIN_TOKEN='<ADMIN_TOKEN>'

mcp-hub admin list --endpoint https://mcp.example.com
mcp-hub admin get filesystem --endpoint https://mcp.example.com
mcp-hub admin add filesystem --file ./filesystem.json --endpoint https://mcp.example.com
mcp-hub admin edit filesystem --file ./filesystem.json --endpoint https://mcp.example.com
mcp-hub admin delete filesystem --endpoint https://mcp.example.com --yes
```

这些命令复用 Browser Admin 的 session、CSRF、ETag/CAS、strict validation、preflight、原子持久化、reload 与 rollback 路径。

详细格式、安全语义和故障排查见 [REMOTE-ADMIN.md](REMOTE-ADMIN.md)。

## 9. Admin 安全机制

- Admin Token 与 MCP Token 分离；
- login/API 有速率限制；
- 随机 server-side session；
- `HttpOnly` / `Secure` / `SameSite=Strict` Cookie；
- exact same-origin + CSRF；
- JSON-only mutation；
- ETag/CAS；
- connection-changing edit preflight；
- OS-backed config lock；
- 原子写入；
- Secret placeholder，不回显现有 Secret；
- CSP / frame deny / nosniff / Permissions-Policy。

不要直接公开 Admin API 给不受信自动化；能执行 Admin 写入的主体实际上拥有修改 downstream 执行面的能力。

## 10. 防火墙

公网只开放：

- 80/tcp（如果用于 ACME/跳转）；
- 443/tcp。

不要公开 8080。Hub 应只监听 loopback。

SSH 端口按你自己的运维策略限制来源；它与 MCP Hub 本身是独立安全面。

## 11. 更新 / 升级

优先从正式 GitHub Release 安装固定版本并校验 `SHA256SUMS`。

如果尚未发布正式 Release，而任务要求验证 `main`，必须明确标记为 source/RC build。

升级顺序：

1. 记录当前版本；
2. 备份 binary、config、env、service、proxy 配置；
3. 下载并校验新 binary，或构建精确 commit；
4. 用新 binary `validate` 现有 config；
5. 原子替换 binary；
6. restart；
7. 检查 systemd 状态；
8. 运行 `status` / `doctor`；
9. 检查 `/admin/`；
10. 失败则回滚 binary/config/service 并保留日志证据。

## 12. 发布版本说明

源码当前身份为 `0.4.0`，但在仓库出现正式 `v0.4.0` Tag 和 GitHub Release 之前，应视为 **v0.4.0 release candidate**。

不要把未发布的 main build 伪装成已发布稳定版本。

## 13. 上线检查清单

- [ ] Hub 运行用户不是 root
- [ ] Hub 只监听 loopback
- [ ] 8080 未公网开放
- [ ] HTTPS 正常
- [ ] Host 与 `publicUrl` 一致
- [ ] `trustedProxies` 仅包含真实代理
- [ ] MCP/Admin Token 独立且强度足够
- [ ] config/env 权限受限
- [ ] `mcp-hub validate` 通过
- [ ] `status` 通过
- [ ] `doctor` 无关键错误
- [ ] `/admin/` 可登录
- [ ] downstream 只包含受信命令/服务
- [ ] 备份和回滚路径已记录

更多边界见 [SECURITY.md](SECURITY.md)。
