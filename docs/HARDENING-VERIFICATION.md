# v0.3 hardening verification

Base: `ce44fd0` (`refactor/v0.3`). This is a hardening change, not a formal release.

## Exercised on the final source tree

Linux amd64, Go 1.25.0, Node 22.23.2:

- `go test -count=1 -timeout 180s ./...`: PASS.
- `go test -race -count=1 -timeout 180s ./...`: PASS.
- `go vet ./...`: PASS.
- `go build -trimpath -o dist/mcp-hub ./cmd/mcp-hub`: PASS.
- JavaScript syntax check and four pure helper tests: PASS.
- Six Chromium DOM/module regressions with a stubbed API: PASS.
- `node internal/admin/browsertest/live-smoke.mjs`: PASS against the real local Hub: cookie flags, login, edit/JSON argument preservation, secret preservation, duplicate replacement persisted to disk, missing-CSRF rejection, and authentication-change restart warning.
- `git diff --check`: PASS.

The independent SDK probe previously passed with race enabled during this work. Runtime/admin race regressions also passed ten repetitions. Initial tests run while parallel edits were in progress failed compilation; those runs are not counted as passing evidence. The final root checks above were rerun after parallel edits ended.

## Boundaries and follow-up

- Windows CI must run on this commit; old CI is not evidence for the new changes. Native macOS, named GUI MCP clients and third-party compatibility remain unverified.
- Browser smoke uses local mode and disabled fixture downstreams; it does not validate public reverse proxies or third-party process behavior.
- Rollback tests reconcile a real Manager but inject an error after application. Active-lease drain timeout/cancellation lifecycle needs separate investigation.
- Transaction locking coordinates in-process Hub writers. A digest check rejects snapshots replaced during parsing; arbitrary external editors do not participate in the transaction lock.
- Review findings not yet independently reproduced/fixed: unbounded status response `io.ReadAll`; endpoint output may expose URL userinfo/query credentials; import preview prints raw Args/URL and should not promise complete secret redaction.
- Release packaging remains PARTIAL: exact-tag binaries, checksums and a release workflow are required before formal v0.3 publication.
