import {
  SECRET_SENTINEL,
  collectKeyValues,
  escapeHTML,
  filterCalls,
  formatTimestamp,
  formatUptime,
  nonEmptyLines,
  normalizeServer,
  statusLabel,
} from "./app-core.mjs";

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

const state = {
  csrf: "",
  etag: "",
  config: null,
  status: null,
  editorMode: "form",
  editingId: null,
  refreshing: false,
};

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
    headers.set("X-CSRF-Token", state.csrf);
  }

  let response;
  try {
    response = await fetch(`/api/admin/v1${path}`, {
      ...options,
      headers,
      credentials: "same-origin",
    });
  } catch {
    throw new Error("无法连接到 MCP Hub，请检查服务状态");
  }

  if (response.status === 401) {
    showLogin();
    throw new Error("登录已失效，请重新登录");
  }

  const text = await response.text();
  if (!response.ok) {
    throw new Error(text.trim() || `请求失败 (${response.status})`);
  }

  const etag = response.headers.get("ETag");
  if (etag) state.etag = etag;
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    throw new Error("Hub 返回了无法解析的数据");
  }
}

function showLogin() {
  $("#login").classList.remove("hidden");
  $("#app").classList.add("hidden");
  $("#topActions").classList.add("hidden");
}

function showApp() {
  $("#login").classList.add("hidden");
  $("#app").classList.remove("hidden");
  $("#topActions").classList.remove("hidden");
}

function toast(message, type = "success") {
  const element = document.createElement("div");
  element.className = `toast ${type}`;
  element.textContent = message;
  $("#toastStack").appendChild(element);
  window.setTimeout(() => element.remove(), 3200);
}

function setConnectionState(connected) {
  const pill = $("#headerHealth");
  if (!connected) {
    pill.className = "status-pill danger";
    pill.innerHTML = '<span class="status-dot"></span>连接中断';
    return;
  }
  const needsRestart = Boolean(state.status?.restartRequired);
  pill.className = `status-pill ${needsRestart ? "warn" : "ok"}`;
  pill.innerHTML = `<span class="status-dot"></span>${needsRestart ? "需要重启" : "运行正常"}`;
}

async function boot() {
  try {
    const session = await api("/auth/me");
    state.csrf = session.csrfToken;
    showApp();
    await refreshAll();
  } catch {
    showLogin();
  }
}

async function refreshAll({ announce = false } = {}) {
  if (state.refreshing) return;
  state.refreshing = true;
  try {
    const [config, status] = await Promise.all([
      api("/config"),
      api("/status"),
    ]);
    state.config = config;
    state.status = status;
    render(status);
    setConnectionState(true);
    if (announce) toast("状态已刷新");
  } catch (error) {
    setConnectionState(false);
    if (announce) toast(error.message, "error");
    throw error;
  } finally {
    state.refreshing = false;
  }
}

async function refreshStatus() {
  if (state.refreshing || $("#app").classList.contains("hidden")) return;
  try {
    const status = await api("/status");
    state.status = status;
    render(status);
    setConnectionState(true);
  } catch {
    setConnectionState(false);
  }
}

