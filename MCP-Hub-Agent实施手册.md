# MCP Hub：Agent 维护与实施手册

本文面向接手 `online111111/mcp-hub` 的编码 Agent、运维 Agent 和人工维护者。它描述**当前 v0.4.0 release candidate** 的维护、验证、部署和变更规则，不再沿用早期“项目尚未实现 / SDK v1.4.1 / 无管理台”的旧假设。

## 0. 先读什么

开始工作前至少阅读：

1. `README.md` 或 `README.zh-CN.md`；
2. `MCP-Hub-开发设计文档.md`；
3. `docs/CONFIG.md`；
4. `docs/SECURITY.md`；
5. `docs/COMPATIBILITY.md`；
6. `docs/IMPLEMENTATION-STATUS.md`；
7. 涉及 SDK 时阅读 `verification/sdkprobe/README.md`；
8. 涉及远程管理时阅读 `docs/REMOTE-ADMIN.md`；
9. 涉及公网部署时阅读 `docs/VPS.md`。

当前关键基线：

- Go directive：1.25.0；
- CI：Go 1.25.8 / 1.27.1；
- MCP Go SDK：v1.7.0；
- 产品版本身份：0.4.0 RC；
- 生产形态：单 Go 二进制；
- 管理路径：Browser Admin + Remote Admin CLI + Agent Skill；
- 配置持久化事实来源：JSON 文件。

## 1. 接手规则

1. 先检查 GitHub `main` 最新 SHA、open PR、CI 和当前分支基线，不从旧摘要直接假设。
2. 先复现问题，再改代码；能写回归测试的缺陷优先写回归。
3. 不为了“测试变绿”放宽断言或删除安全检查。
4. 不把 README/状态表中的 PASS 当作执行结果；测试证据必须来自实际命令或 CI。
5. 不自动扩大 MCP 协议能力声明。SDK v1.7.0 不等于产品完整支持 MCP 2026-07-28。
6. 不把 Hub 当沙箱。stdio downstream 是受信代码，会获得 Hub 用户权限。
7. 不在日志、PR、最终报告中输出 Token、环境变量值或下游 Secret。
8. 不通过直接改配置文件旁路 Admin CAS/preflight，除非任务本身就是离线修复且明确说明风险。
9. 生产部署默认使用专用非 root 用户，公网 Hub 监听 loopback 并由 HTTPS reverse proxy 暴露。
10. 代码变更完成后同步相关文档，尤其是兼容性、安全边界、配置和实现状态。

## 2. 仓库结构

主要目录：

```text
cmd/mcp-hub/                 CLI 入口
internal/admin/              Admin API、UI、内置 Agent 资源
internal/bridge/             stdio -> Hub bridge
internal/catalog/            工具目录
internal/cli/                CLI 子命令，包括 Remote Admin
internal/config/             严格配置、锁、原子写入、解析
internal/downstream/         MCP downstream session
internal/inbound/            Hub HTTP /mcp 和诊断入口
internal/process/            stdio 进程树管理
internal/runtime/            reload / generation / runtime 协调
verification/sdkprobe/       独立 SDK 证据模块
docs/                        当前专题文档
```

## 3. 常见任务决策

### 修运行时 / downstream

检查：

- generation / lease 生命周期；
- cancellation 是否贯穿；
- 调用是否存在重放；
- tool catalog revision；
- timeout / concurrency / backoff；
- shutdown drain 顺序。

必须考虑 Linux、Windows、macOS CI 差异。

### 修改 stdio 进程行为

必须考虑：

- POSIX process group；
- Windows worker + Job Object；
- 子进程退出早于后代进程的情况；
- safe environment allowlist；
- Hub MCP/Admin Secret 不得隐式继承；
- 普通 command/args 不应自动经过 shell。

### 修改 HTTP / MCP transport

必须检查：

- HTTPS policy；
- redirect；
- Header 注入边界；
- Host / Origin；
- body / response size；
- session deadline；
- error sanitization；
- 2025-11-25 兼容语义与 SDK v1.7.0 行为。

### 修改 Admin 写入

必须保留：

- session auth；
- same-origin / CSRF；
- ETag/CAS；
- strict JSON；
- 1 MiB 边界；
- preflight；
- OS lock；
- atomic persistence；
- reload；
- rollback / last healthy state。

慢网络 Body 必须在获取共享 config transaction lock 前完成受限读取。

### 修改 Admin UI

注意：

