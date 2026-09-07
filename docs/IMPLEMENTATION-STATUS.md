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
| Live runtime controller | PARTIAL | Polling, immediate reload and invalid-config retention exist; reload ordering, rollback concurrency and startup-setting detection require the hardening regressions, not just the original tests |
| Inbound HTTP | PASS | Host/Origin enforcement, public bearer auth, body/session bounds, stateful MCP, and sanitized diagnostics |
| stdio bridge | PASS | authenticated three-hop forwarding, dynamic tool sync, cancellation, failure exit, and stdout purity |
| Admin API | PASS | local/public same-origin login, bounded sessions/rates, CSRF, strict JSON, secret placeholders, ETag/CAS writes, and security headers |
| Admin browser UI | PARTIAL | Pure helper tests and Go static/HTTP checks cover specific contracts, not complete browser workflows; see the hardening report for separately exercised browser scenarios |
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
| Windows amd64 | PARTIAL | The pre-hardening CI passed native tests and lifecycle checks; changes in this hardening branch require a fresh Windows CI run |
| macOS | NOT_RUN | CI does not currently include a macOS runner |
| Cursor | NOT_RUN | no exact installed version was supplied for an end-to-end exercise |
| Claude Desktop | NOT_RUN | no exact installed version was supplied for an end-to-end exercise |
| Arbitrary third-party MCP servers | NOT_RUN | compatibility depends on each server's protocol behavior and runtime dependencies |

## Merge and release gates

| Remaining work | Blocks this hardening merge? | Required boundary |
|---|---|---|
| Release packaging (`PARTIAL`) | No | Before a formal v0.3 release, build binaries from the exact tag and publish matching checksums. CI build success is not a release workflow or a published release. |
| Native macOS (`NOT_RUN`) | No | Do not claim verified macOS support until native tests and lifecycle checks are recorded. Cross-compilation is not native validation. |
| Cursor / Claude Desktop (`NOT_RUN`) | Usually no | Before claiming client compatibility, record exact versions and exercise connect, list tools, and call tools end to end. |
| Arbitrary MCP servers (`NOT_RUN`) | No | Exhaustive compatibility is impossible; maintain a representative stdio and Streamable HTTP matrix with exact versions. SDK fixtures alone are not third-party compatibility proof. |
| Credential redirects and reload ordering | Yes | Regression tests must reject redirect credential forwarding and cover serialized reload plus real-Manager rollback under polling. Race detection alone cannot prove logical ordering. |

Release artifacts are intentionally not tracked as stale binaries or checksum
files. Generate both from the exact tagged commit and publish the checksums with
the release. No release has been published as part of this hardening work.
