# MCP Manager implementation status

Last updated: 2026-09-08 for the v0.4.0 release candidate after repository/module migration and documentation reorganization.

Status values are `PASS`, `PARTIAL`, and `NOT_RUN`. `PASS` means the stated contract has direct automated/platform evidence; it is not universal certification of every client, downstream, proxy, or OS release.

## Current implementation

| Area | Status | Evidence / notes |
|---|---|---|
| Product/repository identity | PASS | product `MCP Manager`, repository `online111111/mcp-manager`, module `github.com/online111111/mcp-manager`, CLI/Release prefix `mcp-manager`, UI and Skill aligned |
| Strict configuration | PASS | strict decoding, bounded reads/digests, validation, one-pass env expansion, path resolution, CAS, durable atomic writes, OS-backed locking |
| Configuration compatibility | PASS | JSON `hub.*` and `/mcp`/`/admin` deliberately unchanged |
| Downstream stdio | PASS | direct command/args, safe compatibility env, explicit secret forwarding, cancellation, POSIX process groups, Windows Job Objects |
| Downstream Streamable HTTP | PASS | SDK v1.7.0, HTTPS policy, redirect rejection, header restrictions, bounded handling, session/cancellation behavior |
| Protocol boundary | PARTIAL | documented stateful/session-oriented compatibility model; complete native coverage of every MCP 2026-07-28 capability is not claimed |
| Catalog/router/runtime | PASS | deterministic publication, revisions, generations, leases, backoff, no-replay calls, sanitized diagnostics, graceful drain |
| stdio bridge | PASS | authenticated forwarding, dynamic tool sync, safe endpoint policy, stdout purity |
| Browser Admin | PASS | auth/session/rates, CSRF, strict JSON, secret placeholders, ETag/CAS, preflight, rollback, security headers, Chromium regressions + real local smoke |
| Remote Admin CLI | PASS | `list/get/add/edit/delete`, session + CSRF, redacted reads, ETag/CAS, HTTPS policy, redirect blocking, delete confirmation, bounded server JSON |
| Credential compatibility | PASS | `MCP_MANAGER_TOKEN` / `MCP_MANAGER_ADMIN_TOKEN` primary; historical `MCP_HUB_TOKEN` / `MCP_HUB_ADMIN_TOKEN` remain bounded runtime fallback |
| Agent deployment assets | PASS | embedded `mcp-manager-deployer` Skill, authenticated ZIP, current prompt, installer targets `online111111/mcp-manager` |
| CI / vulnerability gate | PASS | Linux/Windows/macOS × Go 1.25.8/1.27.1, current-Go race + SDK probe, Linux Chromium + real-Manager smoke, `govulncheck`, pinned Actions, repository identity checks |
| Release packaging | PARTIAL | workflow builds six `mcp-manager-*` archives plus `SHA256SUMS`; formal `v0.4.0` Release is still pending |
| Branch protection / ruleset | PARTIAL | main currently lacks enforced protection/ruleset |
| License | PARTIAL | no LICENSE selected yet; owner/legal decision remains pending |

## Current baseline

- product: **MCP Manager**
- repository: `online111111/mcp-manager`
- Go module: `github.com/online111111/mcp-manager`
- binary/CLI: `mcp-manager`
- source version default: `0.4.0`
- Go directive: `1.25.0`
- CI: Go `1.25.8` and `1.27.1`
- MCP Go SDK: `v1.7.0`
- primary MCP token env: `MCP_MANAGER_TOKEN`
- primary Admin token env: `MCP_MANAGER_ADMIN_TOKEN`
- runtime compatibility envs: `MCP_HUB_TOKEN`, `MCP_HUB_ADMIN_TOKEN`
- deployer Skill: `mcp-manager-deployer`
- Release prefix: `mcp-manager-`

## Compatibility contract

Canonicalized: product/UI/build identity, GitHub repository slug, Go module/import path, CLI/release binary, generated client token env, Release archives, and deployment Skill/install flow.

Preserved: JSON schema including `hub.*`, `/mcp`, `/admin`, `/api/...`, runtime/Admin transaction semantics, stored config/secrets, and historical token variables as v0.4 runtime fallback.

The duplicate legacy source entrypoint is removed. CI enforces the canonical module path and prevents stale public repository/product identities from returning to the current code tree.

## Verification commands

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

SDK probe:

```bash
cd verification/sdkprobe
go test -race -count=1 -timeout 120s ./...
```

Browser regression/integration:

```bash
cd internal/admin/browsertest
npm ci
npx playwright install --with-deps chromium
npm test
cd ../../..
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node internal/admin/browsertest/live-smoke.mjs
```

## Evidence boundary

| Environment/client | Status |
|---|---|
| Linux | PASS |
| Windows | PASS |
| macOS | PASS |
| Browser Admin loopback | PASS |
| Remote Admin CLI | PASS |
| Cursor exact version | NOT_RUN |
| Claude Desktop exact version | NOT_RUN |
| Arbitrary third-party MCP servers | NOT_RUN |
| Exhaustive reverse-proxy/TLS matrix | PARTIAL |

## Release/governance remaining work

Before calling v0.4.0 a formal release:

1. confirm the final `main` CI after documentation organization is green;
2. make an explicit decision on the stale dependency update PR if it remains open;
3. create the intended exact `v0.4.0` tag;
4. verify all six `mcp-manager-*` archives and `SHA256SUMS` are published.

Recommended governance follow-up: enable main protection/required CI, choose a LICENSE if public reuse is intended, and prune obsolete branches after semantic verification.

## Related public documentation

- [README](../../README.md)
- [Configuration](../../docs/CONFIG.md)
- [Architecture](../../docs/ARCHITECTURE.md)
- [Security](../../docs/SECURITY.md)
- [Compatibility](../../docs/COMPATIBILITY.md)
- [Remote Admin](../../docs/REMOTE-ADMIN.md)
- [VPS deployment](../../docs/VPS.md)
