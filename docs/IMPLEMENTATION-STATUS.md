# MCP Hub implementation status

Last updated: 2026-09-07 for the v0.4.0 release candidate.

Status values are `PASS`, `PARTIAL`, and `NOT_RUN`. `PASS` means the stated contract has direct automated or platform evidence; it is not a claim of compatibility with every third-party MCP server or GUI client.

## Current implementation

| Area | Status | Evidence / notes |
|---|---|---|
| Strict configuration | PASS | strict case-sensitive decoding, duplicate/unknown/trailing input rejection, 1 MiB bounded reads/digests, validation, one-pass environment expansion, path resolution, CAS, durable atomic writes, and OS-backed cross-process locking |
| Shared configuration loading | PASS | CLI, runtime reload, and Admin use the shared strict loader; oversized/special-file reads are bounded |
| Downstream transports | PASS | stdio and Streamable HTTP, MCP Go SDK v1.7.0, pagination, cancellation, response/body policies, legacy discovery compatibility, redirect/header restrictions, and remote HTTPS enforcement |
| stdio environment boundary | PASS | child processes inherit a compatibility allowlist rather than the Hub environment wholesale; server `env` is explicit opt-in and Hub auth values remain filtered |
| Process ownership | PASS | POSIX process groups and Windows Job Objects; cancellation/close kills descendants, including the POSIX root-exits-first case |
| Catalog and publication | PASS | deterministic public names, schema/name validation before SDK publication, filtering, immutable snapshots, accurate unpublished counts, and revision-gated publication |
| Manager and router | PASS | generations, leases, concurrency, backoff, revision linearization, superseded-operation cancellation, no-replay calls, sanitized diagnostics, and graceful drain ordering |
| Live runtime controller | PASS | file polling/Admin share one transaction domain; stale snapshots are rejected; startup-bound changes report restart required without silently revoking live credentials |
| Inbound HTTP | PASS | Host/Origin policy, public bearer auth, body/session caps, init and existing-session POST read deadlines, stateful protocol negotiation, and sanitized diagnostics |
| stdio bridge | PASS | authenticated forwarding, dynamic tool synchronization, normal-disconnect classification, failure exit, stdout purity, and safe Hub endpoint policy |
| Admin API | PASS | same-origin auth, bounded sessions/rates, CSRF, strict JSON, secret placeholders, ETag/CAS, isolated downstream preflight before persistence, rollback/restoration, and security headers |
| Admin browser UI | PASS | JavaScript unit tests, Chromium regressions with stubbed API, and integrated Chromium smoke against a real local Hub on Linux CI |
| CLI diagnostics | PASS | `status`, `doctor`, and `stdio` support auth; endpoint policy, redirect blocking, response caps, output sanitization, and centralized v0.4.0 version identity are implemented |
| CI / vulnerability gate | PASS | matrix covers Linux, Windows, and macOS at compatibility/current Go; current-Go race and SDK probe jobs plus `govulncheck`; Actions are pinned |
| Release packaging | PARTIAL | tag workflow is implemented to verify version, test/vet/probe, cross-build six OS/arch targets, inject build metadata, archive artifacts, generate SHA256SUMS, and publish a verified-tag GitHub release; the tag-triggered workflow remains NOT_RUN until an actual release tag is authorized |
| Repository maintenance | PASS | CODEOWNERS and weekly Dependabot coverage for both Go modules and GitHub Actions |

## v0.4.0 release-candidate hardening

The v0.4.0 candidate includes the earlier v0.3.1-v0.3.4 audit fixes and closes the major deferred engineering items from those reviews:

- upgraded the root module and independent SDK probe to MCP Go SDK v1.7.0, including current/legacy protocol and transport behavior;
- replaced broad stdio environment inheritance with a least-privilege compatibility allowlist;
- replaced crash-stale lock-file ownership with OS-backed advisory locking while retaining a stable sibling lock path;
- preflights new/connection-changed Admin downstreams and validates their discovered tools before persistence;
- bounds configuration reads and digest computation and strengthens atomic-write durability;
- cleans POSIX descendants even when the direct child exits before its process group;
- requires public/Admin authentication tokens to be at least 32 characters and distinct;
- centralizes runtime/build version identity at v0.4.0 and supports release-time commit/build-date injection;
- applies a bounded read deadline to existing-session MCP POST requests without affecting SSE GET streams;
- expands CI to Linux, Windows, and macOS, adds compatibility/current Go coverage, race/SDK/browser checks, vulnerability scanning, pinned Actions, release packaging, Dependabot, and CODEOWNERS.

## Verification commands

Root module:

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
go build -trimpath ./cmd/mcp-hub
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
```

Independent SDK probe:

```bash
cd verification/sdkprobe
go test -race -count=1 -timeout 120s ./...
```

GitHub Actions additionally exercise the OS/Go matrix, Chromium browser regressions, an integrated browser smoke test against a real local Hub, and `govulncheck`.

## Evidence boundary

| Environment or client | Status | Notes |
|---|---|---|
| Linux | PASS | local current-Go test/race/vet/SDK probe pass; CI covers compatibility/current Go plus browser/integration checks |
| Windows | PASS | CI covers compatibility/current Go, build/tests/vet, current-Go race/SDK probe, and native Job Object regressions |
| macOS | PASS | CI matrix covers compatibility/current Go build/tests/vet and current-Go race/SDK probe |
| Cursor | NOT_RUN | no exact installed GUI-client version is asserted as end-to-end certified |
| Claude Desktop | NOT_RUN | no exact installed GUI-client version is asserted as end-to-end certified |
| Arbitrary third-party MCP servers | NOT_RUN | compatibility still depends on each server's protocol behavior, schemas, runtime, and external dependencies |
| Reverse-proxy/public deployment matrix | PARTIAL | security policy and trusted-proxy behavior are automated; exhaustive Caddy/Nginx/TLS-provider combinations are not certified |

## Remaining non-blocking work

These are not known merge blockers for the v0.4.0 candidate, but they remain outside an exhaustive compatibility claim:

- run named/versioned GUI-client acceptance tests for Cursor and Claude Desktop when those clients are available;
- broaden real third-party MCP server interoperability fixtures beyond the deterministic SDK probe and test servers;
- exercise a larger matrix of production reverse proxies/TLS termination deployments;
- consider moving the declared Go compatibility floor forward in a future release when backward compatibility is no longer useful.

## Merge and release gates

The v0.4.0 release-candidate PR is ready to leave draft only when its **final head** has a green GitHub Actions matrix and vulnerability scan, documentation matches that head, and review finds no unresolved blocker.

A formal release should be created from an exact signed/verified `v0.4.0` tag after the stacked hardening PRs are merged in order. Release artifacts are generated from the tagged commit; binaries and checksum files are not committed as stale repository artifacts.
