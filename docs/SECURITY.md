# MCP Hub security model

MCP Hub is a local personal gateway, not a sandbox or an authentication boundary against other processes running as the same user. Downstream services execute with the Hub user's permissions.

## Inbound HTTP boundary

- Local mode restricts the listener to loopback (`127.0.0.1`, `localhost`, or `[::1]`). Public mode must be enabled explicitly and satisfy the authentication/HTTPS policy during configuration validation.
- The inbound handler validates `Host` against the bound loopback host/port or the explicit public allowlist. It never trusts `X-Forwarded-Host`.
- MCP and diagnostic endpoints reject every request containing `Origin`, including `Origin: null`. The admin handler is the only browser surface and independently enforces exact same-origin requests plus CSRF tokens for mutations.
- Public MCP and diagnostic requests require the configured bearer token; `/healthz` remains credential-free but still follows the public HTTPS and Host policy.
- `/healthz`, `/readyz`, and `/api/v1/status` are read-only and send `Cache-Control: no-store`.
- Request bodies are capped at 8 MiB and upstream stateful sessions are capped at 32. Initial session admission is serialized and has a bounded read deadline.

In public mode, only direct TLS or `X-Forwarded-Proto: https` from a configured trusted-proxy CIDR is accepted. Keep the Hub listener on loopback where practical, preserve the original Host, and never trust a public network as a proxy CIDR.

## Admin browser boundary

- MCP and admin bearer tokens are distinct credentials.
- Successful login creates a bounded random server-side session. Cookies are `HttpOnly`, `SameSite=Strict`, and `Secure` whenever HTTPS is in use.
- Login attempts and authenticated API traffic are rate-limited. Login-source tracking is also bounded to prevent an attacker from growing it without limit.
- Mutations accept only the `application/json` media type, reject unknown fields and trailing JSON values, require a synchronizer CSRF token, and use the current config ETag for compare-and-swap persistence.
- The Content Security Policy allows scripts only from the same origin. The UI uses module scripts and registered event listeners; it does not rely on inline handlers.

## Configuration secrets

Environment variables and HTTP header values are expanded only in memory. Import previews show only environment/header names. Status and recent-call diagnostics never include URLs, commands, arguments, paths, environment values, headers, sessions, parameters, results, or raw stacks.

Do not put secrets directly in command-line arguments or commit them to configuration files. `${NAME}` expansion is one-pass and does not execute a shell; `$${NAME}` is literal. Missing variables reject validation rather than silently producing an empty credential.

## Downstream HTTP

Configured transport headers cannot replace MCP/session framing headers. Requests are cloned before custom headers are injected, and redirects are rejected to avoid forwarding credentials to another origin. The HTTP client has no short global timeout that would terminate a persistent SSE lifetime; startup and individual calls have separate contexts.

## Process ownership

On Windows, managed stdio workers use a non-inheritable Job Object with kill-on-close semantics and a bootstrap latch: a worker cannot start the downstream process until the Hub has assigned it to the Job and sent the bounded bootstrap payload. Unsupported batch scripts are rejected unless the user explicitly selects a native shell command. On POSIX, managed processes use a process group with termination escalation; a process that deliberately escapes its group is outside the sandbox guarantee.

The Hub does not use shell interpolation for ordinary commands. Arguments remain separate values. A user who configures `cmd.exe` or another shell has explicitly accepted that shell's semantics and risks.

## Tool calls and diagnostics

Routing uses an explicit public-name-to-server/original-name table. Calls carry raw JSON arguments, preserve SDK-expressible result fields, and are never automatically replayed after timeout, cancellation, or transport failure. A timeout does not imply that a downstream side effect was undone.

External transport/protocol failures are mapped to short categories and request IDs. Avoid enabling verbose transport logging in production: raw errors can contain paths, headers, endpoint details, or other sensitive data, and string replacement is not a complete redaction strategy.

## Limits and operational guidance

The implementation enforces bounded config/server/session/tool/call-admission resources described in [`CONFIG.md`](CONFIG.md) and the design baseline. Keep downstream tools trusted, review newly discovered tools, and use `tools.disabled` as a declarative blacklist. New tools are open by default by design.

Public mode protects the Hub transport and browser management plane against unauthenticated network use; it does not make configured downstream tools safe. Run the Hub under a dedicated non-root account and treat every configured stdio command and remote MCP service as trusted code.
