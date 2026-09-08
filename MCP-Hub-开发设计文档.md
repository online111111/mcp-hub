# MCP Hub 开发设计文档

- 文档版本：0.4
- 产品阶段：v0.4.0 release candidate
- 当前实现基线：Go 1.25+，MCP Go SDK v1.7.0
- 定位：个人/受信环境中的 MCP 聚合、路由、管理与客户端接入网关

本文描述**当前实现**。历史 v0.2 设计中的“无 Web UI、HTTP 管理面只读、固定 SDK v1.4.1”等约束已经失效，不再作为开发依据。

## 1. 产品目标

MCP Hub 将多个下游 MCP 服务集中配置、启动、发现和管理，再通过一个统一 `/mcp` Streamable HTTP 入口向客户端提供工具目录与调用路由。

核心目标：

1. 下游只维护一次，多个客户端复用同一 Hub；
2. 同时支持本地 stdio 与远程 Streamable HTTP 下游；
3. 支持 HTTP 客户端与仅支持 stdio 的客户端；
4. 配置变更安全、可验证、可回滚；
5. 公网部署默认 fail-closed；
6. 管理台、远程 CLI、Agent Skill 复用同一套管理语义；
7. 保持单 Go 二进制生产形态，不引入前端运行时依赖。

## 2. 当前能力范围

### 已实现

- 严格 JSON 配置、环境变量引用、配置导入/导出/校验；
- stdio 和远程 Streamable HTTP 下游；
- 工具发现、分页、名称隔离、禁用名单；
- 动态工具发布与 `tools/list_changed`；
- 调用转发、取消、超时、并发限制、重试/backoff；
- POSIX process group 与 Windows Job Object 进程树管理；
- 文件轮询 + Admin 写入共享配置事务域；
- 原子持久化、跨进程锁、ETag/CAS；
- 新建/连接级修改的 downstream preflight；
- `/admin/` 响应式管理台；
- 远程 Admin CLI：`list/get/add/edit/delete`；
- stdio bridge；
- `status` / `doctor`；
- 公网 HTTPS/Host/Token/可信代理安全策略；
- 内置 `mcp-hub-deployer` Skill 和 Agent Prompt；
- Linux / Windows / macOS CI、race、SDK probe、Chromium、govulncheck；
- Release workflow：六平台架构包 + SHA256SUMS。

### 明确不宣称

- MCP resources / prompts / sampling / roots / elicitation / task extensions 的通用代理；
- 任意未知 MCP 扩展方法透明转发；
- 完整原生 MCP 2026-07-28 能力覆盖；
- 多租户、RBAC、团队平台、计费；
- 沙箱或不受信代码执行隔离；
- 对所有第三方 MCP 服务或 GUI 客户端的兼容认证；
- 自动重放可能产生副作用的工具调用。

## 3. 协议与 SDK 基线

生产模块和 `verification/sdkprobe` 都固定 MCP Go SDK v1.7.0。

MCP Hub 使用 SDK v1.7.0 提供的客户端/服务端能力，但产品兼容语义仍保持当前经过验证的有状态/session-oriented Streamable HTTP 模型，并包含旧对端发现兼容处理。不要因为 SDK 本身支持更新协议常量，就自动扩大产品兼容声明。

协议边界以 `docs/COMPATIBILITY.md` 为事实来源。

## 4. 核心架构

```text
HTTP MCP client ------------------------------+
                                               |
stdio-only client -> mcp-hub stdio -----------+--> Hub /mcp
                                                    |
                                                    v
                                            SDK MCP Server
                                                    |
                                              Publisher
                                                    |
                                               Catalog
                                                    |
                                                Router
                                                    |
                                         Downstream Manager
                                          /              \
                                      stdio          Streamable HTTP
```

并行管理面：

```text
Browser /admin/ ----+
                     |
mcp-hub admin -------+--> Admin API --> Config transaction
                     |                       |
Agent / Skill -------+                       +--> strict validate
                                             +--> preflight
                                             +--> CAS + lock
                                             +--> atomic persist
                                             +--> runtime reload
                                             +--> rollback/restore
```

### 配置是事实来源

磁盘 JSON 配置仍是持久化事实来源。运行时只接受经过严格解析、验证和环境解析后的快照。

Admin 浏览器、远程 Admin CLI 与 Agent 自动化都必须走同一个 Admin 事务路径，不应通过旁路直接改磁盘文件来绕过 ETag、preflight 或回滚。

## 5. 配置事务模型

配置事务需要同时保证：

- 进程内串行化；
- 跨进程 OS advisory lock；
- ETag/CAS 防止陈旧编辑覆盖；
- 1 MiB 配置读取上限；
- 严格 JSON 和字段大小写；
- 保存前连接级 preflight；
- 原子替换与持久化；
- reload 后失败时保留/恢复最近健康状态。

Admin PUT 请求必须先读取受限网络 Body，再进入共享配置事务锁，避免慢客户端阻塞 runtime polling。

## 6. Downstream 模型

### stdio

