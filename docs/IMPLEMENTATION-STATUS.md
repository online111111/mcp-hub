# MCP Manager implementation status

Last updated: 2026-09-08 for the v0.4.0 release candidate and MCP Hub -> MCP Manager rename.

Status values are `PASS`, `PARTIAL`, and `NOT_RUN`. `PASS` means the stated contract has direct automated/platform evidence; it is not a universal certification of every client, downstream, proxy, or OS release.

## Current implementation

| Area | Status | Evidence / notes |
|---|---|---|
| Product rename | PASS | public identity `MCP Manager`, new `mcp-manager` command entrypoint, new Release asset naming, new Admin UI/Skill branding; legacy source entrypoint and token variables retained for migration |
| Strict configuration | PASS | strict decoding, bounded reads/digests, validation, one-pass env expansion, path resolution, CAS, durable atomic writes, OS-backed locking |
| Configuration compatibility | PASS | existing JSON `hub.*` schema and `/mcp`/`/admin` routes deliberately unchanged by product rename |
| Downstream stdio | PASS | direct command/args, safe compatibility env, explicit secret forwarding, cancellation, POSIX process groups, Windows Job Objects |
| Downstream Streamable HTTP | PASS | SDK v1.7.0, HTTPS policy, redirect rejection, header restrictions, bounded handling, session/cancellation behavior |
| Protocol boundary | PARTIAL | stateful/session-oriented compatibility model documented; full native coverage of every MCP 2026-07-28 capability is not claimed |
| Catalog/router/runtime | PASS | deterministic publication, revisions, generations, leases, backoff, no-replay calls, sanitized diagnostics, graceful drain |
| stdio bridge | PASS | authenticated forwarding, dynamic tool sync, safe endpoint policy, stdout purity |
| Browser Admin | PASS | auth/session/rates, CSRF, strict JSON, secret placeholders, ETag/CAS, preflight, rollback, security headers, Chromium regressions + real local integration smoke |
| Remote Admin CLI | PASS | `list/get/add/edit/delete`, session + CSRF, redacted reads, ETag/CAS, HTTPS policy, redirect blocking, delete confirmation, bounded server JSON |
| Credential rename compatibility | PASS | `MCP_MANAGER_TOKEN` preferred over legacy `MCP_HUB_TOKEN`; new Admin token name is mapped to legacy-compatible Remote Admin path; existing old names remain usable during migration |
| Agent deployment assets | PASS | embedded `mcp-manager-deployer` Skill, authenticated ZIP, migration-aware prompt, release installer with new-repo-first / old-repo fallback |
| CI / vulnerability gate | PASS | Linux/Windows/macOS × Go 1.25.8/1.27.1, current-Go race + SDK probe, Linux Chromium + real-Manager smoke, `govulncheck`, pinned Actions |
| Release packaging | PARTIAL | workflow builds six `mcp-manager-*` archives plus SHA256SUMS; no formal `v0.4.0` Release has been published yet |
| Repository rename | PARTIAL | code/product is prepared for `online111111/mcp-manager`; GitHub repository slug itself still requires repository-administration rename |
| Go module path | PARTIAL | internal module path remains `mcp-hub` during v0.4 to avoid a high-risk import-only rewrite; this does not affect public binary/product identity |
| Branch protection / ruleset | PARTIAL | main currently lacks enforced protection/ruleset |
| License | PARTIAL | no LICENSE selected yet; owner/legal decision remains pending |

## Current baseline

- product name: **MCP Manager**
- official binary/CLI: `mcp-manager`
- source version default: `0.4.0`
- Go directive: `1.25.0`
- CI: Go `1.25.8` and `1.27.1`
- MCP Go SDK: `v1.7.0`
- new MCP token env: `MCP_MANAGER_TOKEN`
- new Admin token env: `MCP_MANAGER_ADMIN_TOKEN`
- legacy CLI token envs: `MCP_HUB_TOKEN`, `MCP_HUB_ADMIN_TOKEN` during migration
- official deployer Skill: `mcp-manager-deployer`
- official release prefix: `mcp-manager-`

## Rename contract

Changed:

- product/UI/build identity
- official CLI and release binary name
- generated client key and token env name
- Release archive names
- deployment Skill/prompt/install workflow
- new installation examples and service names

Preserved:

- JSON schema, including `hub.*`
- `/mcp`, `/admin/`, `/api/...` paths
- runtime/admin transaction semantics
- existing stored secrets/config files
- legacy CLI token variables during compatibility migration
- legacy `cmd/mcp-hub` source entrypoint in CI for the transition

The internal Go module path stays `mcp-hub` for v0.4. A module-path change is intentionally separated from the public rename so a branding migration does not create a repository-wide import rewrite and unrelated compatibility risk.

## Verification commands

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
go build -trimpath ./cmd/mcp-hub
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

1. merge the documentation/rename stack in order;
2. confirm final main CI is green;
3. make an explicit decision on stale Dependabot PR #10;
4. rename the GitHub repository slug to `mcp-manager` when repository-admin access is available;
5. create exact `v0.4.0` tag on the intended commit;
6. verify all six `mcp-manager-*` archives and `SHA256SUMS` are published.

Recommended governance follow-up: enable main protection/required CI, choose a LICENSE if public reuse is intended, and prune obsolete branches after semantic verification.

## Related documentation

- [README](../README.md)
- [Configuration](CONFIG.md)
- [Architecture](ARCHITECTURE.md)
- [Security](SECURITY.md)
- [Compatibility](COMPATIBILITY.md)
- [Remote Admin](REMOTE-ADMIN.md)
- [VPS deployment](VPS.md)
