# MCP Manager development design

- Document version: 0.4
- Product stage: v0.4.0 release candidate
- Current implementation: Go 1.25+, MCP Go SDK v1.7.0
- Repository: `online111111/mcp-manager`
- Go module: `github.com/online111111/mcp-manager`
- Product: lightweight self-hosted MCP gateway, downstream manager, and Admin console

This file records maintainer-level design decisions. User/operator contracts live under `docs/` and take precedence for public behavior.

## Product goals

1. Configure downstream MCP services once and reuse them across clients.
2. Support local stdio and remote Streamable HTTP downstreams.
3. Serve both native HTTP MCP clients and stdio-only clients.
4. Make configuration mutation strict, CAS-protected, preflighted, atomic, reloadable, and rollback-safe.
5. Fail closed for public deployment.
6. Keep Browser Admin, Remote Admin CLI, and Agent deployment tooling on the same management semantics.
7. Preserve a single-Go-binary production shape.

## Implemented surface

Implemented: strict configuration, stdio/HTTP downstream management, tool discovery/publication/routing, cancellation/timeouts/concurrency/backoff, cross-platform process-tree handling, hot reload, Admin UI, Remote Admin CRUD, stdio bridge, status/doctor, public security policy, `mcp-manager-deployer`, multi-OS CI, and Release workflow.

Not claimed: general resources/prompts/sampling/roots/elicitation/task extension proxying, complete native MCP 2026-07-28 coverage, multi-tenant/RBAC/billing, sandboxing, universal third-party client/server compatibility, or automatic replay of potentially side-effecting tool calls.

## Stable compatibility contract

Canonical identity:

- product: MCP Manager
- repository: `online111111/mcp-manager`
- module: `github.com/online111111/mcp-manager`
- CLI/binary: `mcp-manager`
- primary env: `MCP_MANAGER_TOKEN`, `MCP_MANAGER_ADMIN_TOKEN`
- Release prefix: `mcp-manager-`
- Skill: `mcp-manager-deployer`

Compatibility retained deliberately:

- `/mcp`, `/admin/`, `/api/...`
- JSON `hub.*`
- downstream runtime/routing semantics
- Admin transaction semantics
- `MCP_HUB_TOKEN` / `MCP_HUB_ADMIN_TOKEN` as bounded runtime fallback
- existing configuration environment-variable references

The duplicate old CLI source entrypoint is removed. CI enforces the canonical module/repository/product identity while allowing the explicit historical environment-variable compatibility names.

## Architecture

```text
HTTP client -------------------------------+
                                            |
stdio client -> mcp-manager stdio ---------+--> /mcp -> Publisher -> Catalog -> Router
                                                                      |
                                                                      v
                                                               Downstream Manager
                                                                /           \
                                                             stdio     Streamable HTTP
```

Management plane:

```text
Browser /admin/ ------+
                       |
mcp-manager admin -----+--> Admin API -> auth/session/CSRF -> ETag/CAS
                       |                                -> validate/preflight
Agent/Skill -----------+                                -> atomic persist/reload
                                                        -> rollback on failure
```

Disk JSON remains the persistence source of truth.

## Downstream and call semantics

Normal stdio command/args are not shell-interpolated. Children inherit a safe compatibility environment baseline; additional credentials require explicit server `env`. POSIX uses process groups; Windows uses worker + Job Object handling.

Remote non-loopback Streamable HTTP must use HTTPS, redirects are rejected, and configured headers cannot override MCP/session framing headers.

Tool calls use an explicit public-name -> downstream/original-tool route. The runtime does not guess a destination and does not automatically replay failed calls.

## Reload and revision model

Connection-level downstream changes use generation replacement. Calls already admitted keep the old generation lease. Listener/auth/Admin changes that are startup-bound mark `restartRequired` and are not considered fully applied until process restart.

## Admin safety

Browser and CLI Admin share authentication/session handling, CSRF, strict JSON, secret placeholders, ETag/CAS, preflight, config locking, atomic persistence, runtime reload, and rollback.

Admin PUT reads and bounds the network body before acquiring the shared configuration transaction lock so a slow authenticated upload cannot block runtime polling while holding that lock.

## Public deployment

Recommended topology:

```text
Internet -> HTTPS reverse proxy -> 127.0.0.1:8080 MCP Manager
```

Only necessary 80/443 should be public. Do not expose 8080 directly. Keep MCP/Admin credentials distinct and run Manager under a dedicated non-root account where practical.

## Verification baseline

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

CI covers Linux/Windows/macOS, Go 1.25.8/1.27.1, current-Go race, SDK probe, Chromium regressions, real-Manager smoke, govulncheck, and repository identity checks.

## Maintainer sources of truth

- `AGENTS.md`
- `README.md` / `README.zh-CN.md`
- `docs/ARCHITECTURE.md`
- `docs/CONFIG.md`
- `docs/SECURITY.md`
- `docs/COMPATIBILITY.md`
- `docs/REMOTE-ADMIN.md`
- `.github/maintainer/IMPLEMENTATION-STATUS.md`
