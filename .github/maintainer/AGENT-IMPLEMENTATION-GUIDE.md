# MCP Manager: Agent maintenance and implementation guide

This document is for coding agents, operations agents, and human maintainers. It describes the current v0.4.0 release-candidate maintenance, verification, deployment, compatibility-upgrade, and release rules.

For the short entrypoint, read [`../../AGENTS.md`](../../AGENTS.md) first.

## Read next as needed

1. `README.md` / `README.zh-CN.md`
2. `.github/maintainer/DEVELOPMENT-DESIGN.md`
3. `docs/CONFIG.md`
4. `docs/SECURITY.md`
5. `docs/COMPATIBILITY.md`
6. `.github/maintainer/IMPLEMENTATION-STATUS.md`
7. Remote Admin: `docs/REMOTE-ADMIN.md`
8. Deployment: `docs/VPS.md`
9. SDK behavior: `verification/sdkprobe/README.md`

Current baseline: Go 1.25+, MCP Go SDK v1.7.0, product MCP Manager, CLI `mcp-manager`, module `github.com/online111111/mcp-manager`, one Go production binary, Browser Admin + Remote Admin + `mcp-manager-deployer` Skill.

## Handoff rules

- Re-check current main/PR/CI before acting; do not treat an old summary as authority.
- Reproduce before fixing where practical; add a regression for defects.
- Do not weaken assertions or security checks just to make CI pass.
- Documentation marked PASS is not a substitute for executed evidence.
- SDK v1.7.0 does not imply complete MCP 2026-07-28 support.
- MCP Manager is not a sandbox; stdio downstreams are trusted code.
- Never expose secrets in logs, PRs, screenshots, or reports.
- Prefer Browser/Remote Admin for normal downstream CRUD so CAS/preflight remains in the path.
- Public deployment defaults to loopback + HTTPS reverse proxy + non-root service user.

## v0.4 compatibility rules

Canonical identity:

- repository: `online111111/mcp-manager`
- Go module: `github.com/online111111/mcp-manager`
- CLI/binary: `mcp-manager`
- primary token variables: `MCP_MANAGER_TOKEN`, `MCP_MANAGER_ADMIN_TOKEN`
- Skill: `mcp-manager-deployer`
- Release prefix: `mcp-manager-`

Compatibility intentionally retained:

- `MCP_HUB_TOKEN` and `MCP_HUB_ADMIN_TOKEN` runtime fallback
- JSON `hub.*`
- `/mcp`, `/admin`, and `/api/...` routes

The duplicate legacy source CLI entrypoint is removed. New code must not reintroduce the old repository slug, old binary name, or old public product name. Repository-identity CI protects that boundary, while historical environment-variable names are explicit compatibility exceptions.

Do not rename a working systemd service or data directory merely for visual consistency. Runtime continuity comes first; operational path migration should be explicit, backed up, and verified.

## Main directories

```text
cmd/mcp-manager/             primary CLI entrypoint
internal/admin/              Admin API/UI/Agent assets
internal/bridge/             stdio bridge
internal/cli/                CLI + Remote Admin
internal/config/             strict config/lock/atomic persistence
internal/downstream/         downstream MCP sessions
internal/inbound/            /mcp and diagnostics
internal/process/            cross-platform process tree
internal/runtime/            revision/reload/transaction
docs/                        user/operator-facing docs
.github/maintainer/          maintainer design/evidence docs
verification/sdkprobe/       independent SDK evidence
```

## Core-change checks

Runtime/downstream: generation, lease, cancellation, no-replay, catalog revision, timeout/concurrency/backoff, shutdown drain.

stdio/process: POSIX process groups, Windows Job Objects, safe environment inheritance, no implicit Manager/Admin secret inheritance, and no implicit shell interpolation for normal command/args.

HTTP/MCP: HTTPS, redirect policy, headers, Host/Origin, body/response bounds, session deadlines, sanitized errors, compatibility protocol boundary.

Admin: auth/session, same-origin/CSRF, ETag/CAS, strict JSON, 1 MiB Admin body bound, preflight, OS lock, atomic persistence, reload, rollback. Slow request bodies must be read before acquiring the shared config transaction lock.

UI: no external production runtime/CDN, mobile coverage, keyboard focus, ARIA, reduced motion, editor-open ETag, and atomic config+ETag refresh publication.

Remote Admin: MCP/Admin token separation, remote HTTPS, redirect rejection, session+CSRF, ETag/CAS, redaction, bounded input/output.

## Verification

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

Browser:

```bash
cd internal/admin/browsertest
npm ci
npx playwright install --with-deps chromium
npm test
cd ../../..
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node internal/admin/browsertest/live-smoke.mjs
```

## Deployment / upgrade

1. Inventory OS/arch/current version/service/proxy.
2. Back up binary/config/env/service/proxy.
3. Obtain a pinned Release + `SHA256SUMS`, or build an exact commit.
4. Validate the existing config with the candidate binary.
5. Atomically replace or stage alongside the old binary.
6. Restart.
7. Run `status` and `doctor`.
8. Verify `/admin/` and a representative client path where available.
9. Roll back on regression.
10. Report version, paths, endpoint and backup location without credential values.

## Skill

The embedded deployment Skill lives at `internal/admin/agent-skill/` and is published as `mcp-manager-deployer`. When it changes, validate its complete file structure, scripts, and packaged `skill.zip` so the Admin download remains aligned with the binary and documentation.

## PR / Release

Prefer short, single-purpose branches. Verify final-head CI and expected head SHA before merge. For stacked PRs, merge in dependency order and re-check each remaining base/diff/CI after every merge.

Before formal v0.4.0: final main CI must be green, documentation must match the final head, dependency PRs must have an explicit decision, repository/module identity must be consistent, the intended exact `v0.4.0` tag must exist, the Release workflow must succeed, and all six `mcp-manager-*` archives plus `SHA256SUMS` must be present.

Final reports should say that no known release-blocking issue was found within the audited and executed verification scope; do not claim absolute bug-freedom.
