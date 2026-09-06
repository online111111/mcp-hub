# MCP Hub 开发设计文档

- 版本：0.2，技术决策基线，替代 v0.1 全文。
- 定位：个人使用的 MCP 聚合与请求路由器。
- 核心价值：下游只配置一次，自己的多个客户端各接入 Hub 一次。
- 配套：`MCP-Hub-Agent实施手册.md`、`verification/sdkprobe/README.md`。
- 状态：架构与开发契约已收敛，SDK 探针已验证；不是完整产品验收或所有客户端兼容性保证。

## 1. 目标与范围

用户导入已有 MCP 配置，启动一个常驻 Hub。HTTP 客户端连接 `/mcp`，需要 stdio 的客户端运行桥接。Hub 管理下游、汇总工具、路由调用并返回结果。新增服务、换 Token、改路径只维护 Hub。部分客户端需要刷新或重连，但无需重新填写下游配置。

### P0 必须交付

| ID | 能力 |
|---|---|
| FR-01 | 严格 JSON 配置、服务增删改和启停 |
| FR-02 | 常见 mcpServers JSON 导入预览、确认写入 |
| FR-03 | stdio、Streamable HTTP 下游 |
| FR-04 | 工具发现、下游分页、名称隔离、禁用名单 |
| FR-05 | 有状态 Streamable HTTP `/mcp` |
| FR-06 | stdio 到常驻 Hub 的桥接，不重复启动下游 |
| FR-07 | 调用准确转发及结果语义保留 |
| FR-08 | 差异热重载，非法配置保留上一有效版本 |
| FR-09 | 超时、取消、并发限制、独立重连、进程清理 |
| FR-10 | validate、import、export、status、doctor |
| FR-11 | 只读诊断、最近调用摘要及错误脱敏 |
| FR-12 | 回环监听、Host/Origin 检查、资源上限 |

### 不进入 P0

旧 HTTP+SSE、YAML、Web UI、配置写 HTTP API、工具审批状态、工具分组、多用户、计费、OAuth、负载均衡、自动重放、按需启动、per-client 下游会话。

不代理 resources、prompts、sampling、roots、elicitation、任务扩展、进度通知和未知扩展。不做 Agent 决策或工作流编排，不是沙箱。旧 SSE 明确属于 P1，不自动猜测协议和降级；已有服务仅支持旧 SSE 时，报告当前不支持。

## 2. 已固定的技术决策

| 项目 | 决策 |
|---|---|
| 语言 | Go；go directive 1.25.0；验证工具链 Go 1.26.3 |
| SDK | `github.com/modelcontextprotocol/go-sdk v1.4.1`，不使用 @latest |
| 协议基线 | SDK 默认 2025-06-18，沿用其协商，不宣称其他版本全部能力合规 |
| 配置 | 单一 JSON 配置文件，不要审批 state.json，不要数据库 |
| 工具注册 | 非泛型 `Server.AddTool` / `Server.RemoveTools` |
| 原子发布 | Hub 发布读写锁 + SDK 内置线程安全目录，不重写 JSON-RPC |
| 会话 | 一个下游服务共享一个会话；每个上游客户端独立会话 |
| 重载 | 连接级变更统一 drain → close → connect，接受短暂不可用 |
| 工具策略 | 初次和后续新工具均默认开放；由 tools.disabled 过滤 |
| 配置检测 | 标准库每秒读取并做内容摘要，无 fsnotify 依赖 |
| 管理面 | 文件唯一写入口；HTTP 仅只读诊断 |
| 主验收系统 | Windows amd64；Linux CI 测试及构建；macOS 构建不冒充实测 |

选用 v1.4.1 是可复现的保守基线，不声称它是最新版本。已下载检查 v1.7.0，其默认协议常量为 2026-07-28，首版避免同时引入其协议模型变化。发布前必须漏洞检查；影响当前路径的漏洞须显式升级、记录 ADR 并重跑验证，不能以锁版本为由忽略。

## 3. 架构及模块

