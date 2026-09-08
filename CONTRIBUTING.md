# Contributing to MCP Manager

Thanks for helping improve MCP Manager.

## Before opening an issue

- Check existing issues and pull requests for duplicates.
- Use the latest release when practical, or include the exact commit SHA you tested.
- Remove tokens, credentials, private hostnames, and other secrets from logs or screenshots.
- For deployment and security questions, read `docs/VPS.md`, `docs/SECURITY.md`, and `docs/PRODUCTION.md` first.

## Bug reports

Please include:

- MCP Manager version or commit SHA
- operating system and architecture
- downstream transport (`stdio` or Streamable HTTP)
- expected behavior
- actual behavior
- minimal reproduction steps
- relevant logs with secrets removed

For Windows launcher issues, also include the Windows and PowerShell versions when known.

## Feature requests

Describe the problem or workflow first, then the proposed behavior. Please note whether the request affects compatibility, configuration, `/mcp`, `/admin`, authentication, or downstream lifecycle semantics.

## Pull requests

1. Base changes on the current `main` branch.
2. Keep compatibility contracts intact unless the change explicitly includes a migration plan.
3. Add or update tests for behavioral changes.
4. Run the relevant verification before opening the PR:

```bash
go test -count=1 -timeout 180s ./...
go test -race -count=1 -timeout 240s ./...
go vet ./...
node --check internal/admin/web/app.js
node --test internal/admin/webtest/*.test.mjs
go build -trimpath -o dist/mcp-manager ./cmd/mcp-manager
```

For Admin UI changes, also run the repository browser regressions described in `AGENTS.md`.

## Compatibility and security

Please read `AGENTS.md` before substantial implementation work. In particular:

- preserve the historical `hub.*` configuration contract;
- preserve `/mcp` and `/admin` compatibility unless intentionally migrated;
- do not reintroduce the old repository or binary identity;
- do not weaken authentication, Host/Origin validation, bounded reads, ETag/CAS behavior, rollback, or no-replay guarantees;
- never commit real tokens or secrets.

## Community

- LINUX DO: https://linux.do/
- 烧饼论坛: https://sb.sb/

GitHub Issues remain the preferred place for actionable bugs and feature requests.

## License

By contributing, you agree that your contributions will be licensed under the repository's MIT License.
