"use strict";

const PAGE_SIZE_DEFAULT = 50;

const state = {
  tab: "dashboard",
  project: "",
  access:   { search: "", limit: PAGE_SIZE_DEFAULT, offset: 0, lastCount: 0 },
  waitlist: { search: "", limit: PAGE_SIZE_DEFAULT, offset: 0, lastCount: 0 },
};

let modalCallback = null;

document.addEventListener("DOMContentLoaded", init);

async function init() {
  document.querySelectorAll("#tabs .tab").forEach(btn => {
    btn.addEventListener("click", () => setTab(btn.dataset.tab, true));
  });

  document.getElementById("dashboard-days").addEventListener("change", loadDashboard);
  document.getElementById("dashboard-refresh").addEventListener("click", loadDashboard);

  bindList("access",   loadAccess);
  bindList("waitlist", loadWaitlist);

  document.getElementById("modal-cancel").addEventListener("click", closeModal);
  document.getElementById("modal-confirm").addEventListener("click", submitModal);
  document.getElementById("modal-reason").addEventListener("input", e => {
    document.getElementById("modal-reason-count").textContent = e.target.value.length;
  });

  await loadProjectFilter();

  // Pick up the active tab from the URL hash, if any.
  const initialTab = (location.hash.match(/tab=([\w-]+)/) || [])[1] || "dashboard";
  setTab(initialTab, false);
}

async function loadProjectFilter() {
  const select = document.getElementById("project-filter");
  if (!select) return;
  try {
    const projects = await apiFetch("GET", "/admin/projects");
    select.innerHTML = '<option value="">All projects</option>';
    for (const p of projects) {
      const opt = document.createElement("option");
      opt.value = p.slug;
      opt.textContent = `${p.name} (${p.slug})`;
      select.appendChild(opt);
    }
    select.addEventListener("change", () => {
      state.project = select.value;
      refreshCurrentTab();
    });
  } catch (_) { /* project filter is optional */ }
}

function projectParam() {
  return state.project ? `&project=${encodeURIComponent(state.project)}` : "";
}

function refreshCurrentTab() {
  if      (state.tab === "dashboard") loadDashboard();
  else if (state.tab === "access")    loadAccess();
  else if (state.tab === "waitlist")  loadWaitlist();
}

function bindList(name, loader) {
  const search = document.getElementById(`${name}-search`);
  const limit  = document.getElementById(`${name}-limit`);
  const prev   = document.getElementById(`${name}-prev`);
  const next   = document.getElementById(`${name}-next`);

  const debounced = debounce(() => {
    state[name].search = search.value.trim();
    state[name].offset = 0;
    loader();
  }, 250);

  search.addEventListener("input", debounced);

  limit.addEventListener("change", () => {
    state[name].limit = parseInt(limit.value, 10);
    state[name].offset = 0;
    loader();
  });

  prev.addEventListener("click", () => {
    state[name].offset = Math.max(0, state[name].offset - state[name].limit);
    loader();
  });

  next.addEventListener("click", () => {
    state[name].offset += state[name].limit;
    loader();
  });
}

function setTab(name, updateHash) {
  if (!["dashboard", "access", "waitlist"].includes(name)) name = "dashboard";
  state.tab = name;

  document.querySelectorAll(".tab").forEach(b => b.classList.toggle("active", b.dataset.tab === name));
  document.querySelectorAll(".tab-panel").forEach(p => p.hidden = (p.id !== `tab-${name}`));

  if (updateHash) location.hash = `tab=${name}`;

  if      (name === "dashboard") loadDashboard();
  else if (name === "access")    loadAccess();
  else if (name === "waitlist")  loadWaitlist();
}

// --- Dashboard ---------------------------------------------------------------

async function loadDashboard() {
  const days = document.getElementById("dashboard-days").value;
  try {
    const data = await apiFetch("GET", `/admin/dashboard?days=${encodeURIComponent(days)}${projectParam()}`);
    document.getElementById("count-waitlist").textContent = data.waiting_list;
    document.getElementById("count-access").textContent   = data.with_access;
    document.getElementById("count-total").textContent    = data.total;
    renderChart(document.getElementById("chart"), data.enlistments_by_day || []);
  } catch (err) {
    showError(`Dashboard failed: ${err.message}`);
  }
}

