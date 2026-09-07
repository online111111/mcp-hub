# Console polish and reliability verification

## Scope

This change preserves the single Go binary and dependency-free production UI. It does not deploy or alter any existing Hub installation.

### Console design

- Dark workspace navigation with small inline SVG icons, a light working area, and a shared color/spacing/component token system.
- Reworked login, overview metrics, service cards, client access, deployment assets and editor styles.
- Responsive layouts verified at 320, 390, 768, 1024 and 1440 pixels.
- Preserve every call-table column on mobile in a keyboard-accessible horizontal scroll region rather than hiding data.
- Accessible filter labels and dialog naming, visible keyboard focus on the enable switch, reduced-motion handling in both CSS and navigation JavaScript.
- No external fonts, icon libraries or frontend runtime dependencies added.

### Reliability fixes

1. **Slow request bodies must not hold the configuration transaction lock.** Decode the bounded request body before acquiring the shared transaction lock. Raw-config reads, ETag checks, mutation, persistence and reload still execute inside the lock. A pipe-based regression covers standalone and runtime-shared locks.
2. **Editor drafts keep their original configuration revision.** Snapshot the ETag when the editor opens so a later refresh cannot silently authorize a stale draft against a newer revision. Configuration content and its ETag are committed together only after a complete successful refresh, including when a sibling status request fails. Server-side compare-and-swap remains authoritative.
3. **Manual refresh failures must not escape as unhandled promise rejections.** Existing toast and connection indicators remain visible while event handlers consume the already-reported rejection.
4. **Failed logout must not imply successful server-side invalidation.** Clear the local view, handle the rejected request and warn that the server session may remain active.

## Verification

Run from the repository root (Go 1.27.1 / Node 22 used for local verification):

```sh
go test -p 2 -count=1 -timeout 180s ./...
go test -race -p 2 -count=1 -timeout 180s ./...
go vet -p 2 ./...
(cd verification/sdkprobe && go test -p 2 -count=1 -timeout 180s ./...)
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
npm ci --prefix internal/admin/browsertest
npm test --prefix internal/admin/browsertest
go build -p 2 -trimpath -o dist/mcp-hub ./cmd/mcp-hub
node internal/admin/browsertest/live-smoke.mjs
```

`npm test` now includes both interaction and design regressions, with test files run sequentially for small development machines. The Chromium suites use production HTML/CSS/JS with explicitly stubbed Admin API data; they do not prove backend persistence. The separate `live-smoke.mjs` starts a real local Hub with disabled fixture downstreams and verifies cookie flags, CSRF rejection, argument roundtrips and persisted secret handling.

Screenshots were also captured against a real loopback Hub with **two disabled fixture downstreams**, not against a production service. Desktop, mobile and editor views were inspected; the 390px document had no horizontal page overflow and no JavaScript page errors.

## Remaining opportunities

- Server search/status filtering would be useful for installations with many downstreams. The current change deliberately avoids adding more UI state before that need is established.
- Follow-up interaction work could synchronize navigation selection with manual scrolling, and warn before discarding dirty editor drafts.
- The header currently indicates Hub connectivity rather than aggregate downstream readiness. A future health-summary pass should make that distinction explicit when a downstream is in backoff.
- Public reverse-proxy deployments and third-party GUI MCP clients still require separate compatibility verification. Loopback browser evidence does not substitute for those checks.
