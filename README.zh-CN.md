# MCP Manager

[English](README.md) | **简体中文**

**MCP Manager** 是一个轻量、自托管的 MCP 网关与管理控制台。下游 MCP 服务只配置一次，即可通过统一的 Streamable HTTP 入口提供给 IDE、Agent 和仅支持 stdio 的客户端。

> 改名说明：MCP Manager 是原 **MCP Hub** 的新产品名。v0.4.0 发布候选版保留现有 JSON 配置结构以及 `/mcp`、`/admin` 路径，现有部署不需要因为改名重写配置。

## 核心能力

- 统一 `/mcp` 入口与动态工具目录。
- 管理本地 stdio 子进程和远程 Streamable HTTP 下游。
- 响应式 `/admin/` 管理台：运行状态、调用记录、服务增删改、客户端接入、Agent 部署资源。
- Remote Admin CLI：无需 SSH 手改服务器配置即可 `list/get/add/edit/delete` 下游服务。
- 严格且有大小上限的 JSON 配置，ETag/CAS 并发保护、原子持久化、预检、回滚和热重载。
- 稳定的公开工具名，调用失败后不自动重放可能带副作用的请求。
- 默认回环安全模式，以及显式认证的 HTTPS 公网模式。
- 为只支持 stdio 的客户端提供桥接。
- 内置 `mcp-manager-deployer` Skill 和通用 Agent Prompt，用于部署、改名迁移、升级、验证和恢复。

生产模块当前使用 `github.com/modelcontextprotocol/go-sdk v1.7.0`。这**不代表** MCP Manager 宣称完整原生覆盖 MCP 2026-07-28 的所有能力；具体边界见[兼容性说明](docs/COMPATIBILITY.md)。

## 快速开始

从源码构建需要 Go 1.25 或更新版本：

```bash
go build -trimpath -o mcp-manager ./cmd/mcp-manager
cp config.example.json config.json
./mcp-manager validate --config ./config.json
./mcp-manager serve --config ./config.json
```

默认 MCP 地址：

```text
http://127.0.0.1:8080/mcp
```

诊断：

```bash
./mcp-manager status --endpoint http://127.0.0.1:8080
./mcp-manager doctor --endpoint http://127.0.0.1:8080
```

## 新旧环境变量

新安装统一使用：

```bash
MCP_MANAGER_TOKEN=...
MCP_MANAGER_ADMIN_TOKEN=...
```

改名兼容期内，CLI 仍接受原来的 `MCP_HUB_TOKEN` 和 `MCP_HUB_ADMIN_TOKEN`。新生成的示例和客户端导出只使用 `MCP_MANAGER_*`。已有部署不需要仅仅为了改名字就旋转 Token。

## 客户端接入

原生支持 Streamable HTTP 的客户端连接：

```text
https://mcp.example.com/mcp
```

仅支持 stdio 的客户端：

```bash
MCP_MANAGER_TOKEN='...' mcp-manager stdio --connect https://mcp.example.com/mcp
```

支持的客户端优先用 CLI 导出配置：

```bash
mcp-manager export --client cursor --transport stdio --endpoint https://mcp.example.com/mcp --token-env
```

## 远程管理下游服务

这里使用 Admin Token，不要和 MCP Bearer Token 混用：

```bash
export MCP_MANAGER_ADMIN_TOKEN='<ADMIN_TOKEN>'

mcp-manager admin list --endpoint https://mcp.example.com
mcp-manager admin get remote-tools --endpoint https://mcp.example.com
mcp-manager admin add filesystem --file ./filesystem.json --endpoint https://mcp.example.com
mcp-manager admin edit filesystem --file ./filesystem.json --endpoint https://mcp.example.com
mcp-manager admin delete filesystem --endpoint https://mcp.example.com --yes
```

远程变更仍然走和浏览器管理台相同的安全事务链：认证会话、CSRF、ETag/CAS、严格校验、下游预检、原子写入、失败回滚和热重载。详见 [Remote Admin](docs/REMOTE-ADMIN.md)。

## 配置

改名后仍保留历史 `hub` 配置对象，这是兼容契约，不应该机械改成 `manager`：

```json
{
  "version": 1,
  "hub": { "listen": "127.0.0.1:8080" },
  "defaults": {
    "startupTimeout": "20s",
    "callTimeout": "60s",
    "maxConcurrency": 8
  },
  "mcpServers": {
    "memory": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-memory"]
    }
  }
}
```

完整字段、严格解码、环境继承、热重载和 Admin 写入语义见[配置文档](docs/CONFIG.md)。

## 公网部署

推荐拓扑：

```text
Internet -> HTTPS Caddy/Nginx -> 127.0.0.1:8080 MCP Manager
```

公网模式需要 HTTPS `publicUrl`、明确的 `allowedHosts`、可信代理 CIDR，以及两枚不同且至少 32 字符的 MCP/Admin Token。不要把 8080 直接暴露到公网。详见 [VPS 部署](docs/VPS.md)和[安全边界](docs/SECURITY.md)。

## MCP Hub → MCP Manager 迁移

这次改名采用兼容迁移，不要求一次性“全盘换名”：

1. 备份旧二进制、配置、环境文件、systemd/服务配置和反向代理配置。
2. 安装或暂存新的 `mcp-manager` 二进制。
3. 用 `mcp-manager validate` 验证原有配置；配置 schema 和 `/mcp`、`/admin` 地址不变。
4. 原有 `MCP_HUB_*` 变量在兼容期内可以继续使用；新服务文件优先写 `MCP_MANAGER_*`。
5. 验证通过后再把服务启动命令从 `mcp-hub` 改成 `mcp-manager`。
6. 跑 `status`、`doctor`、Admin 登录和至少一条代表性客户端链路后，再删除旧二进制。

迁移期间 `cmd/mcp-hub` 旧源码入口仍由 CI 编译；正式 v0.4 Release 只发布名为 `mcp-manager` 的新二进制。

Go 内部 module path 在 v0.4 改名阶段暂时保持 `mcp-hub`，避免为了内部标识做一次高风险的全仓 import 重写。它不影响用户看到的产品名、二进制名或 Release 资产；module path 是否迁移可以等产品改名稳定后单独评估。

## 开发与验证

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
```

CI 覆盖 Linux、Windows、macOS 的兼容/当前 Go 矩阵；当前 Go 还跑 race 与独立 SDK probe，Linux 额外运行 Chromium 回归和真实 MCP Manager 浏览器 smoke，并设有 `govulncheck`。

## Release 包

正式 `v*` tag 会生成：

- Linux amd64/arm64 `.tar.gz`
- Windows amd64/arm64 `.zip`
- macOS amd64/arm64 `.tar.gz`
- `SHA256SUMS`

资产名类似：

```text
mcp-manager-0.4.0-linux-amd64.tar.gz
```

## 文档

- [配置](docs/CONFIG.md)
- [架构](docs/ARCHITECTURE.md)
- [安全边界](docs/SECURITY.md)
- [兼容性](docs/COMPATIBILITY.md)
- [Remote Admin](docs/REMOTE-ADMIN.md)
- [VPS 部署](docs/VPS.md)
- [实现状态](docs/IMPLEMENTATION-STATUS.md)
- [管理台验证](docs/CONSOLE-POLISH.md)

## 安全边界

MCP Manager 是可信个人网关，**不是沙箱**。stdio 下游会以 Manager 服务用户的权限运行。建议使用专用非 root 用户，并且只配置你信任的 MCP 服务。