function render(status) {
  const servers = status.servers || [];
  const calls = status.recentCalls || [];
  const ready = servers.filter((server) => server.state === "ready").length;
  const failed = servers.filter((server) =>
    ["unavailable", "backoff"].includes(server.state),
  ).length;
  const tools = servers.reduce(
    (total, server) => total + (server.publishedToolCount || 0),
    0,
  );
  const active = servers.reduce(
    (total, server) => total + (server.activeCalls || 0),
    0,
  );
  const successes = calls.filter((call) => call.outcome === "success").length;

  $("#health").textContent = status.restartRequired
    ? "需重启"
    : failed
      ? "部分异常"
      : "运行中";
  $("#healthDetail").textContent = status.restartRequired
    ? "监听地址变更需要完整重启"
    : failed
      ? `${failed} 个服务正在重试`
      : "Hub 正常接受请求";
  $("#serverCount").textContent = servers.length;
  $("#serverReadyCount").textContent = `${ready} 个可用`;
  $("#toolCount").textContent = tools;
  $("#activeCallCount").textContent = `${active} 个正在调用`;
  $("#callCount").textContent = calls.length;
  $("#successRate").textContent = calls.length
    ? `成功率 ${Math.round((successes / calls.length) * 100)}%`
    : "暂无数据";
  $("#hubVersion").textContent = status.version || "—";
  $("#hubUptime").textContent = formatUptime(status.uptimeSeconds);
  $("#lastReload").textContent = status.lastReloadStatus || "配置已加载";

  renderServers(servers);
  renderCalls();

  const endpoint = `${location.origin}/mcp`;
  $("#mcpEndpoint").textContent = endpoint;
  $("#stdioCommand").textContent =
    `mcp-hub stdio --connect ${endpoint} --token <MCP_BEARER_TOKEN>`;
}

function renderServers(statuses) {
  const statusByID = new Map(statuses.map((server) => [server.id, server]));
  const configured = state.config?.mcpServers || {};
  const ids = [
    ...new Set([...Object.keys(configured), ...statusByID.keys()]),
  ].sort();
  $("#serverSummary").textContent = `${ids.length} 个服务`;

  if (!ids.length) {
    $("#servers").innerHTML =
      '<div class="empty-state"><strong>还没有下游 MCP 服务</strong><p>点击“添加服务”创建第一个连接。</p></div>';
    return;
  }

  $("#servers").innerHTML = ids
    .map((id) => {
      const raw = configured[id] || {};
      const status = statusByID.get(id) || { id, state: "starting" };
      const type = raw.type === "streamable_http" ? "Streamable HTTP" : "stdio";
      const description =
        raw.type === "streamable_http"
          ? raw.url || "未配置 URL"
          : [raw.command || "未配置命令", ...(raw.args || [])].join(" ");
      const error = status.errorCategory
        ? `<p class="server-error">${escapeHTML(status.errorCategory)}</p>`
        : "";
      return `
        <article class="server-card">
          <div class="server-main">
            <div>
              <div class="server-title-row">
                <h3>${escapeHTML(id)}</h3>
                <span class="badge ${escapeHTML(status.state)}">${escapeHTML(statusLabel(status.state))}</span>
                <span class="badge">${escapeHTML(type)}</span>
              </div>
              <p class="server-desc">${escapeHTML(description)}</p>
              ${error}
              <div class="server-meta">
                <span><b>${status.publishedToolCount || 0}</b> 个工具</span>
                <span><b>${status.activeCalls || 0}</b> 个调用中</span>
                <span>配置版本 <b>${status.activeRevision || "—"}</b></span>
              </div>
            </div>
            <div class="server-actions">
              <button class="button tiny secondary" data-action="edit" data-server-id="${escapeHTML(id)}" type="button">编辑</button>
              <button class="button tiny secondary" data-action="duplicate" data-server-id="${escapeHTML(id)}" type="button">复制</button>
              <button class="button tiny danger" data-action="delete" data-server-id="${escapeHTML(id)}" type="button">删除</button>
            </div>
          </div>
        </article>`;
    })
    .join("");
}

function renderCalls() {
  const calls = filterCalls(
    state.status?.recentCalls || [],
    $("#callSearch").value,
    $("#callOutcome").value,
  );
  $("#calls").innerHTML = calls
    .map(
      (call) => `
        <tr>
          <td class="call-tool">${escapeHTML(call.tool)}</td>
          <td>${escapeHTML(call.serverId)}</td>
          <td><span class="badge ${call.outcome === "success" ? "success" : "failed"}">${call.outcome === "success" ? "成功" : escapeHTML(call.outcome || "失败")}</span></td>
          <td class="latency">${Number(call.durationMs) || 0} ms</td>
          <td class="call-time">${escapeHTML(formatTimestamp(call.time))}</td>
        </tr>`,
    )
    .join("");
  $("#callsEmpty").classList.toggle("hidden", calls.length > 0);
  $(".table-wrap").classList.toggle("hidden", calls.length === 0);
}

