# MCP Hub：Agent 逐步实施手册

## 0. 接手规则

先读 `MCP-Hub-开发设计文档.md` v0.2，再读 `verification/sdkprobe/README.md` 并运行探针。

目标是实现个人 MCP 聚合网关，不改当前 DeepSeek Harness，不使用动态 Cordis，不做团队平台。不重新询问已固定的 SDK/配置/UI 范围。缺真实客户端不会阻塞基础开发，只影响人工兼容验收。

当前已有文档与独立 SDK 测试模块，尚无生产项目。下面命令是后续目标，不代表当前根目录已有 go.mod 或应用。

### 工作约定

1. 在当前项目根创建生产 Go 模块 `mcp-hub`，go 1.25.0，固定官方 SDK v1.4.1。
2. verification/sdkprobe 保留独立 go.mod；根目录 go test ./... 不会自动覆盖嵌套模块，要分别运行。
3. 不复制或修改 Go module cache 内 SDK；需要适配时在项目包实现。
4. 每任务先测试，再实现，再跑相关测试及全量；完成一个才推进下一个。
5. 建立 `docs/IMPLEMENTATION-STATUS.md`，逐项记 NOT_STARTED / IN_PROGRESS / PASS / FAIL / NOT_RUN、命令和证据路径。
6. 技术失败自行查看日志修复，不以“后续完善”跳过 P0，不把文档勾选当测试结果。
7. 改已决设计须在 `docs/adr/` 写原因、替代和影响，并同步主文档。不能暗中去掉通知、取消、Job、工具禁用或安全边界来让测试通过。
8. 所有 fixture 不需要真实 Token，不调用真实文件删除/Issue 创建等业务。

## 1. 验证基础与依赖（T01）

### 输入

`verification/sdkprobe/go.mod`、`probe_test.go` 及 SDK 原始源码。

### 操作

```powershell
# 在 verification/sdkprobe 工作目录
 go test -count=1 -v ./...
 go test -race -count=1 ./...
```

网络默认代理不可达时，可显式设置 GOPROXY=https://goproxy.cn,direct；这是网络镜像，不关闭校验数据库，不设置 GOSUMDB=off。记录替代代理使用原因。

建立根模块和 CLI skeleton。生产直接依赖仅 SDK 和平台进程所需 x/sys v0.40.0；JSON/HTTP/日志/轮询/CLI 优先标准库。依赖版本写入 go.mod/go.sum。

### 出口

- 根模块编译。
- 探针普通和 race 通过；环境无法运行 race 必须记录原因并在 Linux CI 补齐。
- README 明确单二进制仅网关无运行时依赖，下游仍需自身环境。

## 2. 配置与确定性导入（T02）

### 文件

internal/config、internal/cli，testdata/config。

### 实现

- 类型化结构，enabled 使用指针或自定义 optional bool。
- JSON token 扫描检测重复 key，Decoder.DisallowUnknownFields 检查未知字段，第二次 Decode 必须 EOF。
- 校验传输互斥字段、ID、URL、时长、大小。
- 原始 Config 和 ResolvedConfig 分开：原始配置用于保存，解析秘密值不能反写进配置或导出。
- env 只单次展开，支持字面量转义；测试 Windows 大小写覆盖。
- import dry-run、--yes、remote-type、冲突拒绝，绝不在 import 启动命令。
- 原子替换跨平台实现、锁文件、摘要冲突检测。

### 必测

缺/错 version、enabled=false、重复 key、尾随 JSON、未知字段、路径空格、缺变量、字面量变量、URL 无 type、重名 import、写入失败保持旧文件、同时写入冲突。

### 出口命令

`go test ./internal/config/... ./internal/cli/...`

## 3. 假下游与传输适配（T03）

建立可作为测试进程运行的假 MCP，不使用 npx 下载公共包来作为基础测试。

### fixture 模式

- echo_raw：捕获原始 JSON、大整数参数，返回普通文本及结构化结果。
- image：返回确定的图片字节和 MIME。
- fail_tool：返回 isError。
- block：等待 context.Cancel，通知测试端已进入 handler。
- counter：每次调用自增，用于检测重放。
- list_paged：至少两页、可切换目录、重复 cursor 故障模式。
- crash：调用中退出。
- exclusive：占一个本地端口，检测是否双开。
- spawn_child：生成常驻孙进程并写 PID 给测试，不通过业务 stdout 输出测试日志。

### 适配

封装 ClientSession，不让上层依赖传输细节。HTTP 自定义 RoundTripper 注入每服务 header，clone Request 后再修改；禁重定向。Client.Timeout 不覆盖 SSE 全生命周期。

区分生命周期 context 与启动预算：Connect 使用 startup context 的实际关闭语义先用测试验证；不得连接成功后 cancel startup context 就意外关闭整个会话。如果 SDK 会绑定该 context，使用长期 context，独立计时关闭未完成连接。

