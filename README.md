# MCP Hub

MCP Hub is a personal MCP aggregation and request-routing gateway: configure downstream MCP servers once, then connect multiple clients to the Hub.

> Current state: P0 implementation and automated verification are complete on Windows amd64. Actual Cursor/Claude Desktop compatibility is still `NOT_RUN`; see [`docs/IMPLEMENTATION-STATUS.md`](docs/IMPLEMENTATION-STATUS.md).

## Quick start

1. Create a JSON configuration file. [`config.example.json`](config.example.json) is a safe starting point.
2. Validate it without starting processes or making network calls:

   ```powershell
   .\mcp-hub.exe validate --config .\config.json
   ```

3. Start the Hub:

   ```powershell
   .\mcp-hub.exe serve --config .\config.json
   ```

   The Hub binds a loopback address and exposes the stateful MCP endpoint at `/mcp`.
4. Configure HTTP-capable clients with `http://127.0.0.1:8080/mcp`, or export a stdio entry:

   ```powershell
   .\mcp-hub.exe export --client cursor --transport stdio --endpoint http://127.0.0.1:8080/mcp
   ```

   The exported stdio command runs `mcp-hub stdio --connect ...`; it connects to the existing Hub and does **not** start downstream MCP processes.

Useful read-only commands:

```powershell
.\mcp-hub.exe status --endpoint http://127.0.0.1:8080
.\mcp-hub.exe doctor --endpoint http://127.0.0.1:8080
```

`import` supports `--dry-run` and requires `--yes` for a write. It never starts a downstream service during import.

## Configuration and security

- Configuration is strict JSON: duplicate keys, unknown fields, trailing JSON, invalid transport fields, and invalid URLs are rejected.
- New tools are open by default. Use `tools.disabled` with original downstream tool names for a declarative blacklist.
- Environment references use one-pass `${NAME}` expansion; `$${NAME}` is literal. Secrets are resolved only in memory and are not exported by diagnostics.
- The Hub listens on loopback only, rejects requests with `Origin`, checks `Host`, disables redirects for downstream HTTP, caps POST bodies at 8 MiB, and limits upstream sessions.
- Tool calls are routed by an explicit table, forwarded with raw JSON arguments, and are never automatically replayed after timeout or cancellation.

See [`docs/CONFIG.md`](docs/CONFIG.md), [`docs/SECURITY.md`](docs/SECURITY.md), [`docs/COMPATIBILITY.md`](docs/COMPATIBILITY.md), and [`docs/VPS.md`](docs/VPS.md) for operational details.

## Embedded management page

Enable `hub.admin` to use the built-in responsive management page at `/admin/`. For a public VPS, explicitly enable `hub.publicMode`, configure an HTTPS `publicUrl`, an `allowedHosts` list, separate MCP/admin bearer tokens, and a narrowly scoped `trustedProxies` list. A safe reverse-proxy example is provided in [`config.vps.example.json`](config.vps.example.json) and [`docs/VPS.md`](docs/VPS.md).

## Development and verification

The production module targets Go 1.25 and pins the official MCP Go SDK at v1.4.1. The Hub itself is a single binary and does not require Node.js or Python; a stdio downstream may still require its own runtime (Node.js, Python, uv, or another executable).

```powershell
go fmt ./...
go vet ./...
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 180s ./...
go build -trimpath -ldflags="-s -w" -o dist\mcp-hub.exe .\cmd\mcp-hub
```

The SDK feasibility probe is an independent nested module and must be run from its own directory:

```powershell
Push-Location .\verification\sdkprobe
$env:GOPROXY='https://goproxy.cn,direct'
go test -race -count=1 ./...
Pop-Location
```

Do not treat a cross-compiled binary as proof of native process ownership. Windows Job Object tests are native; POSIX process-group tests require Linux CI. See [`docs/IMPLEMENTATION-STATUS.md`](docs/IMPLEMENTATION-STATUS.md) for evidence boundaries and remaining manual checks.
