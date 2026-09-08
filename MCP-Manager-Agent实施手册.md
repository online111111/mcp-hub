# MCP Manager：Agent 维护与实施手册

本文面向编码 Agent、运维 Agent 和人工维护者，描述当前 v0.4.0 release candidate 的维护、验证、部署、兼容升级和发布规则。

## 先读

1. `README.md` / `README.zh-CN.md`
2. `MCP-Manager-开发设计文档.md`
3. `docs/CONFIG.md`
4. `docs/SECURITY.md`
5. `docs/COMPATIBILITY.md`
6. `docs/IMPLEMENTATION-STATUS.md`
7. 涉及 Remote Admin：`docs/REMOTE-ADMIN.md`
8. 涉及部署：`docs/VPS.md`
9. 涉及 SDK：`verification/sdkprobe/README.md`

当前基线：Go 1.25+、MCP Go SDK v1.7.0、产品名 MCP Manager、正式 CLI `mcp-manager`、Go module `github.com/online111111/mcp-manager`、单 Go 二进制、Browser Admin + Remote Admin + `mcp-manager-deployer` Skill。

## 接手规则

- 先核对最新 main/PR/CI，不从旧摘要直接假设。
- 先复现，再修；缺陷优先加回归。
- 不放宽断言或安全检查来“让 CI 变绿”。
- 不把文档 PASS 当实际执行证据。
- 不因 SDK v1.7.0 自动声称完整 MCP 2026-07-28 支持。
- MCP Manager 不是沙箱；stdio downstream 是受信代码。
- 不在日志、PR、报告中输出 Secret。
- 正常 downstream CRUD 优先走 Browser/Remote Admin，不绕过 CAS/preflight。
- 公网部署默认 loopback + HTTPS reverse proxy + 非 root 服务用户。

## v0.4 兼容升级规则

当前仓库、Go module、正式 CLI/二进制、Release 资产和 Skill 均已统一为 MCP Manager：

- 仓库：`online111111/mcp-manager`
- Go module：`github.com/online111111/mcp-manager`
- CLI/二进制：`mcp-manager`
- 新变量：`MCP_MANAGER_TOKEN`、`MCP_MANAGER_ADMIN_TOKEN`
- Skill：`mcp-manager-deployer`
- Release：`mcp-manager-*`

为避免已有测试/部署因升级立即中断，v0.4 仍兼容 `MCP_HUB_TOKEN` 与 `MCP_HUB_ADMIN_TOKEN`。配置 `hub.*`、`/mcp`、`/admin` 也是稳定兼容契约，不做品牌式机械重命名。

旧源码 CLI 入口已经移除；新代码不得重新引入旧仓库 slug、旧二进制名或旧公开产品名。CI 的 repository identity check 会阻止这些字符串重新进入当前代码树（历史环境变量名除外）。

不要为了视觉一致性强制改写已经工作的 systemd service 名或数据目录；运行时兼容优先，运维层路径可以在明确备份和验证后按需迁移。

## 主要目录

```text
cmd/mcp-manager/             正式 CLI 入口
internal/admin/              Admin API/UI/Agent 资源
internal/bridge/             stdio bridge
internal/cli/                CLI + Remote Admin
internal/config/             strict config/lock/atomic persistence
internal/downstream/         downstream MCP sessions
internal/inbound/            /mcp 与诊断入口
internal/process/            跨平台进程树
internal/runtime/            revision/reload/transaction
docs/                        当前规范
verification/sdkprobe/       独立 SDK 证据
```

## 修改核心行为时

运行时/downstream：检查 generation、lease、cancellation、no-replay、catalog revision、timeout/concurrency/backoff、shutdown drain。

stdio：检查 POSIX process group、Windows Job Object、safe environment、Manager/Admin Secret 不隐式继承、普通 command/args 不自动进 shell。

HTTP/MCP：检查 HTTPS、redirect、Header、Host/Origin、body/response bound、session deadline、错误脱敏、兼容协议边界。

Admin：必须保留 auth/session、same-origin/CSRF、ETag/CAS、strict JSON、1 MiB bound、preflight、OS lock、atomic persistence、reload、rollback。慢 body 必须在 config transaction lock 前读取。

UI：保持无外部运行时/CDN，覆盖移动端、keyboard focus、ARIA、reduced motion、editor-open ETag、config+ETag 一致发布。

Remote Admin：保持 MCP/Admin Token 分离、远程 HTTPS、redirect reject、session+CSRF、ETag/CAS、redaction、bounded input/output。

## 验证

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

SDK probe：

```bash
cd verification/sdkprobe
go test -race -count=1 -timeout 120s ./...
```

Browser：

```bash
cd internal/admin/browsertest
npm ci
npx playwright install --with-deps chromium
npm test
cd ../../..
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node internal/admin/browsertest/live-smoke.mjs
```

## 部署/升级

1. inventory OS/arch/现有版本/服务/代理；
2. 备份 binary/config/env/service/proxy；
3. 获取固定 Release + `SHA256SUMS` 或构建精确 commit；
4. 用新二进制 validate 现有 config；
5. 原子替换或旁路暂存；
6. restart；
7. `status` / `doctor`；
8. 检查 `/admin/` 和代表性客户端；
9. 失败回滚；
10. 报告版本、路径、endpoint、备份位置，不输出 Secret。

## Skill

内置 Skill：`internal/admin/agent-skill/`，公开名 `mcp-manager-deployer`。修改 Skill 时必须验证完整文件结构、脚本和 `skill.zip` 打包结果，确保管理台下载资源与当前二进制一致。

## PR / Release

建议短分支、单主题、Draft -> final-head CI -> Ready；合并时核对 expected head。堆叠 PR 按依赖顺序处理，每次合并后重新核对后续 base/diff。

正式 v0.4.0 前：final main CI green、文档与 final head 一致、依赖 PR 有明确决定、仓库身份与 module path 一致、精确 `v0.4.0` tag、Release workflow 成功、六套 `mcp-manager-*` 包和 `SHA256SUMS` 齐全。

最终汇报使用“在当前审计和已执行验证范围内，没有发现已知 release-blocking 问题”，不要声称绝对无 bug。