function openEditor(id = null, source = null) {
  state.editingId = id;
  const server = normalizeServer(
    source || (id && state.config?.mcpServers?.[id]) || {},
  );
  $("#editorTitle").textContent = id ? `编辑 ${id}` : "添加 MCP 服务";
  $("#editorSubtitle").textContent = id
    ? "修改后保存会立即热重载。"
    : "填写常用字段即可，高级 JSON 可选。";
  $("#serverId").value = id || "";
  $("#serverId").readOnly = Boolean(id);
  fillForm(server);
  $("#serverJSON").value = JSON.stringify(compactServer(server), null, 2);
  $("#editorError").textContent = "";
  setEditorMode("form");
  $("#editor").showModal();
}

function fillForm(serverInput) {
  const server = normalizeServer(serverInput);
  $("#serverType").value = server.type;
  $("#enabled").checked = server.enabled;
  $("#command").value = server.command;
  $("#args").value = server.args.join("\n");
  $("#cwd").value = server.cwd;
  $("#remoteUrl").value = server.url;
  $("#startupTimeout").value = server.startupTimeout;
  $("#callTimeout").value = server.callTimeout;
  $("#maxConcurrency").value = server.maxConcurrency;
  $("#disabledTools").value = server.tools.disabled.join("\n");
  renderKeyValues("#envRows", server.env);
  renderKeyValues("#headerRows", server.headers);
  syncTransport();
}

function renderKeyValues(selector, entries) {
  const root = $(selector);
  root.replaceChildren();
  for (const [key, value] of Object.entries(entries)) {
    addKeyValue(root, key, value);
  }
  ensureKeyValueEmptyState(root);
}

function ensureKeyValueEmptyState(root) {
  if ($$(".kv-row", root).length) return;
  const empty = document.createElement("div");
  empty.className = "kv-empty";
  empty.textContent = "暂无项目";
  root.replaceChildren(empty);
}

function addKeyValue(root, key = "", value = "") {
  $(".kv-empty", root)?.remove();
  const row = document.createElement("div");
  row.className = "kv-row";

  const keyInput = document.createElement("input");
  keyInput.className = "kv-key";
  keyInput.placeholder = "名称";
  keyInput.value = key;

  const secretWrap = document.createElement("div");
  secretWrap.className = "kv-secret-wrap";
  const valueInput = document.createElement("input");
  valueInput.className = "kv-value";
  const secret = value === SECRET_SENTINEL;
  valueInput.placeholder = secret ? "值（留空保留现有 Secret）" : "值";
  valueInput.value = secret ? "" : value;
  valueInput.dataset.secret = secret ? "1" : "0";
  secretWrap.appendChild(valueInput);

  if (secret) {
    const label = document.createElement("span");
    label.className = "kv-secret-label";
    label.textContent = "SECRET 已设置";
    secretWrap.appendChild(label);
  }

  const remove = document.createElement("button");
  remove.className = "remove-kv";
  remove.type = "button";
  remove.title = "删除";
  remove.setAttribute("aria-label", `删除 ${key || "这一项"}`);
  remove.textContent = "×";
  remove.addEventListener("click", () => {
    row.remove();
    ensureKeyValueEmptyState(root);
  });
  valueInput.addEventListener("input", () => {
    if (!valueInput.value) return;
    valueInput.dataset.secret = "0";
    $(".kv-secret-label", row)?.remove();
  });

  row.append(keyInput, secretWrap, remove);
  root.appendChild(row);
}

function collectKeyValueRows(selector) {
  return collectKeyValues(
    $$(".kv-row", $(selector)).map((row) => ({
      key: $(".kv-key", row).value,
      value: $(".kv-value", row).value,
      preserveSecret: $(".kv-value", row).dataset.secret === "1",
    })),
  );
}

