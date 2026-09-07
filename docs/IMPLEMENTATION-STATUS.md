# MCP Hub implementation status

Last updated: 2026-09-07 for the v0.3.1 audit-hardening branch.

Status values are `PASS`, `PARTIAL`, and `NOT_RUN`. A passing automated test is
evidence for the tested contract, not a claim about every third-party server or
client.

## Current implementation

| Area | Status | Evidence / notes |
|---|---|---|
| Strict configuration | PASS | size bounds after decode entry, exact case-sensitive field names, duplicate/unknown/trailing input rejection, validation, environment expansion, path resolution, import preview, CAS, and atomic writes |
| Shared configuration loading | PASS | CLI and live runtime use shared strict loading; loading has no network or process side effects |
| Downstream transports | PARTIAL | stdio and Streamable HTTP sessions, redirect/header policy, raw arguments, pagination, and cancellation are covered; the pinned SDK v1.4.1 still lacks later transport response/event hardening and protocol support targeted for a separate upgrade |
| Process ownership | PASS | native POSIX process groups and Windows Job Object lifecycle implementations; Windows context cancellation now closes the Job tree and is regression-tested |
| Catalog and publication | PASS | deterministic public names, SDK-compatible schema validation, filtering, immutable snapshots, dynamic add/remove, and publish locking |
| Manager and router | PASS | generations, leases, concurrency, backoff, revision-gated discovery/publication, no-replay calls, bounded public tool names, and sanitized call records |
| Live runtime controller | PASS | polling/admin reload share one transaction domain; stale snapshots are rejected; superseded discovery is cancelled/gated; active startup credentials remain filtered from reloaded stdio environments |
| Inbound HTTP | PASS | Host/Origin enforcement, public bearer auth, body/session bounds, stateful MCP, and sanitized diagnostics |
| stdio bridge | PASS | authenticated three-hop forwarding, dynamic tool sync, cancellation, failure exit, stdout purity, and remote plaintext HTTP rejection |
| Admin API | PASS | local/public same-origin login, bounded sessions/rates, CSRF, strict JSON, secret placeholders, ETag/CAS writes, and security headers |
| Admin browser UI | PASS | pure helper tests, six Chromium regressions with stubbed API, and integrated Chromium smoke against a real local Hub are exercised on Ubuntu CI |
| CLI diagnostics | PARTIAL | authenticated `status`/`doctor`, redirect blocking and bounded response reads are implemented; endpoint display sanitization and a few diagnostics UX edge cases remain follow-up work |
| Release packaging | PARTIAL | native Linux/Windows CI builds pass; versioned release binaries, checksums and a release workflow have not yet been published |

## v0.3.1 audit-hardening corrections

- Reject downstream tools whose `outputSchema` would cause the pinned MCP SDK's
  `Server.AddTool` path to panic.
- Synchronize hot-reloaded generation call-timeout reads/writes and cover the
  path under the race detector.
- Linearize desired revisions against catalog publication. Superseded
  connect/discovery attempts are cancelled and stale refresh results cannot
  overwrite a newer configuration.
- Keep startup-active Hub MCP/Admin credentials filtered from stdio child
  environments until the process is actually restarted after token rotation.
- Preserve native POSIX `npx` execution instead of imposing the Windows npm
  installation layout on Unix hosts.
- Make imports reject trailing JSON; dry-run previews no longer expose argument
  values or URL path/query credentials.
- Centralize MCP HTTP transport policy: remote connections require HTTPS,
  plaintext HTTP is loopback-only, and Hub CLI/bridge endpoints reject URL
  credentials/query/fragment values that can leak through diagnostics.
- Bound CLI diagnostic response bodies and reject invalid public tool names
  before they can be retained in recent-call diagnostics or echoed to a
  terminal.
- Report actual unpublished-tool counts from the Catalog instead of a hard-coded
  zero.
- Drain active generation leases before cancellation during manager/coordinator
  shutdown.
- Route Windows context cancellation through Job Object-aware `Process.Close`,
  matching POSIX process-tree semantics.
- Reject case-folded duplicate HTTP Header/environment names so configuration
  behavior does not depend on Go map iteration or Windows case folding.
- Enforce exact case-sensitive JSON field names in configuration files instead
  of accepting `encoding/json`'s case-insensitive struct aliases.

## Verification commands

Root module:

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 180s ./...
go vet ./...
go build -trimpath ./cmd/mcp-hub
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

Independent SDK probe:

```bash
cd verification/sdkprobe
go test -race -count=1 -timeout 90s ./...
```

GitHub Actions run the root Go suite on current Ubuntu and Windows runners.
Ubuntu additionally runs the admin JavaScript tests, Chromium regressions, and
integrated Chromium smoke against a real Hub.

## Evidence boundary

| Environment or client | Status | Notes |
|---|---|---|
| Linux amd64 | PASS | current audit-hardening CI passed tests, race tests, vet, SDK probe, real-Hub Chromium smoke and build |
| Windows amd64 | PASS | current audit-hardening CI passed native tests, race tests, vet, SDK probe, Windows process-tree context-cancellation regression and build |
| macOS | NOT_RUN | CI does not currently include a macOS runner |
| Cursor | NOT_RUN | no exact installed version has been exercised end to end |
| Claude Desktop | NOT_RUN | no exact installed version has been exercised end to end |
| Arbitrary third-party MCP servers | NOT_RUN | compatibility depends on each server's protocol behavior and runtime dependencies |

## Known follow-up work

The following items are intentionally not hidden behind `PASS` labels and are
not part of this focused audit-hardening PR:

- Upgrade the root module and independent SDK probe together from MCP Go SDK
  v1.4.1 to a current release, then add current-protocol and transport-size
  regressions. This is a protocol/behavior upgrade, not a one-line dependency
  bump.
- Redesign inherited stdio environment policy. Today child processes inherit
  the Hub environment except filtered Hub authentication values; a safer
  allowlist/explicit opt-in model is behavior-changing and needs migration
  semantics.
- Clarify or extend Admin save semantics: `Manager.Apply` accepts desired state
  asynchronously, so a syntactically valid but unreachable downstream may be
  persisted before its later connection failure is observed.
- Replace crash-persistent sibling `.lock` files with a cross-platform locking
  design or a provably safe stale-lock recovery mechanism. A timeout-based
  deletion heuristic is deliberately not used because it can break a live
  writer.
- Investigate the POSIX corner case where the root process exits before a
  descendant while the process group remains alive.
- Complete CLI endpoint-display sanitization/version identity cleanup and other
  lower-risk diagnostics polish.
- Add macOS, patched minimum-Go, `govulncheck`, release packaging/checksums, and
  repository protection before claiming a formal stable release.

## Merge and release gates

This audit-hardening PR may be merged once its final CI is green and review is
satisfactory; the deferred items above do not invalidate the targeted fixes.
They **do** remain gates for broader claims such as exhaustive third-party MCP
compatibility or a polished formal v0.3.x stable release.

Release artifacts are intentionally not tracked as stale binaries or checksum
files. Generate them from the exact tagged commit and publish matching checksums
with the release.
