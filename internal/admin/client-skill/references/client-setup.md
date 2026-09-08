# Client setup reference

Use this reference only after identifying the installed client and its current version.

## Preferred transport order

1. Native remote MCP / Streamable HTTP, when the installed client documents support for it.
2. Local stdio bridge through the `mcp-manager` binary when remote MCP is unavailable.

The remote endpoint normally ends in `/mcp` and authenticates with an MCP access token. The Admin Token is unrelated and must never be placed into client MCP configuration.

## Safe configuration editing

- Locate the actual configuration path used by the installed client instead of assuming one from memory.
- Back up the existing configuration before editing.
- Parse and merge structured configuration where possible.
- Preserve every unrelated MCP server and client setting.
- If a client offers a CLI for MCP configuration, prefer the supported CLI over direct file edits.
- Do not invent configuration keys for a client version you cannot inspect. Use the stdio bridge when that is the safer compatible path.

## stdio bridge fallback

The generic local bridge is:

```text
mcp-manager stdio --connect <MCP_ENDPOINT> --token <MCP_TOKEN>
```

Before using it:

- confirm `mcp-manager` is installed on the client machine;
- prefer the absolute binary path if the client launches with a restricted PATH;
- avoid printing the token in terminal logs when an environment-variable mechanism is available;
- verify the bridge can reach the remote HTTPS endpoint.

## Verification

A successful setup should demonstrate as many of these as the client exposes:

- configuration parses without errors;
- MCP Manager connects successfully;
- tools can be listed;
- a harmless representative tool can be invoked;
- reconnect/reload survives a client restart.

If any of these cannot be tested, state that limitation explicitly rather than marking the setup fully verified.