function collectForm() {
  const type = $("#serverType").value;
  const server = { enabled: $("#enabled").checked, type };
  if (type === "stdio") {
    server.command = $("#command").value.trim();
    server.args = nonEmptyLines($("#args").value);
    if ($("#cwd").value.trim()) server.cwd = $("#cwd").value.trim();
    const env = collectKeyValueRows("#envRows");
    if (Object.keys(env).length) server.env = env;
  } else {
    server.url = $("#remoteUrl").value.trim();
    const headers = collectKeyValueRows("#headerRows");
    if (Object.keys(headers).length) server.headers = headers;
  }

  const startupTimeout = $("#startupTimeout").value.trim();
  const callTimeout = $("#callTimeout").value.trim();
  const maxConcurrency = $("#maxConcurrency").value.trim();
  const disabled = nonEmptyLines($("#disabledTools").value);
  if (startupTimeout) server.startupTimeout = startupTimeout;
  if (callTimeout) server.callTimeout = callTimeout;
  if (maxConcurrency) server.maxConcurrency = Number(maxConcurrency);
  if (disabled.length) server.tools = { disabled };
  return server;
}

function compactServer(serverInput) {
  const server = normalizeServer(serverInput);
  const compact = { enabled: server.enabled, type: server.type };
  if (server.type === "stdio") {
    compact.command = server.command;
    if (server.args.length) compact.args = server.args;
    if (server.cwd) compact.cwd = server.cwd;
    if (Object.keys(server.env).length) compact.env = server.env;
  } else {
    compact.url = server.url;
    if (Object.keys(server.headers).length) compact.headers = server.headers;
  }
  if (server.startupTimeout) compact.startupTimeout = server.startupTimeout;
  if (server.callTimeout) compact.callTimeout = server.callTimeout;
  if (server.maxConcurrency)
    compact.maxConcurrency = Number(server.maxConcurrency);
  if (server.tools.disabled.length) compact.tools = server.tools;
  return compact;
}

function validateServer(id, server) {
  if (!/^[a-z][a-z0-9_-]{0,31}$/.test(id)) {
    throw new Error("服务 ID 格式不正确");
  }
  if (!server || typeof server !== "object" || Array.isArray(server)) {
    throw new Error("服务配置必须是 JSON 对象");
  }
  if (server.type === "stdio" && !String(server.command || "").trim()) {
    throw new Error("stdio 服务必须填写启动命令");
  }
  if (server.type === "streamable_http" && !String(server.url || "").trim()) {
    throw new Error("Streamable HTTP 服务必须填写 MCP URL");
  }
}

function syncTransport() {
  const isHTTP = $("#serverType").value === "streamable_http";
  $("#stdioFields").classList.toggle("hidden", isHTTP);
  $("#httpFields").classList.toggle("hidden", !isHTTP);
}

function setEditorMode(mode) {
  if (mode === "json") {
    $("#serverJSON").value = JSON.stringify(collectForm(), null, 2);
  } else if (state.editorMode === "json") {
    try {
      fillForm(JSON.parse($("#serverJSON").value));
    } catch {
      $("#editorError").textContent = "请先修正 JSON 格式";
      return;
    }
  }
  state.editorMode = mode;
  $("#formEditor").classList.toggle("hidden", mode !== "form");
  $("#jsonEditor").classList.toggle("hidden", mode !== "json");
  $("#formTab").classList.toggle("active", mode === "form");
  $("#jsonTab").classList.toggle("active", mode === "json");
  $("#editorError").textContent = "";
}