function renderChart(svg, data) {
  while (svg.firstChild) svg.removeChild(svg.firstChild);
  if (data.length === 0) return;

  const W = 1100, H = 220, P = 28;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  svg.setAttribute("preserveAspectRatio", "none");

  const max = Math.max(1, ...data.map(d => d.count));
  const bw  = (W - 2 * P) / data.length;

  const ns = "http://www.w3.org/2000/svg";

  // Axes.
  const yAxis = document.createElementNS(ns, "line");
  yAxis.setAttribute("class", "axis");
  yAxis.setAttribute("x1", P); yAxis.setAttribute("x2", P);
  yAxis.setAttribute("y1", P / 2); yAxis.setAttribute("y2", H - P);
  svg.appendChild(yAxis);

  const xAxis = document.createElementNS(ns, "line");
  xAxis.setAttribute("class", "axis");
  xAxis.setAttribute("x1", P); xAxis.setAttribute("x2", W - P / 2);
  xAxis.setAttribute("y1", H - P); xAxis.setAttribute("y2", H - P);
  svg.appendChild(xAxis);

  // Y axis max label.
  const yMax = document.createElementNS(ns, "text");
  yMax.setAttribute("class", "axis-label");
  yMax.setAttribute("x", 4); yMax.setAttribute("y", P / 2 + 4);
  yMax.textContent = String(max);
  svg.appendChild(yMax);

  const yZero = document.createElementNS(ns, "text");
  yZero.setAttribute("class", "axis-label");
  yZero.setAttribute("x", 12); yZero.setAttribute("y", H - P + 4);
  yZero.textContent = "0";
  svg.appendChild(yZero);

  // Bars.
  data.forEach((d, i) => {
    const h = ((H - P - P / 2) * d.count) / max;
    const x = P + i * bw;
    const y = H - P - h;
    const rect = document.createElementNS(ns, "rect");
    rect.setAttribute("class", "bar");
    rect.setAttribute("x", x);
    rect.setAttribute("y", y);
    rect.setAttribute("width", Math.max(1, bw - 1));
    rect.setAttribute("height", h);
    const t = document.createElementNS(ns, "title");
    t.textContent = `${d.day}: ${d.count}`;
    rect.appendChild(t);
    svg.appendChild(rect);
  });

  // X axis labels (first, middle, last).
  const tickIdx = [0, Math.floor(data.length / 2), data.length - 1];
  tickIdx.forEach(i => {
    if (i < 0 || i >= data.length) return;
    const lbl = document.createElementNS(ns, "text");
    lbl.setAttribute("class", "axis-label");
    lbl.setAttribute("x", P + i * bw + bw / 2);
    lbl.setAttribute("y", H - P + 14);
    lbl.setAttribute("text-anchor", "middle");
    lbl.textContent = data[i].day.slice(5); // MM-DD
    svg.appendChild(lbl);
  });
}

// --- Users with access -------------------------------------------------------

async function loadAccess() {
  const s = state.access;
  const url = `/admin/users/access?email=${encodeURIComponent(s.search)}&limit=${s.limit}&offset=${s.offset}${projectParam()}`;
  try {
    const data = await apiFetch("GET", url);
    s.lastCount = data.users.length;
    renderAccessRows(data.users);
    updatePager("access");
  } catch (err) {
    showError(`Access list failed: ${err.message}`);
  }
}

function renderAccessRows(users) {
  const body = document.getElementById("access-body");
  const empty = document.getElementById("access-empty");
  body.replaceChildren();

  if (users.length === 0) {
    empty.classList.remove("hidden");
    return;
  }
  empty.classList.add("hidden");

  for (const u of users) {
    const tr = document.createElement("tr");
    tr.appendChild(td(u.email));
    tr.appendChild(td(`${u.firstname} ${u.lastname}`));
    tr.appendChild(td(formatTs(u.created_at)));
    tr.appendChild(td(formatTs(u.access_granted_at)));
    tr.appendChild(td(badge(u.access_granted_by || "—", "badge-source")));

    const status = document.createElement("td");
    if (u.access_revoked_at) {
      status.appendChild(badge("Revoked", "badge-revoked"));
      const reason = document.createElement("div");
      reason.className = "subtle";
      reason.textContent = u.access_revoke_reason || "";
      status.appendChild(reason);
    } else {
      status.appendChild(badge("Active", "badge-ok"));
    }
    tr.appendChild(status);

    const actions = document.createElement("td");
    actions.className = "actions";
    if (!u.access_revoked_at) {
      const btn = document.createElement("button");
      btn.className = "btn-danger";
      btn.textContent = "Revoke";
      btn.addEventListener("click", () => promptRevoke(u));
      actions.appendChild(btn);
    } else {
      const span = document.createElement("span");
      span.className = "subtle";
      span.textContent = "—";
      actions.appendChild(span);
    }
    tr.appendChild(actions);

    body.appendChild(tr);
  }
}

// --- Waiting list ------------------------------------------------------------

async function loadWaitlist() {
  const s = state.waitlist;
  const url = `/admin/users/waitlist?email=${encodeURIComponent(s.search)}&limit=${s.limit}&offset=${s.offset}${projectParam()}`;
  try {
    const data = await apiFetch("GET", url);
    s.lastCount = data.entries.length;
    renderWaitlistRows(data.entries);
    updatePager("waitlist");
  } catch (err) {
    showError(`Waiting list failed: ${err.message}`);
  }
}