```text
HTTP 客户端 ─────────────────────────┐
stdio 客户端 → mcp-hub stdio ─────────┤
                                    ▼
                         常驻 Hub /mcp
                          SDK MCP Server
                                  │
                   Catalog / Publisher / Router
                                  │
                    每服务 Manager + Generation
                         ┌────────┴─────────┐
                     stdio 进程         HTTP MCP

config.json → Poller → Reconciler
                         │
              安全状态 DTO / 有界调用摘要
              /healthz /readyz /api/v1/status
```

```text
cmd/mcp-hub/
internal/config/       # 解析、导入、写入、内容摘要
internal/downstream/   # SDK ClientSession 适配
internal/process/      # worker、管道、平台进程清理
internal/manager/      # 状态、generation、重连、排空
internal/catalog/      # 公共名称、过滤、目录定义
internal/router/       # 租用连接、转发、错误映射
internal/inbound/      # SDK Server、发布事务、HTTP
internal/bridge/       # stdio 到 Hub 的协议适配
internal/diagnostics/  # 安全状态和摘要
internal/cli/
internal/testserver/   # 可控假下游
verification/sdkprobe/ # 已有验证，不作为生产实现
```

CLI 组装模块，Router 依赖 Manager 租用接口。Config 不启动进程。不得直接序列化 SDK Session 或 Manager 对象供诊断使用。

### 下游 API

使用已存在的 `mcp.NewClient`、`Client.Connect`、`mcp.StreamableClientTransport`、`mcp.IOTransport`、`ClientSession.ListTools`、`CallTool`、`Close`。

准确类型名是 StreamableClientTransport，不是 StreamableHTTPTransport。生产 stdio 管理由 process 模块负责，SDK 接收 IOTransport；CommandTransport 仅用于简单验证。

所有下游 Client 和桥接 Client 必须显式设置 `ClientOptions.Capabilities: &mcp.ClientCapabilities{}`；v1.4.1 在该值为 nil 时默认宣告 roots/listChanged，不显式覆盖就会违反 P0 能力边界。不要注册 sampling/elicitation handlers。

`ClientOptions.ToolListChangedHandler` 仅标记/入队，不在 SDK 回调中阻塞等待工具重新发现。遍历 NextCursor 到空，检测重复 cursor 和重复原始名称。收到发现期间的变化标记，完成后最多再拉一次，后续合并到下轮。

## 4. SDK 集成与原子发布

### 4.1 已查证事实

v1.4.1 `mcp/server.go`：AddTool 可替换同名工具；RemoveTools 存在；changeAndNotify 持 SDK mutex 并合并通知；目录和调用查找持 SDK mutex。不存在“SDK 无法线程安全增删工具，必须重写分发器”的前提。

非泛型 `Server.AddTool` 接收动态 schema，handler 的 Arguments 为 json.RawMessage。不要用泛型 AddTool 为代理工具推断 schema。

### 4.2 发布事务

Hub 维护单一 Publisher 和 `publishMu sync.RWMutex`。

1. 锁外完成发现、校验、名称计算和容量检查。
2. 获得发布写锁。
3. 更新路由表及 SDK RemoveTools/AddTool。
4. 增加目录 revision，释放锁。

SDK 通用 handler 只捕获公共名称，执行时读取当前路由，不捕获过期连接。

使用 `Server.AddReceivingMiddleware` 只给 tools/list 的 next 调用包读锁。Router 在查路由和取得 generation 租用时持短读锁，然后释放。

禁止在发布锁内联网、排空、Close 或等待并发许可；禁止给整个 tools/call 持读锁。SDK 自身完成变化通知，不手写通知协议。

每次 tools/list 看不到半发布目录；跨多次分页不承诺同一 revision。变更期间客户端可重新枚举，不把它写成跨页事务保证。

### 4.3 注册前验证

SDK AddTool 对无效 schema 可能 panic，必须先验证：inputSchema 为 JSON 对象，顶层 type=object；outputSchema 若存在为可编码 JSON；工具定义及目录不超限。未通过的单个工具标记 unpublished，不影响其他工具。

传入 SDK 的定义注册后不再修改。保留 SDK 可表达的 description、schema、annotations 等字段，不承诺未知扩展字段无损。