async function deleteServer(id) {
  if (!window.confirm(`确定删除 ${id}？\n该服务会立即从 Hub 中移除。`)) return;
  try {
    await api(`/servers/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: { "If-Match": state.etag },
      body: "{}",
    });
    toast("服务已删除");
    await refreshAll();
  } catch (error) {
    toast(error.message, "error");
  }
}

$("#loginForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  $("#loginError").textContent = "";
  const button = $("#loginButton");
  button.disabled = true;
  button.textContent = "登录中…";
  try {
    const session = await api("/auth/login", {
      method: "POST",
      body: JSON.stringify({ token: $("#token").value }),
    });
    state.csrf = session.csrfToken;
    $("#token").value = "";
    showApp();
    await refreshAll();
  } catch (error) {
    $("#loginError").textContent = error.message;
  } finally {
    button.disabled = false;
    button.textContent = "进入管理台";
  }
});

$("#togglePassword").addEventListener("click", () => {
  const input = $("#token");
  input.type = input.type === "password" ? "text" : "password";
  $("#togglePassword").textContent =
    input.type === "password" ? "显示" : "隐藏";
});

$("#logout").addEventListener("click", async () => {
  try {
    await api("/auth/logout", { method: "POST", body: "{}" });
  } finally {
    state.csrf = "";
    state.config = null;
    state.status = null;
    showLogin();
  }
});

$("#servers").addEventListener("click", (event) => {
  const button = event.target.closest("[data-action][data-server-id]");
  if (!button) return;
  const id = button.dataset.serverId;
  if (button.dataset.action === "edit") openEditor(id);
  if (button.dataset.action === "duplicate") {
    const source = structuredClone(state.config.mcpServers[id]);
    openEditor(null, source);
    $("#serverId").value = `${id}-copy`.slice(0, 32);
  }
  if (button.dataset.action === "delete") void deleteServer(id);
});

$("#addServer").addEventListener("click", () => openEditor());
$("#addServerHero").addEventListener("click", () => openEditor());
$("#serverType").addEventListener("change", syncTransport);
$("#addEnv").addEventListener("click", () => addKeyValue($("#envRows")));
$("#addHeader").addEventListener("click", () => addKeyValue($("#headerRows")));
$("#formTab").addEventListener("click", () => setEditorMode("form"));
$("#jsonTab").addEventListener("click", () => setEditorMode("json"));
$("#closeEditor").addEventListener("click", () => $("#editor").close());
$("#cancelEditor").addEventListener("click", () => $("#editor").close());

$("#serverForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const save = $("#saveServer");
  $("#editorError").textContent = "";
  try {
    const id = $("#serverId").value.trim();
    const server =
      state.editorMode === "json"
        ? JSON.parse($("#serverJSON").value)
        : collectForm();
    validateServer(id, server);
    if (!state.editingId && state.config?.mcpServers?.[id]) {
      throw new Error("这个服务 ID 已存在；请换一个 ID，或从服务列表进入编辑");
    }
    save.disabled = true;
    save.textContent = "保存中…";
    await api(`/servers/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "If-Match": state.etag },
      body: JSON.stringify(server),
    });
    $("#editor").close();
    toast(state.editingId ? "服务已更新" : "服务已添加");
    await refreshAll();
  } catch (error) {
    $("#editorError").textContent = error.message;
  } finally {
    save.disabled = false;
    save.textContent = "保存并热重载";
  }
});

$("#refresh").addEventListener(
  "click",
  () => void refreshAll({ announce: true }),
);
$("#refreshTop").addEventListener(
  "click",
  () => void refreshAll({ announce: true }),
);
$("#callSearch").addEventListener("input", renderCalls);
$("#callOutcome").addEventListener("change", renderCalls);

for (const button of $$(".nav-item")) {
  button.addEventListener("click", () => {
    for (const item of $$(".nav-item")) item.classList.remove("active");
    button.classList.add("active");
    $(`#${button.dataset.section}`).scrollIntoView({ behavior: "smooth" });
  });
}

for (const button of $$(".copy-button")) {
  button.addEventListener("click", async () => {
    const text = $(`#${button.dataset.copyTarget}`).textContent;
    try {
      await navigator.clipboard.writeText(text);
      toast("已复制到剪贴板");
    } catch {
      toast("浏览器未允许复制，请手动复制", "error");
    }
  });
}

window.setInterval(() => {
  if (!document.hidden && !$("#editor").open) void refreshStatus();
}, 10_000);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) void refreshStatus();
});

void boot();