function renderWaitlistRows(entries) {
  const body = document.getElementById("waitlist-body");
  const empty = document.getElementById("waitlist-empty");
  body.replaceChildren();

  if (entries.length === 0) {
    empty.classList.remove("hidden");
    return;
  }
  empty.classList.add("hidden");

  for (const e of entries) {
    const tr = document.createElement("tr");
    tr.appendChild(td(e.email));
    tr.appendChild(td(`${e.firstname} ${e.lastname}`));
    tr.appendChild(td(String(e.weight)));
    tr.appendChild(td(formatTs(e.created_at)));
    tr.appendChild(td(formatTs(e.weighted_created_at)));

    const actions = document.createElement("td");
    actions.className = "actions";

    const grantBtn = document.createElement("button");
    grantBtn.className = "btn-primary";
    grantBtn.textContent = "Grant access";
    grantBtn.addEventListener("click", () => promptGrant(e));
    actions.appendChild(grantBtn);

    const delBtn = document.createElement("button");
    delBtn.className = "btn-danger";
    delBtn.textContent = "Remove";
    delBtn.addEventListener("click", () => promptDelete(e));
    actions.appendChild(delBtn);

    tr.appendChild(actions);
    body.appendChild(tr);
  }
}

// --- Pager -------------------------------------------------------------------

function updatePager(name) {
  const s = state[name];
  document.getElementById(`${name}-prev`).disabled = (s.offset === 0);
  document.getElementById(`${name}-next`).disabled = (s.lastCount < s.limit);
  document.getElementById(`${name}-page-label`).textContent = `offset ${s.offset}`;
}

// --- Modal & actions ---------------------------------------------------------

function promptRevoke(user) {
  openModal({
    title:        `Revoke access for ${user.email}?`,
    body:         "The user will lose access immediately. Provide a reason — it will be visible to the user.",
    requireReason: true,
    onConfirm:    async (reason) => {
      await apiFetch("POST", `/admin/users/${encodeURIComponent(user.id)}/revoke-access`, { reason });
      closeModal();
      loadAccess();
    },
  });
}

function promptGrant(entry) {
  openModal({
    title:        `Grant access to ${entry.email}?`,
    body:         "The user will be removed from the waiting list and gain access immediately.",
    requireReason: false,
    onConfirm:    async () => {
      await apiFetch("POST", `/admin/users/${encodeURIComponent(entry.user_id)}/grant-access`, {});
      closeModal();
      loadWaitlist();
    },
  });
}

function promptDelete(entry) {
  openModal({
    title:        `Remove ${entry.email} from the waiting list?`,
    body:         "The waiting list entry will be deleted. The user record itself is kept.",
    requireReason: false,
    onConfirm:    async () => {
      await apiFetch("DELETE", `/admin/waitlist/${encodeURIComponent(entry.entry_id)}`);
      closeModal();
      loadWaitlist();
    },
  });
}

function openModal({ title, body, requireReason, onConfirm }) {
  document.getElementById("modal-title").textContent = title;
  document.getElementById("modal-body").textContent  = body;
  const reasonRow = document.getElementById("modal-reason-row");
  reasonRow.classList.toggle("hidden", !requireReason);
  if (requireReason) {
    const t = document.getElementById("modal-reason");
    t.value = "";
    document.getElementById("modal-reason-count").textContent = "0";
    setTimeout(() => t.focus(), 0);
  }
  document.getElementById("modal-error").classList.add("hidden");
  document.getElementById("modal").classList.remove("hidden");
  modalCallback = { requireReason, onConfirm };
}

function closeModal() {
  document.getElementById("modal").classList.add("hidden");
  modalCallback = null;
}

async function submitModal() {
  if (!modalCallback) return;
  const errEl = document.getElementById("modal-error");
  errEl.classList.add("hidden");

  let reason = "";
  if (modalCallback.requireReason) {
    reason = document.getElementById("modal-reason").value.trim();
    if (reason === "") {
      errEl.textContent = "Reason is required.";
      errEl.classList.remove("hidden");
      return;
    }
  }
  try {
    await modalCallback.onConfirm(reason);
  } catch (err) {
    errEl.textContent = err.message || "Action failed.";
    errEl.classList.remove("hidden");
  }
}

// --- Helpers -----------------------------------------------------------------

async function apiFetch(method, url, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(url, opts);
  if (res.status === 204) return null;
  let data = null;
  const ct = res.headers.get("Content-Type") || "";
  if (ct.includes("application/json")) {
    try { data = await res.json(); } catch (_) { /* tolerate empty bodies */ }
  }
  if (!res.ok) {
    const msg = (data && (data.error || data.message)) || `${res.status} ${res.statusText}`;
    throw new Error(msg);
  }
  return data;
}

function showError(msg) {
  const banner = document.getElementById("banner");
  banner.textContent = msg;
  banner.classList.remove("hidden");
  setTimeout(() => banner.classList.add("hidden"), 5000);
}

function td(content) {
  const cell = document.createElement("td");
  if (content instanceof Node) {
    cell.appendChild(content);
  } else {
    cell.textContent = content;
  }
  return cell;
}

function badge(text, cls) {
  const span = document.createElement("span");
  span.className = `badge ${cls}`;
  span.textContent = text;
  return span;
}

function formatTs(value) {
  if (!value) return "—";
  const d = new Date(value);
  if (isNaN(d.getTime())) return value;
  return d.toLocaleString();
}

function debounce(fn, ms) {
  let timer = null;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}
