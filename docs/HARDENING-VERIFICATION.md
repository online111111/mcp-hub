# v0.3.1 audit-hardening verification

> Historical evidence for the v0.3.1 branch, not the current implementation.
> For current SDK, environment inheritance, locking, preflight and platform coverage,
> see [implementation status](IMPLEMENTATION-STATUS.md) and
> [console polish verification](CONSOLE-POLISH.md).

Base: `4a502b1` (`main`). Branch: `fix/v0.3.1-audit-hardening`. This is a focused hardening change, not a formal release.

## Current automated evidence

GitHub Actions exercises the branch on Ubuntu and Windows using Go 1.26.6.
The fully exercised audit-hardening source immediately before this documentation
update passed:

### Ubuntu

- `go test -count=1 -timeout 180s ./...`: PASS.
- `go test -race -count=1 -timeout 180s ./...`: PASS.
- `go vet ./...`: PASS.
- independent `verification/sdkprobe` race test: PASS.
- JavaScript syntax/helper tests: PASS.
- six Chromium DOM/module regressions with a stubbed API: PASS.
- integrated Chromium smoke against a real local Hub: PASS.
- `go build -trimpath ./cmd/mcp-hub`: PASS.

### Windows

- `go test -count=1 -timeout 180s ./...`: PASS.
- `go test -race -count=1 -timeout 180s ./...`: PASS.
- `go vet ./...`: PASS.
- independent `verification/sdkprobe` race test: PASS.
- Windows Job Object/process-tree regressions, including context-cancel descendant cleanup: PASS as part of the root suite.
- `go build -trimpath ./cmd/mcp-hub`: PASS.

The documentation-only commits after that run require the final PR-head CI to
remain green before merge.

## Audit findings closed by this branch

- Downstream `outputSchema` values incompatible with the pinned MCP SDK are
  rejected before they can reach an SDK panic path.
- Generation `callTimeout` hot reload no longer contains an unsynchronized
  read/write pair.
- Desired configuration revisions are linearized against tool publication;
  superseded connection/discovery work is cancelled and stale refresh results
  cannot publish over a newer revision.
- Startup-active Hub MCP/Admin tokens remain excluded from reloaded stdio child
  environments until a restart actually rotates the listener/authentication
  state.
- POSIX `npx` is resolved as a native executable rather than assuming the
  Windows npm directory layout.
- Import parsing rejects trailing root JSON and preview output no longer exposes
  raw argument values or URL path/query secrets.
- Remote plaintext MCP HTTP connections are rejected; Hub CLI/bridge endpoints
  additionally reject URL credentials/query/fragment values. CLI diagnostic
  response bodies have a hard read bound.
- Invalid/oversized/control-character public tool names are rejected before
  recent-call recording and terminal-facing error formatting.
- Unpublished-tool diagnostics are sourced from the Catalog rather than a
  hard-coded zero.
- Manager/Coordinator shutdown drains leases before cancelling generation
  contexts.
- Windows context cancellation now goes through Job Object-aware process-tree
  cleanup rather than only terminating the immediate worker.
- Case-folded duplicate HTTP Header/environment names are rejected.
- Configuration struct field names are checked with exact case-sensitive JSON
  tag matching in addition to duplicate/unknown/trailing checks.

## Evidence boundaries and deferred work

- Native macOS, Cursor, Claude Desktop, public reverse-proxy end-to-end behavior,
  and arbitrary third-party MCP servers remain unverified.
- The production root module and the independent SDK probe still pin MCP Go SDK
  v1.4.1. A current-SDK upgrade must update and test both modules together; it
  should also add explicit current-protocol and transport-size regressions.
- Third-party stdio children still inherit the broader Hub process environment
  after Hub authentication-secret filtering. Moving to an allowlist/explicit
  inheritance model is deliberately deferred because it changes runtime
  compatibility.
- Admin persistence validates and applies desired configuration, but downstream
  readiness is asynchronous. A valid yet unreachable new downstream can be
  persisted and then fail during connection. Staged/preflight availability is a
  separate product-semantics decision.
- Configuration writes still use a sibling `.lock` file. A process crash can
  leave a stale lock. This branch deliberately does not implement heuristic
  timeout deletion, because that can remove a legitimate live writer; use OS
  locking or a provably safe stale-lock design in a separate change.
- POSIX process-group handling still warrants a regression for the corner case
  where the root exits while descendants remain alive.
- CLI endpoint-display sanitization, build-version identity cleanup, patched
  minimum-Go testing, `govulncheck`, macOS CI, repository protection and formal
  release packaging remain follow-up work.

## Release boundary

Passing CI for this PR supports merging the targeted hardening changes. It does
not by itself constitute a formal stable release. A formal v0.3.x release should
be built from the exact tag, include published checksums, and accurately state
its verified platform/client matrix.