## 5. 配置契约

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
    "filesystem": {
      "enabled": true,
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "D:/data"],
      "cwd": "D:/data",
      "env": {},
      "tools": { "disabled": [] }
    },
    "search": {
      "type": "streamable_http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer ${SEARCH_TOKEN}" }
    }
  }
}
```

### 5.1 严格规则

- 正式配置 version=1 必填；拒绝未知字段、重复 JSON key 和尾随第二个 JSON 值。
- ID：`^[a-z][a-z0-9_-]{0,31}$`，是稳定标识，不加未定义的 displayName 字段。
- enabled 默认 true，解码须区分未填与 false。
- 正式配置 type 必填。stdio 必须 command，禁止 url/headers；HTTP 必须 url，禁止 command/args/cwd/env。
- 服务级 startupTimeout/callTimeout/maxConcurrency 覆盖 defaults。正时长不超过 24h，并发整数 1..64。
- cwd 缺省配置目录；cwd 及带路径的相对 command 基于配置目录解析。args 原样传递，不改写路径。
- env 继承 Hub 环境后覆盖；Windows 合并键时不区分大小写。
- 仅 env/headers 单次展开 `${NAME}`；`$${NAME}` 为字面量；缺变量报错，不递归展开、不执行 shell。
- 修改进程环境需重启 Hub；修改配置字段可以热重载。
- HTTP URL 支持 https，http 仅允许字面回环 IP/localhost；拒绝 userinfo/fragment。
- 禁止配置 Host、Content-Length、Connection、Mcp-Session-Id、MCP-Protocol-Version 等传输头。
- 下游 HTTP 禁止跟随重定向，避免认证跨来源泄露。
- hub.listen 仅字面回环地址，默认 127.0.0.1:8080，可显式 [::1]:8080；不自动双绑定。
- listen 变更标记 restart_required，不热换端口；其他合法下游变更仍可生效。

### 5.2 工具过滤

首次和后续发现工具均自动开放，仅按原始名 tools.disabled 禁用。没有审批状态文件。下游升级可能增加新能力，导入说明须明确提醒，用户主动禁用敏感工具。

### 5.3 导入

仅转换顶层 mcpServers：有 command 无 type → stdio；明确 type=http → streamable_http；URL 无 type 要求 `--remote-type streamable_http`，不猜测。

旧 sse、未知字段、非法 ID、重名均报告并中止写入。dry-run 不写，正式导入必须 --yes，默认不覆盖。

导入不启动服务，预览不展示凭证，也不要求当时具备引用的环境变量；validate/serve 才展开并检查。

### 5.4 文件更新

每秒读取上限 1 MiB 并计算 SHA-256，连续两次相同新摘要后处理。非法内容每摘要只报告一次，保留旧有效配置。

写入：同目录临时文件 → 写入 → Sync → Close → 原子替换。POSIX rename；Windows 平台实现 ReplaceFile/MoveFileEx，禁止先删除原文件。

CLI 写入使用 O_EXCL 锁文件串行化，写前比对原摘要，变化就中止。与不遵守锁的外部编辑器不构成原子 CAS；遗留锁要求人工确认后移除，不按时间抢锁。

POSIX 新配置 0600；Windows 使用或检查用户目录 ACL，不能把 chmod(0600) 当成 Windows 权限隔离。

## 6. 命名和调用

### 6.1 公共名称

默认 `<serverID>__<originalName>`。Hub 自身兼容性限制为 `[A-Za-z0-9_-]`、最长 64 字符，这不是声称 MCP 标准只有该限制。

非法字符或超长则生成稳定别名：

```text
serverID + "__h" + hex(SHA256(serverID + NUL + originalName))[:24]
```

最长 59 字符。先分配合法直拼名，再分配哈希名；任何冲突不覆盖，冲突工具标记未发布并报告。查显式映射，不切割前缀。

### 6.2 请求链

1. SDK 解析请求。
2. Router 读锁检查开放和当前 generation。
3. generation 锁下检查 Ready/admission，非阻塞取得并发许可并增加 active。
4. 释放锁，创建 callTimeout 子 context。
5. 用原始工具名和 json.RawMessage 参数调用下游。
6. 保留 SDK 可表达的 content、structuredContent、isError，释放租用。

不得把参数解为 float64 后重编码而破坏大整数。不保证结果未知字段、SDK any 解码数值或 JSON 字节级精确保留；大整数结果精度须有测试和文档，不夸大透传保证。

### 6.3 错误映射

| 情况 | 对外结果 |
|---|---|
| 未知/禁用工具、非法 cursor | JSON-RPC -32602 |
| 不支持方法 | SDK -32601 |
| 已知工具下游不可用/繁忙/超时 | CallToolResult isError=true，短分类和 request_id |
| 下游工具执行失败 | 保留其结果及 isError |
| 下游协议/传输错误 | 脱敏 isError，不透出原始命令/header |
| 客户端取消 | 沿 context/SDK 传播，不保证取消方仍收到结果 |
| 内部错误 | -32603，详情仅经过筛选的诊断 |

不自动重放工具调用。超时、取消不表示副作用被撤销。SDK SSE 流恢复不代表允许再次 POST tools/call，必须测试下游调用次数。

## 7. 生命周期和重载

```text
Disabled
Connecting → Ready
Connecting → Unavailable → Backoff → Connecting
Ready → Draining → Closed → Connecting
```

每服务独立 coordinator；配置有 revision，连接有 generation。异步结果仅可发布到匹配的 desired revision，过期结果立即关闭。连接成功包含初始化及完整工具发现。

启动并发 4；失败退避 1/2/4/8/16/30 秒封顶，抖动 ±20%，连续 Ready 60 秒重置。

| 变化 | 行为 |
|---|---|
| 无变化 | 保留实例 |
| 仅 disabled 工具名单 | 只更新目录 |
| 仅 callTimeout | 新调用用新值，在途保持旧截止 |
| maxConcurrency/连接字段 | 排空并重连 |
| 新增服务 | 异步连接，失败不影响其他服务 |
| 删除/禁用 | 撤销新调用许可、移除目录，再排空 |
| 命令/凭证/地址/cwd/env | 先关旧再连新，不双开 |

所有连接级变化 break-before-make，不自动回退旧凭证。失败按新配置重试，用户恢复旧文件即可回滚。这里接受短暂不可用，不承诺毫秒级恢复。

排空先关闭 admission 再等 active=0，最多 10 秒，到期取消并清理。不要使用存在 Add/Wait 竞态的裸 WaitGroup 替代租用协议。

曾发现工具的服务临时离线时可保留当前运行期目录，调用报 unavailable；新连接成功再更新元数据，status 必须体现不可用。首次从未成功的服务不发布猜测工具。

## 8. 进程所有权和 Windows 路线

生产不靠 SDK CommandTransport 自动管理进程树。process 模块持有 Start/Wait/管道/清理，一个进程只允许一个 Wait 所有者。

### 8.1 Windows 启动门闩 worker

避免启动后才分配 Job 的后代逃逸竞态：

1. Hub 创建 KILL_ON_JOB_CLOSE Job Object，句柄不可继承。
2. 启动同一 exe 的隐藏 `_worker`，用 stdin/stdout 管道通信。
3. worker 先等一条有上限的 bootstrap JSON 行，未收到前不能启动下游。
4. Hub 将 worker 加入 Job；失败就终止并 Wait，禁止无 Job 降级。
5. 成功后写 bootstrap，包含解析后的命令、参数、cwd/env。
6. worker 读取 bootstrap 后启动下游并双向复制 MCP；必须复用 buffered reader，保留预读字节。
7. SDK IOTransport 在 bootstrap 后使用该管道。
8. Close 先关协议输入，2 秒宽限后关闭 Job、清理后代并 Wait。

bootstrap EOF 或 5 秒超时退出，所以父进程在分配 Job 前死亡也不会拉起下游。bootstrap 不写日志、不放命令行。worker stdout 仅下游协议，处理复制 goroutine/EOF/Wait 协调。

用 `golang.org/x/sys/windows v0.40.0` 实现 Job；嵌套 Job 失败明确报错。Process.Kill 或 taskkill 本身不能算作树清理验收通过。

### 8.2 Windows npx

- 普通 exe 直接启动，uvx 可能是 exe，不能按名字推断为批处理。
- npx/npx.cmd：查找标准 Node 安装目录，要求同目录 node.exe 和 node_modules/npm/bin/npx-cli.js，归一化为 node.exe + npx-cli.js + 原参数。
- 非标准布局报明确修复建议，用户可直接配置 node.exe 和脚本绝对路径。
- 其他 .cmd/.bat 默认拒绝，提示指定原生解释器。
- 用户显式 command=cmd.exe 属主动启用 shell，原样执行并警告；不自动生成 shell 字符串，也不承诺此时没有 shell 风险。

### 8.3 POSIX

新进程组、关闭输入、TERM、2 秒后 KILL、Wait。恶意服务脱离进程组不属沙箱保证；Hub 被 SIGKILL 后不承诺与 Windows Job 等价的级联清理。

## 9. HTTP、桥接和安全

### 9.1 服务

使用 `mcp.NewStreamableHTTPHandler`，所有连接返回同一 Server。`Stateless:false`，不启用 JSON-only 响应模式，保留 GET 事件通道。

ServerOptions 显式 Tools.ListChanged=true，空目录也宣告 tools，其他未实现能力不宣告。

ReadHeaderTimeout=5s，IdleTimeout=120s，不用短全局 WriteTimeout 杀死 SSE。HTTP Client 不用覆盖 SSE 总寿命的短 Client.Timeout，使用阶段超时和调用 context。

### 9.2 桥接

先连接 Hub 并取完整目录，再运行本地 SDK StdioTransport。每桥接一个独立 Hub ClientSession，使用非泛型 AddTool 转发。

Hub list_changed → 回调入队 → 重新拉目录 → 本地 SDK 增删 → 本地客户端通知。会话失败则桥接非零退出，不重放请求。桥接不读主配置、不启动下游、不用 worker。取消须两跳传播。

### 9.3 只读端点

| 端点 | 契约 |
|---|---|
| GET /healthz | HTTP 存活，200，status=ok |
| GET /readyz | 无启用服务，或至少一个 Ready 且有公开工具则 200；其他 503 |
| GET /api/v1/status | 安全状态 DTO，不支持写入 |

DTO：version、uptimeSeconds、catalogRevision、restartRequired、lastReloadStatus、servers[]、recentCalls[]。

servers 只含 id、state、publishedToolCount、unpublishedToolCount、activeCalls、desiredRevision、activeRevision、errorCategory。recentCalls 只含 requestId/time/durationMs/tool/serverId/outcome/errorCategory。

不得返回 URL、env、headers、命令、args、路径、Session、原始栈、参数或结果。Cache-Control:no-store，无 CORS。

### 9.4 本地安全

Host 必须匹配绑定地址/端口或同端口 localhost，不信任 X-Forwarded-Host。P0 无浏览器 UI，所有带 Origin 的请求一律 403，包括 null；无 Origin 的本机客户端可访问。

不是防御同用户恶意本机进程的认证方案。未来浏览器 UI 必须一并设计认证/CSRF，不能直接放开 Origin。配置由可信用户维护，下游继承当前用户权限。

## 10. 硬性默认上限

| 项目 | 值 |
|---|---|
| 配置 | 1 MiB |
| 服务 | 32 |
| 同时连接建立 | 4 |
| 调用并发/等待队列 | 每服务 8，队列 0，满立即拒绝 |
| 初始化+发现 | 共 20s，可覆盖 |
| 单调用 | 60s，可覆盖 |
| 排空/退出宽限/总关闭 | 10s / 2s / 15s |
| 配置采样 | 1s，稳定两次 |
| 周期工具刷新 | 60s，各服务错开 |
| 工具数 | 每服务 512，总计 2048 |
| 下游分页 | 最多 64 页，重复 cursor 失败 |
| 单工具定义 | 256 KiB |
| 协议单消息 | 8 MiB |
| 汇总目录编码 | 8 MiB |
| 上游 HTTP 会话 | 32，业务空闲 30min 回收 |
| 调用摘要 | 200 条环形缓冲 |
| 子进程 stderr | 默认丢弃正文计数；debug 才保留每服务 16 KiB |
| 日志文件 | 默认关，stderr 输出；可选 5 MiB×3 轮转 |

HTTP POST、stdio 单行和 SSE 单 event 分别限制。不得对 SSE 整条长连接使用累计 8 MiB LimitReader；应在 event 边界限长。P0 可拒绝压缩，不能把压缩前大小当成解压后限制。

会话 admission：HTTP middleware 对无 Mcp-Session-Id 的 POST 先加 initMu，检查 Server.Sessions() 数量是否小于 32，然后同步调用 SDK ServeHTTP，返回后释放；非法或失败初始化不增加永久计数。GET/DELETE/已有会话 POST 不持 initMu，避免串行化工具执行。SDK handler 配置 SessionTimeout=30min，沿用“期间没有新 HTTP 请求”的官方语义，建立中的 GET 不因保持打开而无限续期。对无 session 请求设置 5s 请求体读取截止，避免初始化锁被慢客户端长期占用。若该路径的响应不能在初始化后有限时间返回，测试必须失败并改为显式 reservation 方案，不能删除容量控制。

## 11. CLI

```bash
mcp-hub serve --config config.json
mcp-hub stdio --connect http://127.0.0.1:8080/mcp
mcp-hub validate --config config.json
mcp-hub import --from old.json --config config.json --dry-run
mcp-hub import --from old.json --config config.json --yes
mcp-hub export --client cursor --transport stdio --endpoint http://127.0.0.1:8080/mcp
mcp-hub export --client claude-desktop --transport stdio --endpoint http://127.0.0.1:8080/mcp
mcp-hub status --endpoint http://127.0.0.1:8080
mcp-hub doctor --endpoint http://127.0.0.1:8080
```

- serve 先绑定端口，成功后才启动下游，第二实例端口冲突不得先拉进程。
- validate 只校验，不联网/启动/写入。
- import 仅 --yes 写入，和 --dry-run 互斥，不依赖交互。
- export 默认 stdio，command 为当前 exe 绝对路径；HTTP 模板需显式 --transport http；不输出下游秘密，标明实际客户端版本是否测试。
- status 支持 --json；doctor 只检查已运行 Hub 并执行 initialize/list，不调用业务工具、不离线另起下游。
- Hub 未运行 doctor 返回非零并建议先 serve。
- 退出码 0 成功，2 参数/配置/冲突，3 网络/运行不可用，1 内部错误。
- 日志 stderr，stdio stdout 永远仅协议。

## 12. 验证、交付与证据

### 12.1 必测类别

配置严格解码/导入；哈希命名/schema；分页/通知；同名工具路由；大整数参数/结构化结果/isError；多客户端；两跳取消；不重放；热更新过期结果；独占资源无双开；禁用和排空竞态；Windows worker/Job 孙进程回收；npx 特殊路径；Host/Origin；重定向和大小边界；会话限制；凭证与 stdout 泄露。

### 12.2 验收分层

A. SDK 探针：`verification/sdkprobe` 已有可运行测试。它验证 SDK API 和传输，不覆盖生产进程树、完整 Hub 或真实客户端。

B. 产品自动化：生产模块、假下游端到端、竞态和资源测试。

C. 人工兼容：真实 Cursor/Claude Desktop 与用户下游，记录版本/OS/传输/通知。不具备环境标记 NOT_RUN，不阻塞写代码，不伪造 PASS。

D. 发布：可执行产物、校验和、依赖漏洞检查、安装说明、资源基准。分别报告 Hub 与下游占用，不承诺固定 10 MB 内存。

### 12.3 官方资料

- https://github.com/modelcontextprotocol/go-sdk/tree/v1.4.1
- https://github.com/modelcontextprotocol/go-sdk/blob/v1.4.1/mcp/server.go
- https://github.com/modelcontextprotocol/go-sdk/blob/v1.4.1/mcp/transport.go
- https://github.com/modelcontextprotocol/go-sdk/blob/v1.4.1/mcp/streamable.go
- https://github.com/modelcontextprotocol/go-sdk/blob/v1.4.1/mcp/shared.go
- https://modelcontextprotocol.io/specification/2025-06-18

本次实际下载并读取 SDK 源码。v1.4.1 模块校验值：`h1:M4x9GyIPj+HoIlHNGpK2hq5o3BFhC+78PkEaldQRphc=`。

接手 Agent 按实施手册顺序完成任务，不重复访谈已决选型，不把探针通过当作生产软件完成。
