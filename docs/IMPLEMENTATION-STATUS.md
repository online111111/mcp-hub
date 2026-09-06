# MCP Hub Implementation Status

Last updated: 2026-09-06 (independent acceptance audit on Windows amd64 plus native Debian 12 amd64 verification)

Status values: `NOT_STARTED`, `IN_PROGRESS`, `PASS`, `FAIL`, `NOT_RUN`.

| Task | Status | Evidence / notes |
|---|---|---|
| T01 verification, Go module and skeleton | PASS | `go.mod` uses Go 1.25.0 and SDK v1.4.1; root build and nested SDK probe ordinary/race tests pass with Go 1.26.6 on Windows amd64 |
| T02 strict config/import/atomic writes | PASS | `internal/config`, `internal/cli`; strict decoding, duplicate/unknown/trailing rejection, env/path resolution, import preview/CAS/atomic-write tests pass under race |
| T03 downstream transport adapter | PASS | `internal/downstream`; HTTP/IO sessions, raw args, bounded pagination, dirty callbacks, timeout separation and redirect/header policy pass under race |
| T04 Windows worker/Job process ownership | PASS | `internal/process`; production `_worker` dispatch, startup latch + kill-on-close Job Object, npx resolution, batch rejection, pipe delegation, descendant cleanup and single-Wait tests pass natively on Windows; release binary was exercised with a real stdio MCP child |
| T05 catalog/publisher | PASS | `internal/catalog`, `internal/inbound/publisher.go`; dynamic SDK AddTool/RemoveTools, naming/filtering, route snapshots and publish-lock tests pass under race |
| T06 manager/router | PASS | `internal/manager`, `internal/router`; generations, leases, backoff, break-before-make reload, raw routing, no-replay behavior, error mapping, and recent-call recording pass under race |
| T07 inbound HTTP/capacity/security | PASS | `internal/inbound/http.go`; loopback bind, stateful MCP, session/body limits, Host/Origin protection, hard capacity bounds and sanitized diagnostics pass under race |
| T08 stdio bridge | PASS | `internal/bridge`; three-hop forwarding, shared Hub, raw calls, dynamic directory sync, two-hop cancellation, Hub failure exit and stdout purity pass; bridge stress `go test -race -count=20 ./internal/bridge/...` PASS |
| T09 hot reload/diagnostics/complete CLI | PASS | Reload loop applies only changed content after two stable samples, keeps prior runtime on invalid config, flags listen changes as restart-required, records bounded recent-call summaries through the production adapter, and wires serve/stdio/validate/import/export/status/doctor |
| T10 full tests/build/release | PASS* | Windows release checks pass; native Debian 12 amd64 root ordinary/race tests, vet, SDK probe race test, build, HTTP/MCP smoke test, and POSIX descendant-cleanup tests pass with Go 1.26.6. GitHub Actions now covers Ubuntu and Windows; real GUI client matrix remains NOT_RUN. |

`*` means automated Windows release checks pass; this does not mean every manual client or OS compatibility check is complete.

## Commands and evidence

Root module (`D:\\data\\deepseekharness\\space2`):

```powershell
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 180s ./...
go vet ./...
go build -trimpath -ldflags="-s -w" -o dist\\mcp-hub.exe .\\cmd\\mcp-hub
govulncheck ./...
```

Nested SDK probe (`verification/sdkprobe`):

```powershell
go test -count=1 -timeout 90s ./...
go test -race -count=1 -timeout 90s ./...
```

Release artifact:

- `dist/mcp-hub.exe`
- Windows amd64 SHA-256: `EE07BCD77F89304418A8915AEDA7E73FEBD7D91FAD5077C81E7FA438D1A588EF`
- Linux amd64 cross-build SHA-256: `06FCB8825B4612721914E41EBD9E4E5B47C0CC3BBBCA1E773A99B3A6E210F609`
- `dist/SHA256SUMS.txt`

The release scan used Go 1.26.6 and `govulncheck@v1.7.0`. The checked-in module directive remains `go 1.25.0`; the build toolchain is an operational choice. The previous Go 1.26.3 scan had standard-library findings fixed by Go 1.26.4–1.26.6, so release checks must use the patched toolchain. `golang.org/x/sys` is pinned to v0.44.0 to include the Windows fix for GO-2026-5024.

## Independent acceptance audit

The acceptance pass found and corrected a release-blocking Windows defect: the production binary did not dispatch its private `_worker` mode, although package tests did. Additional remediation covered diagnostic error categorization, production recent-call wiring, remove-before-drain lifecycle ordering, concurrent removal drains, stable two-sample reloads, IPv6 endpoint reporting, all-method request-body bounds, readiness-aware `doctor`, Claude Desktop transport validation, and non-object output schemas.

A live native smoke test then built a real stdio MCP child, started the release candidate with that child, initialized over Streamable HTTP, listed `fixture__ping`, called it successfully (`pong`), and confirmed the sanitized recent-call record in `/api/v1/status`. The temporary fixture files were removed after the check.

A final reliability pass also made generation shutdown cancel in-flight downstream call contexts, fixed the bootstrap timeout reader-goroutine leak for closable inputs, and added race-tested regressions for both behaviors. The final ordinary/race/vet/SDK-probe/vulnerability suite was rerun after these changes.

## Embedded public administration extension

The single binary now embeds a responsive `/admin/` management page with authenticated status viewing and atomic server add/edit/delete operations. Public mode is explicit and fail-closed: it requires an HTTPS public URL, Host allowlist, MCP bearer token, separate admin token, and trusted-proxy CIDRs. The HTTP layer requires TLS or a trusted proxy's `X-Forwarded-Proto: https`, protects public MCP/diagnostic endpoints with bearer authentication, and keeps browser requests confined to the admin origin. The admin plane adds bounded random sessions, Secure/HttpOnly/SameSite cookies, synchronizer CSRF tokens, JSON-only mutations, login/API rate limits, secret placeholders, ETag/CAS writes, CSP and browser security headers. See `docs/VPS.md` and `config.vps.example.json`.

Windows amd64 was tested natively. Debian 12 amd64 was also tested natively on Linux with Go 1.26.6: the root ordinary/race suites, vet, nested SDK probe race test, static build, HTTP/MCP smoke test, explicit Process.Close descendant cleanup, and context-cancellation descendant cleanup all passed. The Linux verification also found and fixed a real cancellation bug where `exec.CommandContext` could kill only the direct child and leave grandchildren alive; POSIX cancellation now routes through `Process.Close` so the full process group is terminated.

## Evidence boundary and not-run checks

The SDK probe is feasibility evidence only. Automated tests do not prove arbitrary third-party server behavior, malicious same-user processes, deliberate process-group escape on POSIX, or compatibility with a GUI client. Native Windows and Debian 12 amd64 process tests were run. macOS runtime remains unverified; GitHub Actions provides ongoing Ubuntu/Windows regression coverage.

| Object | Status | Reason |
|---|---|---|
| Cursor exact installed version | NOT_RUN | No installed client/version was supplied for manual exercise |
| Claude Desktop exact installed version | NOT_RUN | No installed client/version was supplied for manual exercise |
| User file MCP | NOT_RUN | No user server supplied |
| User remote MCP | NOT_RUN | No remote service/token supplied |
| Linux native runtime and POSIX process-group tests | PASS | Debian 12 amd64 native ordinary/race/vet/build/smoke checks pass; explicit close and context-cancel descendant cleanup are regression-tested |
| macOS build/runtime | NOT_RUN | No macOS runner in this workspace |
