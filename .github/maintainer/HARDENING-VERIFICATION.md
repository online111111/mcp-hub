# v0.3.1 audit-hardening verification

> Historical evidence for the v0.3.1 branch, not the current implementation.
> For current status and platform coverage, see [implementation status](IMPLEMENTATION-STATUS.md) and [console verification](CONSOLE-VERIFICATION.md).

Base: `4a502b1` (`main`). Branch: `fix/v0.3.1-audit-hardening`. This was a focused hardening change, not a formal release.

## Automated evidence at the time

GitHub Actions exercised the branch on Ubuntu and Windows using Go 1.26.6.

### Ubuntu

- root `go test`: PASS
- root race test: PASS
- `go vet`: PASS
- independent SDK probe race test: PASS
- JavaScript syntax/helper tests: PASS
- Chromium DOM/module regressions: PASS
- integrated Chromium smoke against a real local process: PASS
- production binary build: PASS

### Windows

- root `go test`: PASS
- root race test: PASS
- `go vet`: PASS
- independent SDK probe race test: PASS
- Windows Job Object/process-tree regressions: PASS as part of the root suite
- production binary build: PASS

## Findings closed by that branch

- Reject downstream `outputSchema` values that could reach an incompatible SDK panic path.
- Remove an unsynchronized generation `callTimeout` read/write pair.
- Linearize desired configuration revisions against tool publication and cancel superseded discovery work.
- Keep startup-active MCP/Admin credentials out of reloaded stdio child environments until restart rotates listener/auth state.
- Resolve POSIX `npx` as a native executable instead of assuming Windows npm layout.
- Reject trailing root JSON; sanitize preview output so raw argument values and URL path/query secrets are not exposed.
- Reject remote plaintext MCP HTTP; reject URL credentials/query/fragment on CLI/bridge endpoints; bound diagnostic bodies.
- Reject invalid/oversized/control-character public tool names before recent-call recording and terminal formatting.
- Source unpublished-tool diagnostics from the Catalog rather than a hard-coded value.
- Drain leases before cancelling generation contexts during shutdown.
- Route Windows context cancellation through Job Object-aware process-tree cleanup.
- Reject case-folded duplicate HTTP headers/environment names.
- Enforce exact case-sensitive JSON-tag matching in addition to duplicate/unknown/trailing checks.

## Historical limitations at that point

The v0.3.1 snapshot still had several limitations that were addressed or changed later: older SDK pinning, broader environment inheritance, sibling lock-file behavior, lack of macOS CI, missing public preflight/release hardening, and incomplete release/governance work. These statements must not be interpreted as current MCP Manager behavior.

Use current source, tests, CI, [`IMPLEMENTATION-STATUS.md`](IMPLEMENTATION-STATUS.md), and public docs under `../../docs/` for present-day decisions.
