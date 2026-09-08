# Production-hardening audit — 2026-09-08

Baseline: `c3353c1bf9a64ebecdf8ecd771d4a1401139bd55` (v0.4.4 source tree).
Candidate source version: 0.4.5. A source version is not a release announcement.

## Scope

Reviewed the inbound MCP/diagnostic surface, Admin authentication and transactions,
configuration loading/locking/reload, catalog publication, downstream HTTP/stdio,
manager generations/cancellation, bridge, process environments, browser state and
credentials, Windows startup, and release verification. Read deployment/security and
agent-facing documentation and inspected the pinned SDK's relevant transport and
registration behavior. This is an evidence-bounded engineering audit, not a formal
proof, external penetration-test certificate, or exhaustive third-party compatibility
certification.

## Findings and regression coverage

| Area | Defect / improvement | Verification anchor |
|---|---|---|
| Inbound authentication | Configured MCP tokens were ignored in local mode | TestLocalConfiguredBearerProtectsMCPAndDiagnostics |
| Token rotation | An authoritative empty token provider could restore the startup fallback credential | TestEmptyAuthoritativeTokenSetNeverRestoresStartupToken |
| Admin JSON | Duplicate keys, case-insensitive field names and null bodies were accepted | TestAdminBodyRejectsAmbiguousJSON |
| Admin login | Non-JSON content types were accepted | TestLoginRequiresJSONContentType |
| Slow Admin bodies | No mutation-specific body read deadline | TestAdminMutationSetsAndResetsBodyDeadline |
| Reload bounds | Polling/reload read full config bytes before size enforcement | TestReloadBoundsReadBeforeInvokingLoader |
| Credential domains | A pending Admin rotation could make the still-active Admin credential an MCP credential | TestReloadKeepsStartupAndMCPAuthDomainsSeparate |
| Peer disconnect | Wrapper could remain open after the SDK peer disconnected | TestPeerDisconnectClosesSessionWrapper |
| Hot changes | A call-timeout/filter edit could cancel the healthy downstream lifetime | TestHotTimeoutChangeDoesNotCancelLiveSession |
| Coordinator cancellation | Disabled coordinator could keep looping after parent cancellation | TestParentCancellationStopsDisabledCoordinator |
| Empty env comparison | Different empty-valued keys compared equal | TestEmptyEnvValuesStillCompareKeyIdentity |
| Callback concurrency | Router callback replacement/read lacked matching synchronization | TestPublisherCallbackConcurrentReplacement with race detector |
| Bridge lifetime | Startup deadline was reused for the entire HTTP connection | TestBridgeSurvivesStartupDeadline |
| CLI Admin sessions | Repeated commands leaked authenticated sessions until capacity exhaustion | TestRepeatedAdminCommandsReleaseSessions |
| Tool registration | SDK-specific invalid definitions could reach panic-based live registration | TestSDKHeaderAnnotationIsValidatedBeforePublication |
| Catalog size | Renaming a tool could exceed the accounted definition size | TestPublicNameCountsTowardDefinitionSize |
| Child env | An explicitly empty sanitized environment could become inherited, including across Windows bootstrap | TestBootstrapPreservesExplicitEmptyEnvironment; TestExplicitEmptyEnvironmentDoesNotInheritParent |
| Wire memory | Unbounded downstream JSON/stdio/SSE message reads | internal/wirelimit framing/chunk/boundary tests; wrapped production transports |
| Discovery memory | Pagination could accumulate more definitions/bytes than publication allowed | Cross-page discovery budget before catalog filtering |
| Local token UX | Deleting the final token could silently remove local authentication | TestLocalAdminCannotDeleteFinalMCPToken; disabled final-delete control |
| Frontend revision coherence | Config and token responses could come from different ETag revisions | Chromium mismatched-revision regression |
| Frontend credential cleanup | Logout/session expiry could retain tokens in hidden DOM and late responses | Chromium masking/logout/late-response regressions |
| Frontend samples | Masked token was still visible in Header and process-argument examples | Masked Header, explicit copy, environment-based bridge instructions |
| Frontend responsiveness | Unbounded requests / overlapping status polls / stale boot responses | Request deadlines, epoch checks, full Chromium regressions |
| Key-value editing | Prototype-like keys could be lost; case-folded duplicate names accepted | app-core unit regressions |
| Windows maintenance | Fixed port/readiness dependency made authenticated or degraded configs inconvenient to repair | Actual CMD/PS5.1 smoke with custom port, authentication, unavailable downstream, BOM and occupied port |
| Windows copy status | Clipboard success could be asserted after copy failed | Explicit copy outcome; clear fallback guidance |
| Shutdown / status | Error paths could retain resources; Admin uptime stayed zero | Bounded deferred cleanup, cancellation-aware signal loop, real process/browser smoke |
| Reconnect pressure | Ready-then-disconnected peers could reconnect without normal backoff | Use the existing bounded jitter/backoff path after disconnect |
| Release chain | Repeated one-shot publishers risked divergent test/build logic | One reusable canonical workflow; build once, checksum, native checks, publish exact artifacts |

## Verification performed during implementation

- Eleven focused defect groups failed against the unmodified baseline, then passed
  against the fixes. The baseline comparison log was retained outside the repository.
- Full Go test suite passed after the core and Windows-adjacent changes.
- Full Go race suite passed after the first two hardening phases; final-head CI must
  rerun it after all subsequent changes before merge.
- go vet and the independent SDK probe passed.
- Six frontend unit tests and 26 actual Chromium tests passed on Linux, including
  responsive layouts at 320/390/768/1024/1440 pixels, focus, reduced motion and pagination.
- Real Manager + Chromium smoke passed for login, cookies/CSRF, ETag-safe editing,
  persistent config writes, preserved secrets and restart-required state.
- Linux govulncheck with x/vuln v1.7.0 reported no known reachable vulnerabilities at
  scan time. npm reported zero vulnerabilities in the small browser-test dependency tree.
- Windows verification is not inferred from Linux: final-head CI and the release's
  exact ZIP job must complete successfully. Windows ARM64 native execution is not
  claimed by an amd64 runner. Check the actual workflow logs for final results.

## Remaining operating requirements

See docs/PRODUCTION.md. A monitored deployment-specific canary/soak, backup restoration,
actual downstream/client/proxy checks, resource budgets and periodic vulnerability scans
remain necessary. Short automated runs do not prove days or months of stability.

The project remains a trusted single-operator gateway, not a multi-tenant sandbox or
HA control plane. Admin privileges can configure executable code. Clipboard history,
local OS permissions and downstream supply-chain pinning remain operator concerns.
No code-signing/SmartScreen guarantee or full MCP 2026-07-28 coverage is asserted.
Branch protection/rulesets and a distribution license require repository-owner policy
choices; this audit does not silently change either one.

Do not label unexecuted checks PASS. Attach final-head CI, native archive verification
and release identity to the PR/release instead of treating this document as a static
certificate of future versions.
