# MCP Hub architecture

## Design goals

MCP Hub keeps one stable client-facing endpoint while downstream MCP servers can start, stop, reconnect, change their tool catalogs, or be reconfigured.

The runtime favors explicit limits, conflict detection, and safe failure over hidden fallback or automatic retries that could repeat side effects.

The production shape remains one Go binary. Browser assets and Agent deployment assets are embedded into that binary.

## Package boundaries

| Package | Owns | Must not own |
|---|---|---|
| `internal/config` | strict decode, validation, environment/path resolution, config locking, atomic persistence | listeners, child-process lifecycle, MCP routing |
| `internal/runtime` | config observation, revision application, restart-required state, transaction coordination | CLI parsing, protocol routing |
| `internal/manager` | downstream desired state, generations, leases, reconnect/backoff | HTTP handlers, config-file parsing |
| `internal/downstream` | SDK client sessions and HTTP/stdio transport adaptation | public naming and inbound admission |
| `internal/catalog` | immutable tool/route snapshots, published names, unpublished counts | tool execution |
| `internal/router` | lease acquisition, call deadlines, result/error mapping, call summaries | downstream lifecycle |
| `internal/inbound` | `/mcp`, diagnostics, Host/Origin/auth/session admission boundary | configuration mutation |
| `internal/admin` | Admin sessions, browser management plane, authenticated config mutation, embedded Admin/Agent assets | downstream lifecycle policy outside transaction hooks |
| `internal/bridge` | stdio-only client compatibility | starting a second downstream set |
| `internal/cli` | CLI parsing/composition, diagnostics, stdio bridge entry, Remote Admin client | runtime reload state machine |
| `internal/process` | cross-platform stdio process-tree ownership | config persistence |
| `internal/buildinfo` | product/version/build identity | runtime behavior |

## Data plane

```text
HTTP MCP client ------------------------------+
                                               |
stdio-only client -> mcp-hub stdio -----------+--> inbound /mcp
                                                    |
                                                    v
                                             SDK MCP Server
                                                    |
                                                Publisher
                                                    |
                                                 Catalog
                                                    |
                                                  Router
                                                    |
                                            Manager / leases
                                              /          \
                                           stdio      HTTP MCP
```

Runtime flow:

1. `config.LoadFile` strictly decodes, validates, and resolves a complete configuration snapshot.
2. `serve` validates startup policy and binds the listener before starting downstream coordination.
3. The manager reconciles one coordinator/generation lineage per configured server.
4. Ready downstreams publish validated tools into immutable catalog snapshots.
5. The inbound SDK server lists the current published catalog.
6. The router resolves a public tool through the exact route snapshot.
7. A call acquires a generation lease, executes once with a bounded context, records a sanitized summary, and releases the lease.
8. Tool-directory changes update publication without inventing alternate routing rules.

## Management plane

```text
Browser /admin/ --------+
                         |
mcp-hub admin CLI -------+--> Admin API
                         |       |
Trusted Agent automation-+       v
                         session/auth/CSRF
                                 |
                                 v
                          config transaction
                       /      |       |      \
                    CAS   validate  preflight  lock
                                 |
                                 v
                         atomic persistence
                                 |
                                 v
                          runtime application
                                 |
                        rollback on failure
```

Browser Admin, Remote Admin CLI, and trusted Agent automation intentionally converge on one server-side transaction path. They should not maintain separate mutation semantics.

The persistent JSON file remains the source of truth.

## Configuration transaction

The shared transaction domain coordinates Admin writes and runtime observation.

Important ordering:

1. bound and decode untrusted Admin request body **before** taking the shared config transaction lock;
2. load the current raw config inside the transaction;
3. verify current ETag / digest;
4. apply and strictly validate the requested mutation;
5. preflight new or connection-changing downstreams outside the live routing table but within the protected transaction semantics;
6. persist through the OS-backed advisory lock and atomic-write path;
7. apply the validated runtime snapshot;
8. rollback persistence and restore the prior runtime snapshot if live apply fails.

This prevents slow network uploads from stalling config polling while still protecting the read/CAS/persist/reload critical section.

## Runtime revisions and generations

The runtime controller rejects stale snapshots.

