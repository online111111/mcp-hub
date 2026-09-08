# MCP Hub security model

MCP Hub is a trusted personal gateway, not a sandbox or a multi-tenant security boundary. Configured stdio services execute with the Hub user's operating-system privileges, and remote MCP services are trusted peers from the Hub's perspective.

## Inbound HTTP boundary

- Local mode is restricted to loopback (`127.0.0.1`, `localhost`, or `[::1]`).
- Public mode must be enabled explicitly and satisfy HTTPS, Host, proxy, and authentication validation before startup.
- MCP and diagnostic endpoints validate `Host`; the Hub does not trust `X-Forwarded-Host` as authority.
- MCP and diagnostic endpoints reject browser `Origin` requests. The browser Admin surface has its own exact same-origin and CSRF policy.
- Public MCP and diagnostic requests require the configured MCP bearer token. `/healthz` remains unauthenticated but still follows Host/HTTPS policy.
- Request and response bodies are bounded. Stateful session admission and established-session POST reads have explicit limits/deadlines.

For public hosting, keep the Hub on loopback and terminate TLS at a trusted local reverse proxy. Only proxy addresses listed in `trustedProxies` may establish HTTPS through `X-Forwarded-Proto`. Never add an untrusted public network to that list.

## Admin management plane

The management plane has three supported clients:

1. the browser console under `/admin/`;
2. the `mcp-hub admin` remote CLI;
3. trusted Agent automation that uses the same CLI/Admin API path.

They all converge on the same authenticated Admin transaction path. There is no separate low-security remote-write API.

Security properties include:

- MCP and Admin tokens are distinct credentials;
- public-mode tokens must satisfy the configured minimum length policy and must not be identical;
- successful Admin login creates a bounded random server-side session;
- cookies are `HttpOnly`, `SameSite=Strict`, and `Secure` whenever HTTPS is active;
- login and authenticated Admin API traffic are rate-limited;
- mutation requests require `application/json` and a synchronizer CSRF token;
- configuration writes require the current ETag / compare-and-swap revision;
- unknown/trailing JSON is rejected;
- connection-changing server edits are preflighted before persistence;
- persistence uses the shared config lock and atomic-write path;
- failed transactions retain or restore the last healthy configuration/runtime state;
- normal Admin reads use secret placeholders rather than returning existing secret values.

The browser editor snapshots the ETag when it opens. A later background refresh cannot silently authorize a stale draft against a newer revision.

The remote CLI also rejects redirects and non-loopback plaintext HTTP. See [REMOTE-ADMIN.md](REMOTE-ADMIN.md).

## Configuration transaction safety

Configuration on disk remains the persistent source of truth. File polling and Admin writes share one transaction domain.

Important invariants:

- request bodies are size-bounded;
- Admin PUT network bodies are read before the shared config transaction lock is acquired, so a slow upload cannot stall unrelated runtime reload work;
- raw config read, revision comparison, validation, mutation, persistence, and reload are serialized appropriately;
- cross-process writes use an OS-backed advisory lock;
- config replacement is atomic and durability-oriented;
- stale ETags fail with a conflict instead of overwriting a concurrent administrator.

Do not run a direct config-file editor concurrently with Browser/CLI Admin writes unless you intentionally accept conflict/reload behavior.

## Secrets

Prefer `${NAME}` references in configuration and inject the real value through the Hub service environment.

- Environment/header references are expanded only in memory.
- Missing environment references fail validation rather than silently becoming empty strings.
- Expansion is one-pass and does not execute a shell.
- Import previews and Admin reads expose key names and secret placeholders, not existing values.
- Status/recent-call diagnostics intentionally avoid returning commands, arguments, filesystem paths, environment values, request parameters, results, raw stacks, headers, or session credentials.

Do not put long-lived secrets in Git, screenshots, PR text, shell history, service command-line arguments, or final support reports.

## stdio environment and process execution

stdio children do **not** inherit the entire Hub process environment. They receive a compatibility allowlist, while additional values must be opted into explicitly through the server `env` map.

Hub authentication values such as `MCP_HUB_TOKEN` and `MCP_HUB_ADMIN_TOKEN` are not implicitly inherited by downstream children. If an operator explicitly forwards a value in server `env`, that is an intentional trust decision.

Ordinary commands use direct process execution with separate arguments, not shell interpolation. Explicitly configuring `cmd.exe`, `cmd`, PowerShell, `/bin/sh`, or another shell opts into that shell's parsing and escaping semantics.

Process-tree ownership:

- POSIX: downstreams run in process groups and cancellation/close escalates against the group;
- Windows: a worker and Job Object with kill-on-close semantics manages the descendant tree.

A program that deliberately escapes OS process ownership controls is outside the Hub's sandbox guarantee because MCP Hub is not a sandbox.

## Downstream Streamable HTTP

Remote non-loopback MCP targets require HTTPS.

The downstream client:

- rejects redirects;
- validates target policy;
- clones requests before adding configured headers;
- prevents configured headers from replacing hop/session framing headers;
- bounds response handling;
- uses startup/call contexts instead of a short global timeout that would incorrectly kill a long-lived MCP connection.

External transport/protocol errors are mapped to short categories. Avoid verbose production logging of raw downstream errors because they may contain private endpoint or credential-adjacent information.

## Tool-call semantics

Routing uses an explicit public-name -> server/original-name mapping. The Hub does not guess destinations from arbitrary names.

Tool calls are not automatically replayed after timeout, cancellation, or transport failure. A timeout does not imply the downstream side effect was rolled back.

Tool names and schemas are validated before publication. Disabled tools are filtered declaratively; newly discovered valid tools are otherwise published according to the configured policy.

## Public deployment checklist

- Hub listens on loopback when practical.
- Only 80/443 are public; do not expose 8080 directly.
- TLS is terminated by a trusted proxy or by the Hub's validated HTTPS path.
- `publicUrl`, `allowedHosts`, and `trustedProxies` are explicit.
- MCP and Admin tokens are strong, distinct, and stored outside Git.
- Hub runs as a dedicated non-root service account.
- Config/env files have restrictive permissions (for example 0600 where applicable).
- Only trusted stdio commands and remote MCP endpoints are configured.
- Upgrades run validation, `status`, and `doctor` before being considered complete.

See [VPS.md](VPS.md) for the deployment baseline.

## Evidence boundary

The repository has automated security regression coverage for the implemented Host/Origin/auth/session/CSRF/config/redirect/secret/process boundaries and runs `govulncheck` in CI. This is not a claim that every reverse proxy, third-party MCP server, operating-system policy, or hostile local workload has been penetration-tested.

Current verification boundaries are tracked in [IMPLEMENTATION-STATUS.md](IMPLEMENTATION-STATUS.md).