普通 command/args 不经过 shell 插值。POSIX 使用独立 process group，Windows 使用 worker + Job Object 管理进程树。

Hub 不把完整宿主环境直接继承给 stdio 子进程，只提供安全兼容基线；额外变量必须通过服务器 `env` 显式声明。Hub 的 MCP/Admin 认证 Secret 不应被隐式继承。

如果用户显式选择 `cmd.exe`、`cmd` 或其他 shell，则视为主动进入 shell 语义，其转义和执行风险由该 shell 决定。

### Streamable HTTP

远程非回环目标必须使用 HTTPS。禁止 redirect，避免认证 Header 跨源泄露。自定义 Header 不能覆盖 MCP/session framing Header。

## 7. 工具目录与调用语义

Catalog 保存下游工具、发布状态与稳定公开名称。Publisher 将经过验证的工具发布到上游 SDK Server。Router 使用显式 public-name -> server/original-tool 映射执行调用。

原则：

- 不根据名称猜测目标；
- 不在不确定结果时自动重放调用；
- raw JSON arguments 尽可能原样转发；
- 对可表示的 MCP 结果语义保持结构；
- transport/protocol 错误向外只暴露短分类与 request ID；
- 工具 schema/name 在发布前验证。

## 8. 热重载与生命周期

运行时控制器比较配置 revision，并拒绝陈旧快照。连接级变化执行对应 generation drain/close/reconnect；startup-bound Hub 变化标记 `restartRequired`。

旧凭据或 listener 行为在完整重启前可能仍然有效，因此 UI/CLI 不得把 `restartRequired` 描述成“已立即生效”。

关闭时先停止接收新调用，再 drain lease/generation，最后释放 downstream 和进程资源。

## 9. 管理面

### Browser Admin

`/admin/` 使用：

- HttpOnly/SameSite Cookie；
- HTTPS 下 Secure Cookie；
- 同源检查；
- synchronizer CSRF；
- 登录/API 速率限制；
- Secret placeholder；
- ETag/CAS；
- CSP、frame deny、nosniff 等安全 Header。

编辑器在打开时固定 revision，防止后台刷新让旧草稿获得新版 ETag。

### Remote Admin CLI

客户端可通过：

```bash
MCP_HUB_ADMIN_TOKEN='...' mcp-hub admin list --endpoint https://host
mcp-hub admin get <id> --endpoint https://host
mcp-hub admin add <id> --file server.json --endpoint https://host
mcp-hub admin edit <id> --file server.json --endpoint https://host
mcp-hub admin delete <id> --endpoint https://host --yes
```

详细语义见 `docs/REMOTE-ADMIN.md`。

## 10. 公网部署

推荐：

```text
Internet -> Caddy/Nginx HTTPS -> 127.0.0.1:8080 MCP Hub
```

公网模式要求：

- `publicMode: true`；
- HTTPS `publicUrl`；
- 明确 `allowedHosts`；
- 仅信任本机/真实代理 CIDR；
- MCP Token 与 Admin Token 分离且各至少 32 字符；
- 不直接暴露 Hub 8080。

详见 `docs/VPS.md` 与 `docs/SECURITY.md`。

## 11. 客户端接入

原生支持 Streamable HTTP 的客户端优先直接连接 `/mcp`。

仅支持 stdio 的客户端使用：

```bash
MCP_HUB_TOKEN='...' mcp-hub stdio --connect https://host/mcp
```

需要生成特定客户端配置时优先使用当前 `mcp-hub export`，不要依赖过期手写格式。

## 12. 验证与 CI

当前 CI 覆盖：

- Ubuntu / Windows / macOS；
- Go 1.25.8 与 1.27.1；
- `go test ./...`；
- current Go race；
- `go vet`；
- 独立 SDK probe；
- Linux Admin UI unit tests；
- Chromium interaction/design regression；
- real-Hub Chromium smoke；
- `govulncheck`。

自动化 PASS 不代表所有 GUI 客户端、所有第三方 MCP 或所有反向代理组合均已认证。证据边界见 `docs/IMPLEMENTATION-STATUS.md`。

## 13. Release

源码版本身份当前为 `0.4.0`，但在正式 `v0.4.0` Tag 和 GitHub Release 创建之前仍应称为 release candidate。

Release workflow 会从精确 Tag：

1. 校验 Tag 与源码版本；
2. 运行 test/vet/sdkprobe；
3. 构建 Linux/Windows/macOS × amd64/arm64 六套包；
4. 注入 commit/build date；
5. 生成 `SHA256SUMS`；
6. 创建 GitHub Release。

## 14. 维护规则

修改核心行为时：

1. 先明确影响的契约；
2. 添加失败回归；
3. 修改实现；
4. 跑目标测试、全量 test/race/vet；
5. 必要时跑 SDK probe/Chromium；
6. 同步 README、COMPATIBILITY、SECURITY、IMPLEMENTATION-STATUS；
7. 不把文档勾选当作测试证据；
8. 不通过放宽断言隐藏行为缺失；
9. 不在没有证据时扩大兼容声明。

当前用户入口：`README.md` / `README.zh-CN.md`。
