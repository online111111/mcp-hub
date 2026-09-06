import test from "node:test";
import assert from "node:assert/strict";

import {
  SECRET_SENTINEL,
  collectKeyValues,
  escapeHTML,
  filterCalls,
  formatUptime,
  nonEmptyLines,
  normalizeServer,
  statusLabel,
} from "../web/app-core.mjs";

test("collectKeyValues preserves redacted secrets and accepts new values", () => {
  assert.deepEqual(
    collectKeyValues([
      { key: "TOKEN", value: "", preserveSecret: true },
      { key: "REGION", value: "us-east", preserveSecret: false },
    ]),
    { TOKEN: SECRET_SENTINEL, REGION: "us-east" },
  );
});

test("collectKeyValues rejects duplicate names", () => {
  assert.throws(
    () => collectKeyValues([{ key: "TOKEN" }, { key: " TOKEN " }]),
    /重复/,
  );
});

test("display helpers handle unsafe and incomplete input", () => {
  assert.equal(
    escapeHTML(`<script>"x"</script>`),
    "&lt;script&gt;&quot;x&quot;&lt;/script&gt;",
  );
  assert.deepEqual(nonEmptyLines(" one\n\n two "), ["one", "two"]);
  assert.equal(formatUptime(90061), "1天 1小时");
  assert.equal(statusLabel("backoff"), "等待重试");
  assert.equal(normalizeServer().type, "stdio");
});

test("filterCalls searches and groups every non-success outcome as an error", () => {
  const calls = [
    { tool: "alpha", serverId: "one", outcome: "success" },
    { tool: "beta", serverId: "two", outcome: "timeout" },
  ];
  assert.deepEqual(filterCalls(calls, "two", "all"), [calls[1]]);
  assert.deepEqual(filterCalls(calls, "", "error"), [calls[1]]);
});