Connection-level downstream changes use generation replacement. Existing admitted calls keep a lease on their original generation; removed/replaced generations drain before final cleanup.

Startup-bound Hub changes are represented as `restartRequired` rather than being partially pretended to be hot-swapped.

A restart-required credential/listener change may leave the old live listener/credential behavior active until the process actually restarts.

## Tool publication invariants

- A public tool route identifies exactly one server and one original downstream tool name.
- Public names are deterministic and validated before SDK publication.
- Tool schemas/names are validated before publication.
- Disabled tools are excluded from the published catalog.
- Publication is revision-aware.
- Calls are not automatically replayed after timeout, cancellation, or transport failure.

## stdio process boundary

stdio commands execute directly with separate arguments unless the operator explicitly chooses a shell.

Children inherit a safe compatibility environment baseline rather than the Hub process environment wholesale. Additional values are explicit server `env` opt-in.

Process ownership:

- POSIX: independent process groups and group termination escalation;
- Windows: worker bootstrap + non-inheritable Job Object with kill-on-close semantics.

This is lifecycle ownership, not a sandbox guarantee.

## Streamable HTTP boundary

Remote non-loopback downstreams require HTTPS. Redirects are rejected. Configured headers cannot override transport/session framing headers.

The project uses MCP Go SDK v1.7.0 while keeping the documented current compatibility model. SDK version alone does not expand the advertised protocol contract. See [COMPATIBILITY.md](COMPATIBILITY.md).

## Browser Admin UI

The browser console uses embedded HTML/CSS plus an ES module entrypoint and pure helper module.

Properties include:

- same-origin CSP with no external frontend runtime;
- no inline event-handler dependency;
- responsive desktop/mobile layouts;
- accessible dialog/filter/focus behavior;
- reduced-motion handling;
- redacted secret editing;
- editor-open ETag snapshot;
- config + ETag publication together only after complete refresh success.

Pure helper behavior is tested with Node's built-in runner. Chromium regressions exercise production DOM/CSS/JS with a stubbed Admin API; a separate real-Hub Chromium smoke covers selected backend/browser integration contracts.

## Remote Admin CLI

`internal/cli` implements the Remote Admin client for `list/get/add/edit/delete`.

It authenticates to the Admin API, keeps the server-issued session/CSRF state, consumes redacted config, and preserves ETag/CAS on mutations. Remote non-loopback plaintext HTTP and redirects are rejected.

See [REMOTE-ADMIN.md](REMOTE-ADMIN.md).

## Core security invariants

- Public mode requires HTTPS evidence from direct TLS or an explicitly trusted proxy.
- MCP and Admin credentials are separate.
- Browser mutations require authenticated session, same-origin request, JSON media type, CSRF token, and current ETag.
- Remote CLI mutations retain the same session/CSRF/ETag semantics.
- Existing secret values are not returned by normal Admin reads.
- Hub auth values are not implicitly inherited by stdio children.
- Diagnostic surfaces are intentionally sanitized.
- Routing does not guess destinations from arbitrary tool names.
- Potentially side-effecting calls are never automatically replayed.

## Change rules

- Add configuration fields in `internal/config` first, with decode/validation/resolution tests.
- Keep reload/revision state in `internal/runtime`.
- Keep downstream lifecycle in manager/downstream/process layers rather than HTTP handlers.
- Keep wire-facing diagnostics scalar and sanitized.
- Treat routing, cancellation, process ownership, authentication, config transaction, and secret handling changes as security-sensitive.
- Add race/integration regressions for concurrency and lifecycle changes.
- Update compatibility/security/implementation documentation when behavior changes.
- Do not expand the advertised MCP capability boundary without direct evidence.

## Verification map

- root unit/integration: `go test ./...`
- concurrency: `go test -race ./...`
- static checks: `go vet ./...`
- independent SDK behavior: `verification/sdkprobe`
- Admin pure helpers: `internal/admin/webtest`
- Browser interaction/design: `internal/admin/browsertest`
- Browser + real Hub smoke: `internal/admin/browsertest/live-smoke.mjs`
- vulnerability gate: `govulncheck` in CI

See [IMPLEMENTATION-STATUS.md](IMPLEMENTATION-STATUS.md) for the current evidence boundary.
