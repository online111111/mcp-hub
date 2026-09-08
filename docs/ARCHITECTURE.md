# MCP Manager architecture

MCP Manager keeps one stable client-facing MCP endpoint while downstream services can start, stop, reconnect, change tool catalogs, or be reconfigured. The runtime favors explicit limits, conflict detection, and safe failure over hidden fallback or automatic retries that could repeat side effects.

> The historical `hub` name remains in configuration field names and some internal package/type vocabulary for v0.4 compatibility. Public product/binary identity is MCP Manager.

## Package boundaries

| Package | Owns |
|---|---|
| `internal/config` | strict decode, validation, environment/path resolution, config locking, atomic persistence |
| `internal/runtime` | config observation, revisions, restart-required state, config transaction coordination |
| `internal/manager` | downstream desired state, generations, leases, reconnect/backoff |
| `internal/downstream` | SDK client sessions and HTTP/stdio transport adaptation |
| `internal/catalog` | immutable tool/route snapshots and public names |
| `internal/router` | lease acquisition, call deadlines, result/error mapping, sanitized call summaries |
| `internal/inbound` | `/mcp`, diagnostics, Host/Origin/auth/session admission boundary |
| `internal/admin` | Browser Admin sessions, authenticated config mutation, embedded Admin/Agent assets |
| `internal/bridge` | stdio-only client compatibility |
| `internal/cli` | CLI, diagnostics, stdio bridge entry, Remote Admin client |
| `internal/process` | cross-platform stdio process-tree ownership |
| `internal/buildinfo` | public product/version/build identity |

## Data plane

```text
HTTP MCP client -------------------------------+
                                                |
stdio-only client -> mcp-manager stdio --------+--> inbound /mcp
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

A call resolves through an immutable route snapshot, acquires one generation lease, executes once with a bounded context, records a sanitized summary, and releases the lease. Calls are never automatically replayed after timeout, cancellation, or transport failure.

## Management plane

```text
Browser /admin/ ------------+
                             |
mcp-manager admin -----------+--> Admin API
                             |       |
trusted Agent automation ----+       v
                         auth/session/CSRF
                                     |
                                     v
                              config transaction
                           /      |       |      \
                         CAS   validate  preflight lock
                                     |
                              atomic persistence
                                     |
                              runtime application
                                     |
                              rollback on failure
```

Browser Admin, Remote Admin, and trusted Agent automation converge on one server-side transaction path. The JSON file remains the persistent source of truth.

## Config transaction ordering

1. Read and bound the untrusted Admin request body before taking the shared transaction lock.
2. Load current config and verify ETag/digest.
3. Apply and strict-validate the requested mutation.
4. Preflight new/connection-changing enabled downstreams and discovered tools.
5. Persist through OS-backed locking and atomic replacement.
6. Apply the validated runtime snapshot.
7. Roll back persistence/runtime if live apply fails.

External file observation and Admin writes share the same revision domain so stale snapshots are rejected.

## Generations and reload

Connection-level downstream changes use break-before-make generation replacement. Existing admitted calls retain their original generation lease. Startup-bound listener/auth/Admin changes set `restartRequired`; MCP Manager does not pretend those changes are fully hot-swapped before a process restart.

## Process and transport boundaries

stdio children receive a safe compatibility environment baseline plus explicit server `env`, not the entire Manager process environment. POSIX uses process groups; Windows uses a worker bootstrap plus Job Objects. This is lifecycle ownership, not sandboxing.

Remote non-loopback Streamable HTTP requires HTTPS. Redirects and unsafe transport/session header overrides are rejected.

## Admin UI and Remote Admin

The embedded browser console uses same-origin assets, CSP, CSRF, redacted secret editing, ETag/CAS, responsive layouts, keyboard accessibility, and reduced-motion handling. The Remote Admin CLI preserves the same authenticated transaction semantics.

The embedded `mcp-manager-deployer` Skill uses those public interfaces for deployment/migration automation rather than defining a separate privileged mutation path.

## Product rename boundary

The v0.4 rename changes:

- public product identity: `MCP Manager`
- official binary/CLI: `mcp-manager`
- new credential variable names: `MCP_MANAGER_TOKEN`, `MCP_MANAGER_ADMIN_TOKEN`
- Release asset prefix: `mcp-manager-`
- deployer Skill: `mcp-manager-deployer`

It intentionally does **not** change:

- `/mcp`, `/admin/`, `/api/...` routes
- JSON `hub.*` schema fields
- current runtime semantics
- internal Go module path `mcp-manager` during the low-risk v0.4 migration

## Protocol boundary

The project uses MCP Go SDK v1.7.0. The SDK dependency version alone is not a claim of full native support for every MCP 2026-07-28 feature. See [COMPATIBILITY.md](COMPATIBILITY.md).

## Verification map

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `verification/sdkprobe`
- Node Admin helper tests
- Chromium interaction/design regressions
- real-MCP-Manager Chromium smoke
- `govulncheck`

See [IMPLEMENTATION-STATUS.md](IMPLEMENTATION-STATUS.md) for evidence boundaries.
