# MCP Manager VPS / 公网部署

推荐拓扑：

```text
Internet -> Caddy/Nginx HTTPS -> 127.0.0.1:8080 MCP Manager
```

Manager 保持监听回环地址，由反向代理终止 TLS。stdio downstream 等同于允许服务用户执行受信进程，因此持久部署应使用专用非 root 用户。

## 1. Token

新部署使用：

```bash
export MCP_MANAGER_TOKEN='给 MCP 客户端使用的 Token'
export MCP_MANAGER_ADMIN_TOKEN='只给管理面使用的不同 Token'
```

两枚 Token 应高熵、不同，并满足至少 32 字符的策略。已有旧版部署可在 v0.4 兼容期继续使用 `MCP_HUB_TOKEN` / `MCP_HUB_ADMIN_TOKEN`，不要仅为升级自动旋转凭据。

## 2. 公网配置

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

`hub.*` 字段继续保留，这是配置兼容契约。

验证：

```bash
mcp-manager validate --config /etc/mcp-manager/config.json
```

## 3. 新部署文件布局

```text
/usr/local/bin/mcp-manager
/etc/mcp-manager/config.json
/etc/mcp-manager/mcp-manager.env
/var/lib/mcp-manager/
/etc/systemd/system/mcp-manager.service
```

已有旧目录或旧 service 名可以在兼容升级期继续使用；运维路径改名不是运行时必须条件。

## 4. systemd 示例

```ini
[Unit]
Description=MCP Manager
After=network-online.target
Wants=network-online.target

[Service]
User=mcp-manager
Group=mcp-manager
EnvironmentFile=/etc/mcp-manager/mcp-manager.env
ExecStart=/usr/local/bin/mcp-manager serve --config /etc/mcp-manager/config.json
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

配置和环境文件建议 0600。不要把 Token 放进 `ExecStart` 参数。

## 5. Caddy

```caddyfile
mcp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

主要地址：

- MCP: `https://mcp.example.com/mcp`
- Admin: `https://mcp.example.com/admin/`
- diagnostics base: `https://mcp.example.com`

只允许真实代理地址进入 `trustedProxies`。

## 6. 客户端

stdio bridge：

```bash
MCP_MANAGER_TOKEN='...' mcp-manager stdio --connect https://mcp.example.com/mcp
```

诊断：

```bash
MCP_MANAGER_TOKEN='...' mcp-manager status --endpoint https://mcp.example.com
MCP_MANAGER_TOKEN='...' mcp-manager doctor --endpoint https://mcp.example.com
```

Remote Admin：

```bash
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin list --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin add filesystem --file ./filesystem.json --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin edit filesystem --file ./filesystem.json --endpoint https://mcp.example.com
MCP_MANAGER_ADMIN_TOKEN='...' mcp-manager admin delete filesystem --endpoint https://mcp.example.com --yes
```

详见 [REMOTE-ADMIN.md](REMOTE-ADMIN.md)。

## 7. 防火墙与安全

- 公网只开放必要的 80/443；不要公开 8080。
- Manager 运行用户不是 root。
- MCP/Admin Token 分离。
- Admin 使用 session、CSRF、same-origin、ETag/CAS、preflight、原子写入和 rollback。
- 只配置受信 stdio 命令和远程 MCP 服务。
- 不要把 Secret 放进 Git、PR、截图或命令行参数。

## 8. 旧部署原地升级

1. 记录现有版本、二进制路径、config/env/service/proxy 状态。
2. 备份所有这些文件。
3. 将新 `mcp-manager` 二进制暂存到旁路路径。
4. 用新二进制验证原配置：`mcp-manager validate --config <old-config>`。
5. 保持原 `hub.*` schema、`/mcp`、`/admin` 路径不变。
6. `MCP_HUB_*` 环境变量可以在兼容期暂时保留；新服务文件优先使用 `MCP_MANAGER_*`。
7. 验证通过后再修改 systemd `ExecStart`。
8. restart 后跑 `status`、`doctor`、Admin 登录和至少一个真实客户端检查。
9. 全部健康后再删除旧二进制；失败立即回滚。

## 9. Release 安装与升级

正式 v0.4 Release 资产使用：

```text
mcp-manager-<version>-<os>-<arch>.<tar.gz|zip>
SHA256SUMS
```

内置 `mcp-manager-deployer` Skill 的安装脚本直接使用规范仓库 `online111111/mcp-manager`，不再依赖旧仓库 slug 回退。

在正式 `v0.4.0` tag/Release 出现前，`main`/PR 构建仍应标注为 release candidate/source build。

## 10. 上线检查

- [ ] 非 root 服务用户
- [ ] 只监听 loopback
- [ ] 8080 未公网开放
- [ ] HTTPS 正常
- [ ] `publicUrl`/Host/trusted proxy 正确
- [ ] MCP/Admin Token 独立
- [ ] config/env 权限受限
- [ ] `mcp-manager validate` 通过
- [ ] `status` 通过
- [ ] `doctor` 通过
- [ ] `/admin/` 可登录
- [ ] downstream 均为受信服务
- [ ] 备份和回滚路径已记录

更多边界见 [SECURITY.md](SECURITY.md)。
