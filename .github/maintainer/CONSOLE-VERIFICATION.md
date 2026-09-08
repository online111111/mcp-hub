# Console polish and reliability verification

## Scope

This evidence note records the Admin console redesign/reliability work while preserving the single Go binary and dependency-free production UI.

### Console design

- Dark workspace navigation with small inline SVG icons, a light working area, and shared color/spacing/component rules.
- Reworked login, overview metrics, service cards, client access, deployment assets, and editor styles.
- Responsive layouts verified at 320, 390, 768, 1024, and 1440 pixels.
- Mobile call tables preserve every column inside a keyboard-accessible horizontal scroll region rather than hiding data.
- Accessible filter labels/dialog naming, visible keyboard focus, reduced-motion handling in CSS and navigation JavaScript.
- No external fonts, icon libraries, or frontend runtime dependencies added.

### Reliability fixes

1. **Slow request bodies must not hold the configuration transaction lock.** Decode the bounded request body before acquiring the shared transaction lock. Raw-config reads, ETag checks, mutation, persistence, and reload remain inside the lock. A pipe-based regression covers standalone and runtime-shared locks.
2. **Editor drafts keep their original configuration revision.** Snapshot the ETag when the editor opens so a later refresh cannot silently authorize a stale draft against a newer revision. Config content and ETag publish together only after a complete successful refresh. Server-side CAS remains authoritative.
3. **Manual refresh failures must not escape as unhandled promise rejections.** Toast/connection indicators report the failure while handlers consume the already-reported rejection.
4. **Failed logout must not imply confirmed server invalidation.** Clear the local view, handle the rejected request, and warn that the server session may still be valid.

## Verification

Representative commands:

```sh
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
(cd verification/sdkprobe && go test -race -count=1 -timeout 120s ./...)
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
npm ci --prefix internal/admin/browsertest
npm test --prefix internal/admin/browsertest
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
node internal/admin/browsertest/live-smoke.mjs
```

Chromium interaction/design regressions use production HTML/CSS/JS with explicitly stubbed Admin API data and therefore do not alone prove backend persistence. The separate live smoke starts a real local MCP Manager with fixture downstreams and verifies cookie/CSRF behavior, argument roundtrips, and persisted secret handling.

Loopback browser evidence also does not substitute for every public reverse proxy, GUI client, or third-party MCP server combination.

## Remaining opportunities

- Server search/status filtering for installations with many downstreams.
- Synchronize navigation selection with manual scrolling.
- Warn before discarding dirty editor drafts.
- Distinguish Manager connectivity from aggregate downstream readiness in the header.
- Continue public reverse-proxy and third-party GUI compatibility verification separately.
