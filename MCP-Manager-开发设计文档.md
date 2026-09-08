# MCP Manager 开发设计文档

- 文档版本：0.4
- 产品阶段：v0.4.0 release candidate
- 当前实现：Go 1.25+，MCP Go SDK v1.7.0
- 产品定位：轻量、自托管的 MCP 网关、下游服务管理器与 Admin 控制台

本文描述当前 **MCP Manager** 实现。产品原名 MCP Hub；旧名称只在兼容配置、内部 module path 和历史证据中保留。

## 产品目标

1. 下游 MCP 服务只维护一次，多客户端复用同一 Manager。
2. 支持 stdio 与远程 Streamable HTTP downstream。
3. 同时服务原生 HTTP MCP 客户端和 stdio-only 客户端。
4. 配置修改具备严格验证、CAS、preflight、原子写入和 rollback。
5. 公网部署默认 fail-closed。
6. Browser Admin、Remote Admin CLI、Agent Skill 复用一套管理事务。
7. 维持单 Go 二进制生产形态。

## 当前能力

已实现：严格配置、stdio/HTTP downstream、工具发现/发布/路由、取消/超时/并发/backoff、跨平台进程树管理、热重载、Admin UI、Remote Admin CRUD、stdio bridge、status/doctor、公网安全策略、`mcp-manager-deployer` Skill、跨平台 CI 与 Release workflow。

不宣称：通用 resources/prompts/sampling/roots/elicitation/task 扩展代理、完整原生 MCP 2026-07-28 覆盖、多租户/RBAC/计费、沙箱、所有第三方客户端/服务的兼容认证、自动重放可能产生副作用的工具调用。

## 产品改名契约

v0.4 改名会改变：

- 产品名：MCP Manager
- 正式 CLI/二进制：`mcp-manager`
- 新环境变量：`MCP_MANAGER_TOKEN`、`MCP_MANAGER_ADMIN_TOKEN`
- Release 资产前缀：`mcp-manager-`
- Skill：`mcp-manager-deployer`

不会改变：

- `/mcp`、`/admin/`、`/api/...`
- JSON `hub.*` schema
- 下游运行时/路由语义
- Admin 安全事务语义
- 现有配置中的旧环境变量引用

兼容期 CLI 仍接受 `MCP_HUB_TOKEN` / `MCP_HUB_ADMIN_TOKEN`。内部 Go module path 在 v0.4 暂时保持 `mcp-hub`，将内部 import 重写与产品改名风险隔离。

## 核心架构

```text
HTTP client -------------------------------+
                                            |
stdio client -> mcp-manager stdio ---------+--> /mcp -> Publisher -> Catalog -> Router
                                                                      |
                                                                      v
                                                               Downstream Manager
                                                                /           \
                                                             stdio     Streamable HTTP
```

管理面：

```text
Browser /admin/ ------+
                       |
mcp-manager admin -----+--> Admin API -> auth/session/CSRF -> ETag/CAS
                       |                                -> validate/preflight
Agent/Skill -----------+                                -> atomic persist/reload
                                                        -> rollback on failure
```

磁盘 JSON 仍是持久化事实来源。

## Downstream 与调用语义

stdio 普通 command/args 不做隐式 shell 插值；子进程只继承安全兼容环境基线，额外 Secret 需要显式 `env`。POSIX 使用 process group，Windows 使用 worker + Job Object。

远程非回环 Streamable HTTP 必须 HTTPS，禁止 redirect，自定义 Header 不得覆盖 MCP/session framing Header。

工具调用使用明确 public-name -> downstream/original-tool 路由，不猜测目的地，不自动重放失败调用。

## Reload 与 revision

连接级 downstream 修改采用 generation replacement；已 admission 的调用继续持有原 generation lease。listener/auth/Admin 等 startup-bound 修改标记 `restartRequired`，只有进程重启后才算完全应用。

## 管理面安全

Browser/CLI Admin 共同使用：Admin 认证、server-side session、CSRF、strict JSON、Secret placeholder、ETag/CAS、preflight、配置锁、原子持久化、runtime reload、rollback。

Admin PUT 先读取并限制网络 body，再获取共享 config transaction lock，避免慢上传阻塞 runtime polling。

## 公网部署

推荐：

```text
Internet -> HTTPS reverse proxy -> 127.0.0.1:8080 MCP Manager
```

只公网开放必要 80/443；不要公开 8080。MCP/Admin Token 分离，Manager 使用专用非 root 账户。

## 验证

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

CI 覆盖 Linux/Windows/macOS、Go 1.25.8/1.27.1、race、SDK probe、Chromium、real-Manager smoke、govulncheck。

## Release

正式 `v0.4.0` tag 将构建 Linux/Windows/macOS × amd64/arm64 六套 `mcp-manager-*` 资产和 `SHA256SUMS`。在 tag + GitHub Release 实际生成前仍称 release candidate。

## 维护事实来源

- `README.md` / `README.zh-CN.md`
- `docs/ARCHITECTURE.md`
- `docs/CONFIG.md`
- `docs/SECURITY.md`
- `docs/COMPATIBILITY.md`
- `docs/REMOTE-ADMIN.md`
- `docs/IMPLEMENTATION-STATUS.md`
