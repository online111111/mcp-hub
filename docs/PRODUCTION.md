# Production operation

## Supported trust and deployment model

MCP Manager is a single-operator, self-hosted gateway for trusted downstream MCP
services. It is not a tenant-isolation boundary or an OS sandbox. A configured
stdio program can access everything permitted to the service account. Do not give
untrusted people Admin access or share the service OS account with hostile programs.

Use an actively supported OS and a pinned release matching its architecture.
Windows amd64 is exercised with the actual packaged CMD launcher and Windows
PowerShell 5.1. Building other architecture archives is not proof of native runtime
validation on those architectures. See each release workflow for exact evidence.

## Deployment baseline

Run a single Manager instance per private config directory. On a VPS, use a dedicated
non-root service account, a service supervisor, and HTTPS termination at a trusted
local reverse proxy. Bind the Manager to loopback; expose only necessary HTTPS/HTTP
ports. Configure publicUrl, allowedHosts and trustedProxies explicitly; do not disable
these checks to make an unexpected proxy arrangement work. See VPS.md.

Store config and environment files outside the repository. On Linux, use a directory
owned by the service account with mode 0700 and files mode 0600. On Windows, use a
private profile directory and verify its ACL; do not extract into a shared public
folder. Do not run the desktop launcher as Administrator merely to avoid permissions
errors. Protect backups with equivalent permissions and retention controls.

Use different random Admin and MCP tokens of at least 32 characters. Put service
secrets in an appropriately protected environment file and reference them explicitly
as ${NAME} in config. Merely setting an environment variable does not substitute for
configuring its reference. Token copies can remain in OS clipboard history, sync
services or third-party clipboard applications; clear them after use.

Local token-free mode is for a trusted single-user computer only. Once MCP tokens are
configured, clients and diagnostics must authenticate even on loopback. The Admin API
will not delete the final MCP token: add a replacement before removing an old token.
Admin/listener/public-security changes require a process restart; MCP token changes
and supported downstream changes hot-reload. Re-check restartRequired after edits.

Pin downstream executable paths, package versions and container images. Avoid
unversioned npx downloads at service startup. Install required runtimes beforehand,
check PATH under the actual service account, and forward only required environment
variables. Explicit shell commands opt into that shell's quoting and execution risk.

## Supervision and resource budgets

For Linux, use systemd Restart=on-failure with a restart delay and StartLimitIntervalSec /
StartLimitBurst appropriate to the host. Keep logs bounded with journald retention.
Set MemoryMax, TasksMax and open-file limits according to representative downstream
loads, not a universal hard-coded number; stdio children consume the same host budget.
Use NoNewPrivileges and filesystem restrictions where compatible with your downstreams.
Validate restrictions against actual tool calls before enabling them in production.

The Windows desktop console must remain open. For unattended Windows operation,
configure a suitable service supervisor and verify stop/restart and child cleanup.
Do not confuse automatic browser opening with installation as a persistent service.

Built-in limits bound individual requests/messages, tool discovery, catalog size,
concurrency and session counts. They do not replace reverse-proxy connection/rate limits,
monitoring, disk quotas or host-level resource controls. SSE streams are long-lived:
avoid a reverse-proxy timeout that kills all otherwise healthy sessions after a short
fixed duration. Use per-request and per-message limits rather than lifetime byte quotas.

## Monitoring

Use /healthz for process liveness. It intentionally exposes no credentials and does not
require an MCP token. Use authenticated status/doctor and /readyz for readiness.
Readiness means no downstream is enabled, or at least one enabled downstream is ready
with published tools. It does NOT mean that every downstream is healthy. Monitor each
server state, tool count, error category, active calls and restartRequired separately.

Alert on repeated reconnect/backoff, growing resource use, low disk space, repeated
configuration rejection and sustained failed calls. Recent-call storage is bounded and
in-memory, not a durable audit log. Ship sanitized service/reverse-proxy logs separately
when durable operational evidence is required; never log bearer tokens or request bodies.

## Changes and upgrades

1. Record the exact release/tag/commit, archive checksum, service definition, config,
   protected environment references, proxy settings and current health.
2. Back up these files with restricted access. Stop a desktop instance before replacing
   its binary. Do not delete config.json; it contains server definitions and credentials.
3. Verify SHA256SUMS and run the new binary's validate command against a backup copy of
   the existing config with the same required environment available.
4. Switch binaries under the service supervisor. Preserve compatible hub.*, /mcp and
   /admin contracts. Do not rotate working credentials just to rename the product.
5. Verify version, health, authenticated readiness, doctor, Admin login, a safe real
   downstream tool call, configuration persistence and restart behavior.
6. Roll back the binary and compatible config snapshot if acceptance fails. Keep rollback
   material until the representative workload has run successfully under monitoring.

Admin uses CSRF and ETag/CAS to reject stale edits. On a conflict, refresh and inspect
current values before reapplying a change. A timeout is not proof a mutation or tool
side effect did not occur; inspect resulting state rather than blindly replaying it.
External manual writers must also respect atomic replacement and coordination. File
locking is not a distributed consensus mechanism; shared-storage multi-writer or HA
operation is not certified by this design.

## Acceptance and evidence limits

Before wide rollout, run a monitored canary with your actual downstreams, client
versions, proxy and credentials. Include restart, disconnected peers, revoked tokens,
invalid config, concurrent edits, backup restoration, and a workload-specific soak.
Track memory, goroutine/process counts and reconnect rates rather than assuming a green
CI run proves multi-day stability. Run vulnerability scans regularly; a clean scan only
addresses known vulnerabilities detected within the analyzed platform/build scope.

The repository's automated regressions and packaged Windows checks are release gates,
not an absolute security certificate. Full MCP 2026-07-28 capability coverage, multi-tenant
isolation, high availability and every third-party client/server are outside this claim.
Repository branch protection and licensing are separate owner decisions; do not infer
that green tests establish either policy.
