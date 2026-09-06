# MCP Hub architecture

## Design goals

MCP Hub keeps one stable client-facing endpoint while downstream MCP servers can
start, stop, reconnect, change their tool catalogs, or be reconfigured. The
runtime favors explicit limits and safe failure over automatic retries that
might repeat side effects.

## Package boundaries

| Package | Owns | Must not own |
|---|---|---|
| `internal/config` | strict decode, validation, environment/path resolution, atomic persistence | listeners, child processes, network sessions |
| `internal/runtime` | config-file observation, stable-sample reloads, restart-required state | CLI parsing, protocol routing, secret persistence |
| `internal/manager` | downstream desired state, generations, leases, reconnect/backoff | config-file polling, HTTP handlers |
| `internal/downstream` | SDK client sessions and HTTP/stdio transport adaptation | public naming and inbound admission |
| `internal/catalog` | immutable tool/route snapshots and public names | tool execution |
| `internal/router` | lease acquisition, call deadlines, result/error mapping, call summaries | downstream lifecycle |
| `internal/inbound` | Streamable HTTP server, admission limits, Host/Origin/auth boundary | configuration writes |
| `internal/admin` | authenticated browser sessions and CAS-backed config editing | downstream lifecycle decisions |
| `internal/bridge` | stdio-only client compatibility | starting a second Hub or downstream set |
| `internal/cli` | command parsing and process composition | reload state machine |
| `internal/buildinfo` | product name and version | runtime behavior |

## Runtime flow

1. `config.LoadFile` strictly decodes, validates, and resolves a complete
   configuration snapshot.
2. `serve` binds the listener before starting downstream work.
3. The manager reconciles one coordinator per configured server.
4. Ready coordinators publish tools into an immutable catalog snapshot.
5. The inbound SDK server lists that snapshot; the router resolves every public
   tool name through the corresponding route snapshot.
6. A call acquires a generation lease, executes once with a bounded context,
   records a sanitized summary, and releases the lease.
7. The runtime controller watches for two identical changed file samples before
   applying an external edit. Admin writes call the same controller immediately
   after atomic persistence.

## Core invariants

- A public tool route always identifies one server and original tool name.
- Calls are never automatically replayed after timeout, cancellation, or
  transport failure.
- Connection-level changes are break-before-make; old tools are revoked before
  a removed generation drains.
- Config secrets are resolved only in memory and are never returned by status
  APIs or the admin config endpoint.
- Hub authentication values are not inherited implicitly by stdio children.
- Public mode requires HTTPS evidence from TLS or an explicitly trusted proxy.
- Browser mutations require an authenticated session, same-origin request,
  JSON media type, CSRF token, and current configuration ETag.

## Admin UI

The browser console uses an ES module entrypoint plus a small pure helper module.
No inline event handlers are used because the admin CSP permits only same-origin
scripts. Pure formatting, filtering, and secret-preservation behavior is tested
with Node's built-in test runner; server-side security and persistence behavior
is covered by Go tests.

## Change rules

- Add configuration fields in `internal/config` first, with strict decode,
  validation, resolution, and fixture coverage.
- Put live file/reload state in `internal/runtime`, not CLI or manager.
- Keep wire-facing DTOs scalar and sanitized.
- Treat changes to routing, cancellation, process ownership, authentication, or
  secret handling as security-sensitive and add race/integration regressions.