### 出口

HTTP 和 IOTransport 均能 initialize/list/call/close；非支持传输明确错误；无真实外部服务依赖。

## 4. Windows 进程管理先行（T04）

这是最高风险模块，不能留到项目末尾。

### 实现

按主文档 `_worker` + bootstrap 门闩 + Job Object 路线。用 x/sys/windows 创建 Job，KILL_ON_JOB_CLOSE，worker 加入成功后才写 bootstrap。Job 句柄不能继承。

worker 自己只负责下游启动、字节转发及退出协调；它不是第二个 Hub，不持配置目录、不提供 HTTP。限定 bootstrap 1 MiB/5s。

生产优先用 IOTransport：Reader/Writer 的 Close 回调委托统一 process owner，用 sync.Once 防止重复清理，关闭管道要能解除阻塞读写。

Node 标准布局解析独立函数，测试虚拟目录；实际 npx smoke test 为补充，不作为不确定的自动联网步骤。

### 必测

- bootstrap 未发出时没有真实下游 PID。
- Job 分配失败时无下游进程，worker 回收。
- 父 Hub 正常结束、强制终止、调用超时清理时，孙进程消失。
- 子进程异常结束后 Wait 只调用一次，goroutine 退出。
- 路径包含空格、Unicode，args 含引号/百分号/& 原样到达 native exe。
- 非标准 npx 给修复建议；普通 .bat 被拒绝；原生 uvx 不被误判。

用测试 pid/句柄判断退出，不只看 taskkill 退出码。测试设置总超时并在失败时兜底清理，不能把僵尸进程留给用户。

### 出口

Windows 原生进程树测试 PASS；POSIX process group 测试加入 Linux CI。只有交叉编译不算通过。

## 5. 目录、命名与 Publisher（T05）

### 实现

- 下游分页、cursor 防循环、工具定义/数量上限。
- 普通公共名与 SHA-256 稳定别名，碰撞拒绝。
- 注册前验证 inputSchema，SDK 对象不可变。
- 路由表、catalog revision、publishMu、tools/list middleware。
- SDK 原生 AddTool/RemoveTools 和通知；不用探针中的快照拦截示例替代主设计。

### 锁顺序

Publisher：publishMu → 更新路由 → SDK 内部锁。

Router：publishMu.RLock → generation.mu → 取得租用 → 释放 generation.mu → 释放 publishMu。

释放租用只取 generation.mu，不反向获取 publishMu。不要在 generation.mu 内调用 Publisher，否则产生反向锁序。

### 必测

同名、超长、非法字符、极端 schema、发布失败部分隔离、并发 list/发布不读半目录、禁用后旧名称调用拒绝。用 go test -race。

## 6. Manager 和 Router（T06）

### 必须建立的类型

```text
DesiredServer: revision + resolvedConfig
Generation: id + admission + active + session + cancel + closeOnce
Lease: generation 引用 + releaseOnce
Route: publicName + serverID + originalName
ServiceStatus: 纯标量安全字段
```

Route 不永久绑定旧 session；租用时取当前可用 generation。一次 Lease 在重载后仍绑定原 generation，不能偷偷迁移在途调用。

### 实现

- 每服务 coordinator，revision 校验异步结果。
- 启动并发 4，退避与抖动。
- 无等待队列并发许可。
- 准确 error mapping、原始参数、结果支持边界。
- 按主文档 break-before-make。
- 超时/取消不重放，SSE 恢复用 counter fixture 验证不会重复 POST。

### 必测

服务失败隔离、旧异步连接不能覆盖新配置、快速改 A→B→C、工具过滤不重启、独占资源不双开、admission 与 drain 同时发生、超时计数一次。

## 7. 对外 HTTP 与容量安全（T07）

### 实现

- `/mcp` stateful SDK handler，Tools.ListChanged=true，SessionTimeout=30min。
- 先绑定监听端口再启动 Manager。
- Host/Origin、拒绝代理头信任、无 CORS。
- POST 体 8 MiB，ReadHeaderTimeout 5s。
- initMu 串行化无 session POST 的容量检查和 SDK 初始化，已存在会话请求不持锁。
- 不设全局短 WriteTimeout；空闲按 SDK 新 HTTP 请求语义。
- 只读 healthz/readyz/status DTO。

### 单消息大小

stdio：bounded line reader，处理最后 EOF，拒绝单行超限。

HTTP 下游：RoundTripper response.Body 包装，application/json 单消息限长；text/event-stream 按空行 event 边界累计，支持 LF/CRLF，保持原字节顺序；每 event 重置，不对整条长连接累计。拒绝未处理的 Content-Encoding。测试分块跨边界、多行 data、超长无换行输入。

