# MCP Hub implementation status

Last updated: 2026-09-08 for the v0.4.0 release candidate.

Status values are `PASS`, `PARTIAL`, and `NOT_RUN`.

- `PASS` means the stated contract has direct automated/platform evidence.
- `PARTIAL` means the implementation exists but the listed verification boundary is incomplete.
- `NOT_RUN` means no exact acceptance evidence has been recorded.

A `PASS` row is not a claim of compatibility with every third-party MCP server, client, proxy, or operating-system release.

## Current implementation

| Area | Status | Evidence / notes |
|---|---|---|
| Strict configuration | PASS | strict case-sensitive decoding, duplicate/unknown/trailing rejection, 1 MiB bounded reads/digests, validation, one-pass env expansion, path resolution, CAS, durable atomic writes, OS-backed cross-process locking |
| Shared configuration loading | PASS | CLI, runtime reload, Browser Admin, and Remote Admin use the shared strict loader/transaction model |
| Downstream stdio | PASS | direct command/args execution, safe compatibility env allowlist, explicit secret forwarding, startup/call limits, cancellation, POSIX process groups, Windows Job Objects |
| Downstream Streamable HTTP | PASS | SDK v1.7.0, remote HTTPS enforcement, redirect rejection, header restrictions, response/body policy, session/cancellation behavior, compatibility discovery paths |
| Protocol boundary | PARTIAL | current tested stateful/session-oriented compatibility model is documented; full native coverage of every MCP 2026-07-28 capability is intentionally not claimed |
| Catalog and publication | PASS | deterministic public names, schema/name validation, filtering, dynamic publication, revision-gated updates, unpublished counts |
| Manager and router | PASS | generations, leases, concurrency, backoff, superseded-operation cancellation, no-replay calls, sanitized diagnostics, graceful drain |
| Live runtime controller | PASS | file polling/Admin share one transaction domain; stale snapshots rejected; last healthy runtime retained; startup-bound changes set restart-required |
| Inbound HTTP | PASS | Host/Origin policy, public bearer auth, body/session caps, init and established-session read deadlines, stateful negotiation, sanitized diagnostics |
| stdio bridge | PASS | authenticated forwarding, dynamic tool sync, failure/normal disconnect classification, stdout purity, safe endpoint policy |
| Browser Admin API | PASS | same-origin auth, bounded sessions/rates, CSRF, strict JSON, secret placeholders, ETag/CAS, preflight, rollback/restoration, security headers |
| Browser Admin UI | PASS | helper tests, Chromium interaction/design regressions, responsive/accessibility checks, integrated Chromium smoke against a real local Hub |
| Remote Admin CLI | PASS | `list/get/add/edit/delete`, Admin session + CSRF, redacted reads, ETag/CAS, HTTPS endpoint policy, redirect blocking, explicit delete confirmation, bounded server JSON |
| CLI diagnostics | PASS | `status`, `doctor`, `stdio`, and remote Admin auth/endpoint policy; response caps and sanitized errors |
| Agent deployment assets | PASS | embedded `mcp-hub-deployer` Skill, authenticated ZIP delivery, copyable Agent Prompt, deployment/upgrade/recovery guidance |
| CI / vulnerability gate | PASS | Linux/Windows/macOS × Go 1.25.8/1.27.1; current-Go race + SDK probe; Linux Chromium + real-Hub smoke; `govulncheck`; pinned Actions |
| Release packaging | PARTIAL | tag workflow verifies source version, tests/vets/probes, cross-builds six OS/arch archives, injects build metadata, generates SHA256SUMS, and publishes GitHub Release; no formal tag/release has been created yet |
| Repository maintenance | PASS | CODEOWNERS and weekly Dependabot coverage for root Go, sdkprobe Go, and GitHub Actions |
| Branch protection / ruleset | PARTIAL | repository currently has no enforced main protection/ruleset; this is a governance risk, not a runtime code blocker |
| License | PARTIAL | public repository currently has no selected LICENSE; owner/legal choice remains pending |

## Current dependency and version baseline

- product identity: `0.4.0` source default;
- production Go directive: `1.25.0`;
- CI compatibility/current toolchains: Go `1.25.8` and `1.27.1`;
- production MCP Go SDK: `v1.7.0`;
- independent SDK probe MCP Go SDK: `v1.7.0`;
- production `golang.org/x/sys`: currently `v0.44.0` on `main`;
- open Dependabot PR #10 proposes `x/sys v0.47.0` but is based on an old main and must be rebased/recreated and revalidated before merge.

## v0.4.0 hardening delivered

The candidate includes the v0.3.1-v0.3.4 audit work plus subsequent release/admin/CLI improvements:

