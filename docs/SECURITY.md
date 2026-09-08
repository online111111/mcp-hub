# MCP Manager security model

MCP Manager is a trusted personal gateway, not a sandbox or multi-tenant security boundary. Configured stdio services execute with the Manager service user's OS privileges; remote MCP services are trusted peers from the Manager's perspective.

## Inbound HTTP

- Local mode is loopback-only.
- Public mode is explicit and must satisfy HTTPS, Host, proxy, and authentication policy before startup.
- MCP/diagnostic endpoints validate `Host` and reject browser `Origin` requests; Browser Admin has its own strict same-origin + CSRF policy.
- Configured MCP tokens protect MCP/diagnostic requests in both local and public mode. Public mode requires tokens; token-free local mode is explicitly unauthenticated. `/healthz` is the intentional non-secret liveness exception.
- Admin refuses deletion of the final MCP token. Rotate by adding a replacement first; disabling local authentication requires an explicit offline configuration change.
- Request/response/session resources are bounded.

For public hosting keep MCP Manager on loopback and terminate TLS at a trusted local proxy. Never place an untrusted public network in `trustedProxies`.

## Admin management plane

Supported management clients are:

1. Browser Admin under `/admin/`;
2. `mcp-manager admin` Remote Admin CLI;
3. trusted Agent automation using the same CLI/Admin path.

They converge on the same authenticated transaction semantics: bounded sessions/rates, HttpOnly/SameSite cookies, Secure cookies under HTTPS, JSON-only mutation requests, CSRF, ETag/CAS, strict decoding, downstream preflight, atomic persistence, rollback, and redacted secret reads.

Remote Admin additionally rejects redirects and non-loopback plaintext HTTP. See [REMOTE-ADMIN.md](REMOTE-ADMIN.md).

## Credential names and rename compatibility

New deployments use:

- `MCP_MANAGER_TOKEN` for public MCP clients;
- `MCP_MANAGER_ADMIN_TOKEN` for Admin access.

The CLI accepts legacy `MCP_HUB_TOKEN` / `MCP_HUB_ADMIN_TOKEN` during the rename compatibility window. Existing config references to the legacy variables may remain. Do not expose, duplicate, or rotate secrets merely to change naming.

New and legacy auth values are never meant to be implicitly inherited by stdio children; explicit server `env` forwarding is an intentional operator trust decision.

## Config transaction safety

Configuration on disk remains the persistent source of truth. File polling and Admin writes share one revision/transaction domain.

- Admin bodies are bounded and read before taking the shared config transaction lock.
- Current raw config and ETag/digest are verified before mutation.
- Cross-process writes use OS-backed locking.
- Replacement is atomic/durability-oriented.
- Stale ETags fail as conflicts rather than silently overwriting concurrent changes.
- Failed live apply restores the previous healthy persistence/runtime state.

## Secrets

Prefer `${NAME}` references and inject values through the Manager service environment. Expansion is one-pass, in-memory only, shell-free, and missing required variables fail validation.

Do not put long-lived secrets in Git, screenshots, PR text, shell history, process arguments, or support reports.

## stdio execution

stdio children receive a restricted compatibility environment, not the full Manager process environment. Ordinary commands execute directly with separate arguments. Explicitly configuring a shell opts into that shell's parsing/escaping risk.

POSIX uses process-group ownership; Windows uses a worker bootstrap plus Job Object lifecycle. These are process ownership controls, not sandbox guarantees.

## Downstream HTTP

Remote non-loopback MCP targets require HTTPS. Redirects are rejected, configured headers cannot replace transport/session framing headers, and responses are bounded. Startup/call contexts are used instead of an unsafe short global timeout for long-lived MCP connections.

## Tool-call semantics

Routing uses explicit public-name -> server/original-name mappings. Calls are never automatically replayed after timeout, cancellation, or transport failure. A timeout does not mean a downstream side effect was undone.

## Public deployment checklist

- Manager listens on loopback where practical.
- Only 80/443 are public; do not expose 8080.
- HTTPS and trusted-proxy settings are explicit.
- MCP/Admin credentials are strong and distinct.
- Manager runs as a dedicated non-root account.
- Config/env files use restrictive permissions.
- Only trusted stdio commands and remote MCP endpoints are configured.
- Upgrades run validate, status, doctor, and rollback on failure.

See [VPS.md](VPS.md) and [production runbook](PRODUCTION.md) for operational/evidence boundaries.