SDK 封装后如无法可靠限制某条解码路径，写失败测试并实现有界 Transport 适配，不能只在结果已经分配后检查大小冒充内存防护。

### 必测

32 会话边界及 33 个并发初始化、失败初始化不泄漏 slot、慢 init 请求有界结束、GET 不阻塞新调用、健康端点不泄露配置、SSE 连续多个合法 event 总量超过 8 MiB 仍正常。

## 8. stdio 桥接（T08）

### 实现

本地 SDK Server + 一个到 Hub 的 ClientSession。先拉目录再向本地客户端服务，使用 SDK 动态工具增删。

调用原始 publicName 不再加第二次前缀。桥接透传的目标是 Hub 公共名，不是 Hub 的下游原始名。

### 必测

客户端 → 桥接 IO → Hub HTTP → 假下游完整三段；两个桥接共用 Hub 时下游进程数不增加；两跳取消；Hub list_changed 到本地；Hub 会话失败桥接退出；stdout 全部可解析为 MCP 消息。

## 9. 热重载、诊断与完整 CLI（T09）

- 配置内容稳定两次，非法变更保旧有效配置。
- 周期发现与 list_changed 合并，不阻塞 SDK 回调。
- 所有协调事件有界缓冲；目录变化反复入队使用 dirty bit/coalescing，不无限积累。
- status/doctor 只读已运行 Hub，不离线另起下游。
- export stdio command 使用当前 exe 绝对路径；HTTP 模板不写未经确认的客户端字段。
- safe error category + request id；默认不展示原始 stderr/headers/params。
- 原始错误可能含秘密，不能仅做字符串替换后认定永不泄露；普通日志只记录白名单字段，debug 明确警告。

### 出口

新 MCP/新 Token/新路径只改 Hub；两客户端刷新后可用。listen 变化仅提示 restart_required，其他变更可应用。

## 10. 全量测试、打包、交接（T10）

### 自动化命令

在根模块：

```powershell
go fmt ./...
go vet ./...
go test -count=1 -timeout 120s ./...
go test -race -count=1 -timeout 180s ./...
go build -trimpath -ldflags="-s -w" -o dist/mcp-hub.exe ./cmd/mcp-hub
```

在 verification/sdkprobe 单独运行普通与 race 测试。Linux CI 运行 go test/race 和 POSIX 进程测试；Windows CI 运行原生 Job/npx 解析测试。用固定版本 govulncheck 做发布检查并记录版本，不能仅写“已检查”。

### 人工矩阵

| 对象 | OS/版本 | 接入 | list/call | 通知 | 结论 |
|---|---|---|---|---|---|
| Cursor | 实测填写 | 首先 stdio，HTTP 另测 | NOT_RUN | NOT_RUN | NOT_RUN |
| Claude Desktop | 实测填写 | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| 用户文件 MCP | 实测填写 | stdio | NOT_RUN | NOT_RUN | NOT_RUN |
| 用户远程 MCP | 实测填写 | Streamable HTTP | NOT_RUN | NOT_RUN | NOT_RUN |

缺少用户凭证或客户端，保留 NOT_RUN 和具体操作步骤，不伪造兼容性、不随意要求用户交出真实 Token。

### 发布文件

- README：安装、Hub 前台启动、客户端导出、退出、故障排查。
- config.example.json：无秘密，提供本机可理解示例。
- docs/CONFIG.md、COMPATIBILITY.md、SECURITY.md。
- docs/IMPLEMENTATION-STATUS.md：T01–T10 状态及证据。
- dist 产物与 SHA-256。
- benchmark 报告：硬件/OS/工具链/服务数量/工具数量/并发，Hub 自身与下游分别测量。

## 11. 交付门槛

- T01–T09 所有 P0 自动化通过，Windows 进程测试不能仅靠静态分析。
- T10 构建、漏洞检查和使用说明完成。
- 无遗留测试进程，无未收集的后台任务。
- 真实客户端未测时允许标记“自动化完成、人工兼容待验收”，不能标记完整产品已验收。
- 没有把 P0 写成 TODO 的空实现；没有为了通过测试修改断言掩盖行为缺失。

## 12. 给接手 Agent 的启动指令

> 阅读 MCP-Hub-开发设计文档.md v0.2、本文及 verification/sdkprobe/README.md。保持个人聚合路由定位，按 T01→T10 顺序实现生产项目。先复跑探针，创建 IMPLEMENTATION-STATUS；每任务完成跑测试并记录证据。SDK 固定 v1.4.1，使用 AddTool/RemoveTools、发布锁和自主管理进程，不自行重写协议。Windows worker/Job 属 P0。缺真实客户端记 NOT_RUN，不阻塞基础开发也不虚报兼容。不要修改 DeepSeek Harness，不增加团队/计费/UI 范围。完成前收集测试结果、清理进程、列出可执行文件和尚未验证的边界。
