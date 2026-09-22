"use strict";

// --- token + api helpers ---
const TOKEN_KEY = "jev_admin_token";
let token = localStorage.getItem(TOKEN_KEY) || "";

const $ = (id) => document.getElementById(id);

// Auth endpoints must NOT trigger auto-logout on 401 (a 401 there means "wrong
// password" / "not logged in", not "session expired") — otherwise logout loops.
const AUTH_PATHS = ["/api/login", "/api/setup", "/api/setup-status", "/api/logout"];

async function api(path, opts = {}) {
  const headers = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
  if (token) headers["Authorization"] = "Bearer " + token;
  const res = await fetch(path, Object.assign({}, opts, { headers }));
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (res.status === 401 && !AUTH_PATHS.includes(path)) {
    logout();
    throw new Error(data.error || "unauthorized");
  }
  if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
  return data;
}

// --- auth flow ---
let setupMode = false;

async function initAuth() {
  const s = await api("/api/setup-status");
  setupMode = s.needs_setup;
  $("auth-sub").textContent = setupMode
    ? "首次使用，请设置管理员口令（至少 6 位）"
    : "请输入管理员口令登录";
  $("pw2").classList.toggle("hidden", !setupMode);
  $("auth-btn").textContent = setupMode ? "设置并进入" : "登录";
}

async function doAuth() {
  const pw = $("pw").value;
  $("auth-err").textContent = "";
  try {
    if (setupMode) {
      if (pw.length < 6) throw new Error("口令至少 6 位");
      if (pw !== $("pw2").value) throw new Error("两次输入不一致");
      const r = await api("/api/setup", { method: "POST", body: JSON.stringify({ password: pw }) });
      token = r.token;
    } else {
      const r = await api("/api/login", { method: "POST", body: JSON.stringify({ password: pw }) });
      token = r.token;
    }
    localStorage.setItem(TOKEN_KEY, token);
    enterApp();
  } catch (e) {
    $("auth-err").textContent = e.message;
  }
}

let loggingOut = false;
function logout() {
  if (loggingOut) return; // guard against re-entrancy
  loggingOut = true;
  const t = token;
  token = "";
  localStorage.removeItem(TOKEN_KEY);
  // Fire-and-forget with a plain fetch so a 401 here can't re-enter api()/logout().
  if (t) {
    fetch("/api/logout", {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: "Bearer " + t },
    }).catch(() => {});
  }
  $("app").classList.add("hidden");
  $("auth").classList.remove("hidden");
  $("pw").value = "";
  $("pw2").value = "";
  loggingOut = false;
}

async function enterApp() {
  $("auth").classList.add("hidden");
  $("app").classList.remove("hidden");
  await refreshAll();
}

// --- state loading ---
async function refreshAll() {
  const st = await api("/api/state");
  renderSettings(st.settings);
  renderStats(st.stats24h);
  renderKeys(st.keys);
  updateMasterPill(st.settings.enabled);
  await loadLogs();
}

function updateMasterPill(enabled) {
  const p = $("master-state");
  p.textContent = enabled ? "过滤已启用" : "过滤已关闭";
  p.className = "pill " + (enabled ? "on" : "off");
}

// --- settings ---
function renderSettings(s) {
  $("s_enabled").checked = s.enabled;
  $("s_upstream").value = s.upstream_base_url || "";
  $("s_jevbase").value = s.jev_base_url || "";
  $("s_model").value = s.jev_model || "";
  $("s_instruction").value = s.safety_instruction || "";
  $("s_threshold").value = s.safety_threshold;
  $("s_maxchars").value = s.max_state_chars;
  $("s_timeout").value = s.jev_timeout_ms;
  $("s_blockbelow").checked = s.block_if_below;
  $("s_failopen").checked = s.fail_open;
  $("s_checkresp").checked = s.check_response;
  $("s_abuse").checked = s.abuse_enabled;
  $("s_abuse_win").value = s.abuse_window_sec;
  $("s_abuse_max").value = s.abuse_max_harmful;
  $("s_abuse_ban").value = s.abuse_ban_sec;
  $("s_blockmsg").value = s.block_message || "";
}

async function saveSettings(e) {
  e.preventDefault();
  $("settings-msg").textContent = "";
  const body = {
    enabled: $("s_enabled").checked,
    upstream_base_url: $("s_upstream").value.trim(),
    jev_base_url: $("s_jevbase").value.trim(),
    jev_model: $("s_model").value.trim(),
    safety_instruction: $("s_instruction").value,
    safety_threshold: parseFloat($("s_threshold").value),
    max_state_chars: parseInt($("s_maxchars").value, 10),
    jev_timeout_ms: parseInt($("s_timeout").value, 10),
    block_if_below: $("s_blockbelow").checked,
    fail_open: $("s_failopen").checked,
    check_response: $("s_checkresp").checked,
    abuse_enabled: $("s_abuse").checked,
    abuse_window_sec: parseInt($("s_abuse_win").value, 10),
    abuse_max_harmful: parseInt($("s_abuse_max").value, 10),
    abuse_ban_sec: parseInt($("s_abuse_ban").value, 10),
    block_message: $("s_blockmsg").value,
  };
  try {
    const s = await api("/api/settings", { method: "PUT", body: JSON.stringify(body) });
    renderSettings(s);
    updateMasterPill(s.enabled);
    $("settings-msg").textContent = "已保存 ✓";
    setTimeout(() => ($("settings-msg").textContent = ""), 2500);
  } catch (err) {
    $("settings-msg").textContent = "";
    alert("保存失败: " + err.message);
  }
}