- 生产 UI 无 Node runtime 依赖；
- 前端使用嵌入静态文件；
- 不引入外部 CDN/font/icon runtime；
- 需要兼顾 320/390/768/1024/1440px；
- keyboard focus / aria / reduced motion；
- editor 必须固定打开时 ETag；
- config 与 ETag 必须原子地进入前端 state。

### 修改 Remote Admin CLI

当前命令：

```bash
mcp-hub admin list
mcp-hub admin get <id>
mcp-hub admin add <id> --file server.json
mcp-hub admin edit <id> --file server.json
mcp-hub admin delete <id> --yes
```

必须保持：

- Admin Token 与 MCP Token 分离；
- 远程非 loopback 明文 HTTP 拒绝；
- redirect 拒绝；
- session + CSRF；
- ETag/CAS；
- Secret redaction；
- bounded input/output。

## 4. 基础验证命令

仓库根目录：

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath ./cmd/mcp-hub
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

SDK probe：

```bash
cd verification/sdkprobe
go test -count=1 -timeout 120s ./...
go test -race -count=1 -timeout 120s ./...
```

浏览器回归：

```bash
cd internal/admin/browsertest
npm ci
npx playwright install --with-deps chromium
npm test
```

真实 Hub 浏览器 smoke：

```bash
go build -trimpath -o dist/mcp-hub ./cmd/mcp-hub
node internal/admin/browsertest/live-smoke.mjs
```

CI 还会执行跨 OS/Go matrix 和 `govulncheck`。

## 5. 配置修改原则

- 先读取当前 config；
- 保留未知于当前任务但合法的现有字段；
- 只修改必要 subtree；
- Secret 优先用 `${NAME}` 服务端环境引用；
- staged config 先 validate；
- 有运行中服务时先备份；
- connection-changing Admin 变更依赖服务端 preflight；
- 不用手工删除 `.lock` 作为常规恢复手段；
- 不与 Admin CLI / Browser 同时直接写配置文件。

## 6. 部署 / 升级流程

优先使用稳定 GitHub Release；需要验证尚未发布的 `main` 时必须明确这是 source/RC build。

标准服务器流程：

1. inventory OS/arch/服务方式/现有版本；
2. 备份 binary/config/env/service/proxy；
3. 下载 Release + `SHA256SUMS`，或按任务要求构建精确 commit；
4. validate 现有配置；
5. 原子替换 binary；
6. restart；
7. `status` / `doctor`；
8. 检查 `/admin/`；
9. 如失败回滚；
10. 报告版本、路径、服务状态、endpoint 和备份位置，不输出 Secret。

公网部署参考 `docs/VPS.md`。

## 7. Agent Skill

仓库内置 `internal/admin/agent-skill/`，随二进制嵌入。

管理台登录后可下载 `skill.zip` 或复制通用 Agent Prompt。Skill 用于：

- Linux/macOS/Windows 安装；
- VPS 部署；
- HTTPS reverse proxy；
- native HTTP client；
- stdio bridge；
- upgrade / repair / verification / rollback。

如果修改 Skill 本身，需要同时验证其文件结构、脚本和打包行为，并确保管理台下载到的是与当前二进制匹配的版本。

## 8. PR 与合并规则

建议：

1. 从最新 `main` 创建短生命周期分支；
2. 单一主题；
3. 先本地/目标测试；
4. 开 Draft PR；
5. 等 final head CI；
6. 检查 base/head/diff；
7. Ready；
8. 合并时锁 expected head SHA；
9. 再看 main push CI。

堆叠 PR 必须按依赖顺序合并，并在每次合并后重新确认后续 PR base/diff，防止重复提交或错误基线。

## 9. Release 规则

当前源码身份为 `0.4.0`，但正式 Tag/Release 之前仍是 RC。

发布前：

- 当前 main 全 CI green；
- 文档与 final head 一致；
- open dependency PR 有明确处理决定；
- 没有已知 blocker；
- exact tag 指向预期 commit；
- Release workflow 成功；
- 六个平台/架构包齐全；
- `SHA256SUMS` 存在。

不要移动已经公开使用的 release tag 来“修正”发布边界；需要修复时使用新 patch release。

## 10. 最终汇报格式

至少包含：

- 基线 commit / PR；
- 修改内容；
- 风险与安全边界；
- 实际执行的验证；
- CI 结果；
- 未验证项；
- 是否已 merge / deploy / release；
- 后续治理事项。

不要使用“绝对没有 bug”这类无法证明的描述。更准确的表述是：

> 在当前审计和已执行验证范围内，没有发现已知 release-blocking 问题。
