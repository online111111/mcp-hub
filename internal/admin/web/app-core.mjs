export const SECRET_SENTINEL = "__MCP_HUB_SECRET_SET__";

export function escapeHTML(value) {
  return String(value ?? "").replace(
    /[&<>"']/g,
    (character) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[character],
  );
}

export function nonEmptyLines(value) {
  return String(value ?? "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

export function collectKeyValues(entries) {
  const entriesOut = [];
  const seen = new Set();
  for (const entry of entries) {
    const key = String(entry.key ?? "").trim();
    if (!key) continue;
    if (seen.has(key.toLowerCase())) {
      throw new Error(`名称“${key}”重复`);
    }
    seen.add(key.toLowerCase());
    entriesOut.push([key, entry.preserveSecret && !entry.value
      ? SECRET_SENTINEL : String(entry.value ?? "")]);
  }
  return Object.fromEntries(entriesOut);
}

export function formatUptime(seconds) {
  const value = Math.max(0, Number(seconds) || 0);
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  if (days) return `${days}天 ${hours}小时`;
  if (hours) return `${hours}小时 ${minutes}分`;
  return `${minutes}分钟`;
}

export function formatTimestamp(value, locale = "zh-CN") {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? String(value)
    : date.toLocaleString(locale, { hour12: false });
}

export function statusLabel(status) {
  return (
    {
      ready: "可用",
      connecting: "连接中",
      starting: "启动中",
      backoff: "等待重试",
      unavailable: "不可用",
      draining: "正在停止",
      disabled: "已停用",
      stopped: "已停止",
      closed: "已关闭",
    }[status] ||
    status ||
    "未知"
  );
}

export function filterCalls(calls, query, outcome) {
  const needle = String(query ?? "")
    .trim()
    .toLowerCase();
  return [...(calls || [])].reverse().filter((call) => {
    const matchesQuery =
      !needle ||
      String(call.tool || "")
        .toLowerCase()
        .includes(needle) ||
      String(call.serverId || "")
        .toLowerCase()
        .includes(needle);
    const matchesOutcome =
      outcome === "all" ||
      (outcome === "error"
        ? call.outcome !== "success"
        : call.outcome === outcome);
    return matchesQuery && matchesOutcome;
  });
}

export function normalizeServer(server = {}) {
  return {
    enabled: server.enabled !== false,
    type: server.type || "stdio",
    command: server.command || "",
    args: Array.isArray(server.args) ? server.args : [],
    cwd: server.cwd || "",
    env: server.env && typeof server.env === "object" ? server.env : {},
    url: server.url || "",
    headers:
      server.headers && typeof server.headers === "object"
        ? server.headers
        : {},
    startupTimeout: server.startupTimeout || "",
    callTimeout: server.callTimeout || "",
    maxConcurrency: server.maxConcurrency || "",
    tools: {
      disabled: Array.isArray(server.tools?.disabled)
        ? server.tools.disabled
        : [],
    },
  };
}
