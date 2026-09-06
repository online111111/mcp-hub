# MCP Hub security model

MCP Hub is a local personal gateway, not a sandbox or an authentication boundary against other processes running as the same user. Downstream services execute with the Hub user's permissions.

## Local HTTP boundary

- The configured listener is restricted to loopback (`127.0.0.1`, `localhost`, or `[::1]`).
- The inbound handler validates `Host` against the bound loopback host/port and does not trust `X-Forwarded-Host` or other proxy headers.
- Requests containing an `Origin` header are rejected in P0, including `Origin: null`; there is no browser UI or CORS policy.
- `/healthz`, `/readyz`, and `/api/v1/status` are read-only and send `Cache-Control: no-store`.
- Request bodies are capped at 8 MiB and upstream stateful sessions are capped at 32. Initial session admission is serialized and has a bounded read deadline.

If the Hub is placed behind a reverse proxy, the proxy must preserve this local-only trust model. Do not expose `/mcp` to a network without designing authentication, authorization, CSRF, and origin policy together.

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

Before exposing the Hub beyond the local machine, add a deliberate security design and tests for authentication and origin/CSRF handling. This P0 build intentionally does not claim that protection.