- upgraded root and SDK probe to MCP Go SDK v1.7.0;
- retained a documented compatibility boundary instead of claiming unverified current-protocol feature coverage;
- replaced broad stdio environment inheritance with a least-privilege compatibility allowlist;
- protects Hub auth secrets from implicit stdio inheritance;
- uses OS-backed config locking and durable atomic writes;
- preflights new/connection-changing Admin downstreams before persistence;
- validates discovered tools before publication;
- cleans POSIX process groups and Windows Job-owned trees;
- requires strong, distinct public/Admin authentication credentials in public mode;
- centralizes build/version identity and release-time metadata injection;
- bounds existing-session POST reads without applying an incorrect short timeout to SSE lifetime;
- provides Browser Admin and Remote Admin CLI through one management transaction model;
- snapshots editor ETag at open time and publishes config+revision together after successful refresh;
- reads slow Admin PUT bodies before taking the shared config transaction lock;
- embeds Agent deployment assets into the single binary;
- expands CI to cross-platform dual-Go coverage, race, SDK probe, browser regressions, real-Hub smoke, and govulncheck.

## Admin management evidence

The management plane has three intended clients:

1. Browser Admin `/admin/`;
2. `mcp-hub admin` Remote Admin CLI;
3. trusted Agent automation using the same CLI/Admin semantics.

Mutations preserve:

- authenticated Admin session;
- CSRF token;
- strict JSON;
- ETag/CAS;
- downstream preflight for connection-changing edits;
- shared config transaction/OS lock;
- atomic persistence;
- runtime reload;
- rollback/last-healthy-state behavior.

See [REMOTE-ADMIN.md](REMOTE-ADMIN.md), [SECURITY.md](SECURITY.md), and [CONSOLE-POLISH.md](CONSOLE-POLISH.md).

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

SDK probe:

```bash
cd verification/sdkprobe
go test -race -count=1 -timeout 120s ./...
```

Browser regressions:

```bash
cd internal/admin/browsertest
npm ci
npx playwright install --with-deps chromium
npm test
```

Real-Hub Chromium smoke from repository root:

```bash
go build -trimpath -o dist/mcp-hub ./cmd/mcp-hub
node internal/admin/browsertest/live-smoke.mjs
```

GitHub Actions additionally runs the six OS/Go matrix jobs and `govulncheck`.

## Evidence boundary

| Environment / client | Status | Notes |
|---|---|---|
| Linux | PASS | CI covers compatibility/current Go, test/vet/build; current Go adds race/SDK/browser/integration checks |
| Windows | PASS | CI covers compatibility/current Go, test/vet/build; current Go adds race/SDK probe and native Job regressions |
| macOS | PASS | CI covers compatibility/current Go test/vet/build; current Go adds race/SDK probe |
| Browser Admin on loopback | PASS | Chromium regressions + real local Hub smoke |
| Remote Admin CLI | PASS | automated CRUD/auth/redaction/conflict-path coverage |
| Cursor | NOT_RUN | no exact installed version recorded as certified |
| Claude Desktop | NOT_RUN | no exact installed version recorded as certified |
| Arbitrary third-party MCP servers | NOT_RUN | depends on each server's protocol/runtime/schema/external dependencies |
| Reverse-proxy/public deployment matrix | PARTIAL | security/trusted-proxy policy is automated; exhaustive Caddy/Nginx/TLS/provider combinations are not certified |

## Documentation status

Current user/maintainer documentation is expected to agree with this file and the code baseline:

- `README.md`
- `README.zh-CN.md`
- `MCP-Hub-开发设计文档.md`
- `MCP-Hub-Agent实施手册.md`
- `docs/CONFIG.md`
- `docs/SECURITY.md`
- `docs/COMPATIBILITY.md`
- `docs/VPS.md`
- `docs/REMOTE-ADMIN.md`
- `verification/sdkprobe/README.md`

`docs/HARDENING-VERIFICATION.md` is historical v0.3.1 evidence and is explicitly labeled as such; historical measurements inside it should not be read as the current implementation contract.

## Remaining non-blocking engineering / compatibility work

- run named/versioned Cursor and Claude Desktop acceptance tests;
- broaden real third-party MCP interoperability fixtures;
- exercise a larger production reverse-proxy/TLS matrix;
- decide whether to merge/recreate the pending `x/sys` dependency bump;
- consider moving the Go compatibility floor in a future release only with explicit justification.

## Remaining release/governance work

Before calling v0.4.0 a formal release:

1. final main CI must be green;
2. documentation must match the final release commit;
3. pending dependency PRs need an explicit decision;
4. create an exact `v0.4.0` tag pointing to the intended commit;
5. verify the Release workflow completes;
6. verify all six archives and `SHA256SUMS` are published.

Recommended repository-governance follow-up:

- enable main branch protection/ruleset with required CI and no force-push;
- choose and add an explicit open-source license if public reuse is intended;
- prune merged/obsolete remote branches after semantic verification.

These governance items do not currently represent a known runtime release-blocking defect, but they matter for a mature public repository.
