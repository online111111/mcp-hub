请在当前这台客户端电脑上把本机 AI/MCP 客户端接入已经运行的 MCP Manager。

MCP Endpoint:
{{MCP_ENDPOINT}}

MCP Access Token:
{{MCP_TOKEN}}

要求：
1. 不要部署、升级、重启或修改远端 MCP Manager 服务端。
2. 先检查当前电脑已经安装并实际使用的 MCP 客户端，优先识别 Codex、Claude Code / Claude Desktop、Cursor、OpenCode、VS Code 或其他兼容 MCP 的客户端。
3. 读取现有客户端配置，备份后再修改；不得覆盖或删除其它已有 MCP 服务。
4. 客户端原生支持远程 / Streamable HTTP MCP 时，优先直接使用上面的 Endpoint 和 Token。
5. 如果客户端只支持 stdio，则使用本机 `mcp-manager stdio --connect <endpoint> --token <token>` 作为桥接；先确认本机已有可执行文件。
6. 尽量通过客户端支持的环境变量/Secret 机制保存 Token，避免把 Token 输出到日志、终端历史、截图或最终汇报中。
7. 修改后检查配置语法并重新加载客户端。
8. 实际验证 MCP Manager 能连接、能列出工具，并在安全可行时完成一次无副作用的代表性调用。
9. 最后只告诉我：配置了哪个客户端、修改了哪个本地配置文件、使用 HTTP 还是 stdio bridge、验证是否成功。Token 必须脱敏。

请直接执行配置，不要只给我一份操作步骤。
