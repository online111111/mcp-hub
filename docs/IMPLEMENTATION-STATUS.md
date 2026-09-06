# MCP Hub implementation status

Last updated: 2026-09-06 for the v0.3 refactor.

Status values are `PASS`, `PARTIAL`, and `NOT_RUN`. A passing automated test is
evidence for the tested contract, not a claim about every third-party server or
client.

## Current implementation

| Area | Status | Evidence / notes |
|---|---|---|
| Strict configuration | PASS | duplicate/unknown/trailing input rejection, validation, environment expansion, path resolution, import preview, CAS, and atomic writes |
| Shared configuration loading | PASS | CLI and live runtime use `config.LoadFile`; loading has no network or process side effects |
| Downstream transports | PASS | stdio and Streamable HTTP sessions, redirect/header policy, raw arguments, pagination, and cancellation regressions |
| Process ownership | PASS | native POSIX process groups and Windows Job Object lifecycle implementations with descendant-cleanup tests |
| Catalog and publication | PASS | deterministic public names, filtering, immutable snapshots, dynamic add/remove, and publish locking |
| Manager and router | PASS | generations, leases, concurrency, backoff, break-before-make reloads, no-replay calls, and sanitized call records |
| Live runtime controller | PASS | two-stable-sample file polling, immediate admin reload, invalid-config retention, and listener restart detection |
| Inbound HTTP | PASS | Host/Origin enforcement, public bearer auth, body/session bounds, stateful MCP, and sanitized diagnostics |
| stdio bridge | PASS | authenticated three-hop forwarding, dynamic tool sync, cancellation, failure exit, and stdout purity |
| Admin API | PASS | local/public same-origin login, bounded sessions/rates, CSRF, strict JSON, secret placeholders, ETag/CAS writes, and security headers |
| Admin browser UI | PASS | CSP-compatible module events, form/JSON editing, secret preservation, filtering, responsive layout, and pure helper tests |
| CLI diagnostics | PASS | local and authenticated public `status`/`doctor`; doctor initializes MCP and lists tools without a persistent SSE stream |
| Release packaging | PARTIAL | native Linux build verified in development; versioned release binaries and checksums must be produced by a release workflow |

## v0.3 corrections

- Local admin login no longer rejects the browser's legitimate same-origin
  `Origin` header when `publicUrl` is empty.
- Service edit, duplicate, and delete actions no longer rely on inline handlers
  forbidden by the server's own Content Security Policy.
- Environment-variable and Header collection no longer calls `forEach` on a
  single DOM element. Duplicate names now fail clearly and secret placeholders
  remain intact.
- Admin login-source tracking is bounded, trailing JSON values are rejected,
  media types are parsed strictly, and random-source failures return an error
  instead of panicking the process.
- An admin change that fails live application is atomically rolled back and the
  previous runtime snapshot is reapplied instead of leaving disk and memory on
  different configurations.
- Public `status` and `doctor` calls use the same `--token`/
  `MCP_HUB_TOKEN` authentication convention as the stdio bridge.
- Product versions are sourced from `internal/buildinfo` rather than scattered
  string literals.
- File observation/reload state moved out of CLI and into `internal/runtime`;
  strict disk loading moved into `internal/config`.

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

GitHub Actions runs the Go suite on current Ubuntu and Windows runners and runs
the admin UI tests on Ubuntu.

## Evidence boundary

| Environment or client | Status | Notes |
|---|---|---|
| Linux amd64 | PASS | native tests, race tests, vet, build, MCP smoke test, and POSIX descendant cleanup were exercised |
| Windows amd64 | PASS | native tests, build, Job Object lifecycle, and a real stdio child were exercised |
| macOS | NOT_RUN | CI does not currently include a macOS runner |
| Cursor | NOT_RUN | no exact installed version was supplied for an end-to-end exercise |
| Claude Desktop | NOT_RUN | no exact installed version was supplied for an end-to-end exercise |
| Arbitrary third-party MCP servers | NOT_RUN | compatibility depends on each server's protocol behavior and runtime dependencies |

Release artifacts are intentionally not tracked as stale binaries or checksum
files. Generate both from the exact tagged commit and publish the checksums with
the release.
