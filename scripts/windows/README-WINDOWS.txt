MCP Manager for Windows

Double-click start-mcp-manager.cmd for local use.
On first run it creates config.json bound to 127.0.0.1:8080, enables the Admin UI, creates a random Admin Token, copies that token to the Windows clipboard, starts MCP Manager, and opens http://127.0.0.1:8080/admin/.

Paste the clipboard value into the Admin Token field on the login page.
If you need the Admin Token again later, double-click copy-admin-token.cmd. It reads hub.admin.token from the local config.json and copies it to the clipboard without printing the token.

The Admin Token is intentionally not printed in console logs. You can also find it manually in config.json under hub.admin.token.

For advanced/server use, run mcp-manager.exe from PowerShell with explicit commands and configuration. Server deployments should prefer environment-backed credentials instead of inline local-first-run tokens.