// --- stats ---
function renderStats(s) {
  const items = [
    { l: "24h 总请求", n: s.total || 0, c: "" },
    { l: "放行", n: s.allowed || 0, c: "allow" },
    { l: "拦截", n: s.blocked || 0, c: "block" },
    { l: "跳过", n: s.skipped || 0, c: "" },
    { l: "错误", n: s.errors || 0, c: "error" },
  ];
  $("stats").innerHTML = items
    .map((i) => `<div class="stat ${i.c}"><div class="n">${i.n}</div><div class="l">${i.l}</div></div>`)
    .join("");
}

// --- keys ---
function renderKeys(keys) {
  const tb = $("keys-table").querySelector("tbody");
  if (!keys || !keys.length) {
    tb.innerHTML = `<tr><td colspan="6" class="muted">尚未添加任何 JEV 密钥</td></tr>`;
    return;
  }
  tb.innerHTML = keys
    .map((k) => {
      const on = k.enabled;
      return `<tr>
        <td>${k.id}</td>
        <td>${escapeHtml(k.label) || "-"}</td>
        <td class="mono">${escapeHtml(k.masked)}</td>
        <td>${k.calls}</td>
        <td><span class="pill ${on ? "on" : "off"}">${on ? "启用" : "停用"}</span></td>
        <td>
          <button class="ghost" data-toggle="${k.id}" data-on="${on}">${on ? "停用" : "启用"}</button>
          <button class="danger" data-del="${k.id}">删除</button>
        </td>
      </tr>`;
    })
    .join("");
  tb.querySelectorAll("[data-toggle]").forEach((b) =>
    b.addEventListener("click", () => toggleKey(b.dataset.toggle, b.dataset.on === "true"))
  );
  tb.querySelectorAll("[data-del]").forEach((b) =>
    b.addEventListener("click", () => delKey(b.dataset.del))
  );
}

async function addKey() {
  $("key-msg").textContent = "";
  const label = $("k_label").value.trim();
  const key = $("k_value").value.trim();
  if (!key) {
    $("key-msg").textContent = "请填写密钥";
    return;
  }
  try {
    await api("/api/keys", { method: "POST", body: JSON.stringify({ label, key }) });
    $("k_label").value = "";
    $("k_value").value = "";
    renderKeys(await api("/api/keys"));
  } catch (e) {
    $("key-msg").textContent = e.message;
  }
}

async function toggleKey(id, on) {
  const action = on ? "disable" : "enable";
  await api(`/api/keys/${id}/${action}`, { method: "POST" });
  renderKeys(await api("/api/keys"));
}

async function delKey(id) {
  if (!confirm("确认删除该密钥？")) return;
  await api(`/api/keys/${id}`, { method: "DELETE" });
  renderKeys(await api("/api/keys"));
}

// --- logs ---
async function loadLogs() {
  const filter = $("log-filter").value;
  const q = filter ? `?decision=${filter}&limit=200` : "?limit=200";
  const logs = await api("/api/logs" + q);
  const tb = $("logs-table").querySelector("tbody");
  if (!logs || !logs.length) {
    tb.innerHTML = `<tr><td colspan="11" class="muted">暂无记录</td></tr>`;
    return;
  }
  tb.innerHTML = logs
    .map((l) => {
      const t = new Date(l.ts).toLocaleString();
      const score = l.score == null ? "-" : l.score.toFixed(3);
      return `<tr>
        <td class="mono">${t}</td>
        <td>${l.method}</td>
        <td class="mono">${escapeHtml(l.path)}</td>
        <td>${escapeHtml(l.kind)}</td>
        <td class="mono">${escapeHtml(l.model) || "-"}</td>
        <td><span class="tag ${l.decision}">${decisionLabel(l.decision)}</span></td>
        <td>${score}</td>
        <td>${l.latency_ms}ms</td>
        <td class="mono">${escapeHtml(l.ip)}</td>
        <td class="muted">${escapeHtml(l.reason)}</td>
        <td class="muted snippet" title="${escapeHtml(l.snippet)}">${escapeHtml(l.snippet)}</td>
      </tr>`;
    })
    .join("");
}

function decisionLabel(d) {
  return { allow: "放行", block: "拦截", skip: "跳过", error: "错误" }[d] || d;
}

// --- utils ---
function escapeHtml(s) {
  if (s == null) return "";
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}

// --- wire up ---
$("auth-btn").addEventListener("click", doAuth);
$("pw").addEventListener("keydown", (e) => e.key === "Enter" && (setupMode ? $("pw2").focus() : doAuth()));
$("pw2").addEventListener("keydown", (e) => e.key === "Enter" && doAuth());
$("logout").addEventListener("click", logout);
$("settings-form").addEventListener("submit", saveSettings);
$("k_add").addEventListener("click", addKey);
$("log-refresh").addEventListener("click", loadLogs);
$("log-filter").addEventListener("change", loadLogs);

// --- boot ---
(async function boot() {
  try {
    await initAuth();
    if (token && !setupMode) {
      // Validate token by loading state.
      try {
        await enterApp();
        return;
      } catch (_) {
        /* fall through to auth screen */
      }
    }
    $("auth").classList.remove("hidden");
  } catch (e) {
    $("auth").classList.remove("hidden");
    $("auth-err").textContent = "无法连接服务: " + e.message;
  }
})();
