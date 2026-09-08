MCP Manager for Windows

Local desktop use
1. Extract the complete ZIP into a private writable directory.
2. Double-click start-mcp-manager.cmd (not mcp-manager.exe).
3. First run creates UTF-8 config.json without BOM and a random Admin Token.
   If clipboard copying succeeds, paste the token into the browser login page.
4. For an existing config, double-click copy-admin-token.cmd to copy its inline
   Admin Token. Environment-reference credentials must be retrieved from their
   configured environment; the helper will not copy a placeholder as a token.
5. Keep the console open while using the service. For persistent server use,
   configure a supported Windows service supervisor separately.

Existing config.json is preserved, including supported UTF-8 BOM files. The
launcher honors the configured loopback port and opens Admin even if a downstream
is temporarily unavailable. It does not rewrite config.json on every restart.
Do not delete config.json during upgrades: it stores your servers and credentials.
Stop the old process, back up the directory, then replace the release program files.

Configuring MCP access tokens now protects /mcp and diagnostics even in local mode.
Clients and doctor/status must supply a valid MCP token. /healthz stays public.
Admin Token and MCP Token are distinct credentials. The Admin login page does not
provide any unauthenticated API to reveal either token.

Security
Do not share config.json, screenshots containing revealed tokens, or clipboard
history. Other local applications and clipboard synchronization may retain copied
secrets. Clear the clipboard/history after use. Only configure trusted downstream
programs; they run with the same OS privileges as MCP Manager, not in a sandbox.
Use a non-administrator account and a private configuration directory.

The launcher is for local loopback use. Public HTTPS deployments and headless
configurations should use explicit mcp-manager.exe commands and the production
runbook in docs/PRODUCTION.md. Browser-opening failures do not stop a healthy
service; open the printed Admin URL manually. Startup errors remain visible.
