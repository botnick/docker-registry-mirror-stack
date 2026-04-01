const root = document.getElementById("page-root");
const flash = document.getElementById("flash-message");
const pageId = document.body.dataset.page;
const csrfToken = document.body.dataset.csrf || "";
const navToggle = document.getElementById("nav-toggle");
const navClose = document.getElementById("nav-close");
const navOverlay = document.getElementById("nav-overlay");
let liveTimer = null;

document.querySelectorAll("[data-nav]").forEach((link) => {
  if (link.dataset.nav === pageId) link.classList.add("is-active");
});

function setNavOpen(open) {
  document.body.classList.toggle("nav-open", open);
  navToggle?.setAttribute("aria-expanded", open ? "true" : "false");
}

navToggle?.addEventListener("click", () => setNavOpen(!document.body.classList.contains("nav-open")));
navClose?.addEventListener("click", () => setNavOpen(false));
navOverlay?.addEventListener("click", () => setNavOpen(false));
document.querySelectorAll(".nav a").forEach((link) => {
  link.addEventListener("click", () => {
    if (window.innerWidth <= 1080) setNavOpen(false);
  });
});

window.addEventListener("keydown", (event) => {
  if (event.key === "Escape") setNavOpen(false);
});

window.addEventListener("resize", () => {
  if (window.innerWidth > 1080) setNavOpen(false);
});

document.getElementById("refresh-page")?.addEventListener("click", () => window.__pageReload?.());
document.getElementById("logout-button")?.addEventListener("click", async () => {
  await fetch("/auth/logout", {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
    credentials: "same-origin",
  });
  window.location.href = "/login";
});

window.addEventListener("popstate", () => {
  setNavOpen(false);
  window.__pageReload?.();
});

function stopLiveTimer() {
  if (liveTimer) clearInterval(liveTimer);
  liveTimer = null;
}

function startLiveTimer(callback, interval = 4000) {
  stopLiveTimer();
  liveTimer = setInterval(() => {
    callback().catch((error) => console.error(error));
  }, interval);
}

function showFlash(message, kind = "success") {
  flash.textContent = message;
  flash.className = `flash is-${kind}`;
  flash.classList.remove("hidden");
}

function clearFlash() {
  flash.textContent = "";
  flash.className = "flash hidden";
}

function enhanceTables(container = document) {
  container.querySelectorAll(".table-wrap table").forEach((table) => {
    table.classList.add("responsive-table");
    const headers = [...table.querySelectorAll("thead th")].map((th) => th.textContent.trim());
    table.querySelectorAll("tbody tr").forEach((row) => {
      [...row.children].forEach((cell, index) => {
        if (cell.tagName !== "TD" || cell.hasAttribute("colspan")) return;
        cell.dataset.label = headers[index] || "";
      });
    });
  });
}

async function api(url, options = {}) {
  const method = (options.method || "GET").toUpperCase();
  const headers = { Accept: "application/json", ...(options.headers || {}) };
  if (!["GET", "HEAD"].includes(method)) {
    headers["X-CSRF-Token"] = csrfToken;
    if (!headers["Content-Type"] && !(options.body instanceof FormData)) headers["Content-Type"] = "application/json";
  }

  const response = await fetch(url, { credentials: "same-origin", ...options, method, headers });
  const data = await response.json().catch(() => ({}));
  if (response.status === 401) {
    window.location.href = "/login";
    throw new Error("ต้องเข้าสู่ระบบใหม่");
  }
  if (response.status === 403 && data.must_change_password) {
    window.location.href = "/force-password";
    throw new Error("ต้องเปลี่ยนรหัสผ่านก่อน");
  }
  if (!response.ok) throw new Error(data.error || "คำขอไม่สำเร็จ");
  return data;
}

const postJSON = (url, payload) => api(url, { method: "POST", body: JSON.stringify(payload || {}) });
const esc = (value) => String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#039;");
const fmtTime = (value) => value ? new Intl.DateTimeFormat("th-TH", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value)) : "-";
const fmtBool = (value) => value ? "เปิด" : "ปิด";

function fmtBytes(value) {
  let n = Number(value || 0);
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

const jsonBlock = (value) => `<pre class="json-block">${esc(JSON.stringify(value || {}, null, 2))}</pre>`;
const pill = (text, kind = "") => `<span class="pill ${kind}">${esc(text)}</span>`;
const panel = (title, subtitle, body) => `<section class="panel"><div class="panel-head"><div><h2>${esc(title)}</h2>${subtitle ? `<p>${esc(subtitle)}</p>` : ""}</div></div>${body}</section>`;
const defs = (items) => `<div class="definition-list">${items.map((item) => `<div class="definition-item"><span>${esc(item.label)}</span><strong class="${item.mono ? "mono" : ""}">${esc(item.value)}</strong></div>`).join("")}</div>`;

function currentQuery() {
  return new URLSearchParams(window.location.search);
}

function setQuery(next, replace = false) {
  const params = currentQuery();
  Object.entries(next).forEach(([key, value]) => {
    if (value === "" || value === null || value === undefined) params.delete(key);
    else params.set(key, String(value));
  });
  const search = params.toString();
  const url = `${window.location.pathname}${search ? `?${search}` : ""}`;
  history[replace ? "replaceState" : "pushState"]({}, "", url);
  window.__pageReload?.();
}

function statusTone(state) {
  if (!state) return "";
  if (String(state).includes("degraded") || String(state).includes("pressure")) return "warn";
  if (String(state).includes("emergency")) return "danger";
  if (String(state).includes("maintenance")) return "warn";
  return "success";
}

function badgeSet(item) {
  const badges = [];
  if (item.pinned) badges.push(pill("pinned", "success"));
  if (item.explicit_protected) badges.push(pill("explicit protect", "warn"));
  if (item.regex_protected) badges.push(pill("regex protect", "warn"));
  if (item.deleted_at) badges.push(pill("deleted", "danger"));
  if (item.candidate) badges.push(pill("candidate", "success"));
  return badges.join(" ");
}

function pager(data, key = "offset") {
  const from = data.total === 0 ? 0 : data.offset + 1;
  const to = Math.min(data.offset + data.limit, data.total);
  return `<div class="pagination"><span>แสดง ${from}-${to} จาก ${data.total} รายการ</span><div class="inline-actions"><button class="ghost-button" type="button" data-page-prev="${key}" ${data.offset <= 0 ? "disabled" : ""}>ก่อนหน้า</button><button class="ghost-button" type="button" data-page-next="${key}" ${!data.has_more ? "disabled" : ""}>ถัดไป</button></div></div>`;
}

function bindPager(offset, limit, key = "offset") {
  root.querySelector(`[data-page-prev="${key}"]`)?.addEventListener("click", () => setQuery({ [key]: Math.max(offset - limit, 0) }));
  root.querySelector(`[data-page-next="${key}"]`)?.addEventListener("click", () => setQuery({ [key]: offset + limit }));
}

async function quickCleanup(dryRun, force = false) {
  const result = await postJSON("/api/cleanup/run", { dry_run: dryRun, force });
  showFlash(result.skipped ? (result.skip_reason || "งานถูกข้าม") : (dryRun ? "จำลอง cleanup สำเร็จ" : "รัน cleanup สำเร็จ"), result.skipped ? "warn" : "success");
  return result;
}

async function quickGC(force = true) {
  const result = await postJSON("/api/gc/run", { force });
  showFlash(result.skipped ? (result.skip_reason || "งานถูกข้าม") : "เริ่มงาน GC แล้ว", result.skipped ? "warn" : "success");
  return result;
}

function maintenanceButtons(forceJanitor = true) {
  return `<div class="toolbar"><button class="primary-button" id="cleanup-dry" type="button">ดูผล Cleanup แบบ Dry Run</button><button class="ghost-button" id="cleanup-live" type="button">รัน Cleanup จริง</button>${forceJanitor ? '<button class="ghost-button" id="cleanup-force" type="button">บังคับ Cleanup</button>' : ""}<button class="danger-button" id="gc-now" type="button">บังคับ GC ตอนนี้</button></div>`;
}

function bindMaintenanceButtons(refreshFn, forceJanitor = true) {
  root.querySelector("#cleanup-dry")?.addEventListener("click", async () => { await quickCleanup(true); await refreshFn?.(); });
  root.querySelector("#cleanup-live")?.addEventListener("click", async () => {
    if (!window.confirm("ยืนยันรัน cleanup จริง?")) return;
    await quickCleanup(false);
    await refreshFn?.();
  });
  if (forceJanitor) {
    root.querySelector("#cleanup-force")?.addEventListener("click", async () => {
      if (!window.confirm("ยืนยันบังคับ cleanup แม้ระบบ degraded?")) return;
      await quickCleanup(false, true);
      await refreshFn?.();
    });
  }
  root.querySelector("#gc-now")?.addEventListener("click", async () => {
    if (!window.confirm("ยืนยันบังคับ GC ตอนนี้? ระหว่างรันอาจมีช่วงหยุดบริการสั้น ๆ")) return;
    await quickGC(true);
    await refreshFn?.();
  });
}

function renderLogRows(items) {
  return (items || []).map((item) => `
    <tr data-log-id="${item.id}">
      <td class="mono">${item.id}</td>
      <td>${fmtTime(item.created_at)}</td>
      <td>${esc(item.level)}</td>
      <td>${esc(item.scope)}</td>
      <td>${esc(item.actor || "-")}</td>
      <td>${esc(item.message)}</td>
      <td><details><summary>details</summary>${jsonBlock(item.details)}</details></td>
    </tr>`).join("");
}

function renderJobRows(items) {
  return (items || []).map((item) => `
    <tr>
      <td class="mono">${item.id}</td>
      <td>${esc(item.job_type)}</td>
      <td>${esc(item.trigger_source)}</td>
      <td>${pill(item.status, item.status === "success" ? "success" : (item.status === "error" ? "danger" : "warn"))}</td>
      <td>${fmtTime(item.started_at)}</td>
      <td>${fmtTime(item.finished_at)}</td>
      <td><details><summary>details</summary>${jsonBlock(item.details)}</details></td>
    </tr>`).join("");
}

async function renderDashboard() {
  const [me, overview] = await Promise.all([api("/api/auth/me"), api("/api/system/overview")]);
  const fallback = overview.fallback || {};
  const storage = overview.storage || {};
  root.innerHTML = `
    <section class="hero-grid">
      <article class="panel">
        <div class="status-banner">
          <div>
            <strong>${esc(fallback.summary || "-")}</strong>
            <p class="muted">${esc(fallback.details || "-")}</p>
          </div>
          <div class="badge-row">
            ${pill(`state: ${fallback.state || "-"}`, statusTone(fallback.state))}
            ${pill(`cached mode: ${fallback.cached_mode_usable ? "ใช้ได้" : "ใช้ไม่ได้"}`, fallback.cached_mode_usable ? "success" : "danger")}
            ${pill(`destructive: ${fallback.destructive_paused ? "paused" : "ready"}`, fallback.destructive_paused ? "warn" : "success")}
          </div>
        </div>
        <h2 class="kpi-number">${fmtBytes(storage.used_bytes || 0)}</h2>
        <p class="support-copy">ใช้ไป ${fmtBytes(storage.used_bytes || 0)} จากทั้งหมด ${fmtBytes(storage.total_bytes || 0)} เหลือว่าง ${fmtBytes(storage.free_bytes || 0)}</p>
        <div class="progress-bar"><span style="width:${Math.min(Number(storage.used_pct || 0), 100)}%"></span></div>
      </article>
      <article class="panel">
        <div class="panel-head"><div><h2>คำสั่งด่วน</h2><p>สั่ง cleanup และ GC จากหน้าหลักได้ทันที</p></div></div>
        ${maintenanceButtons(false)}
        <div class="metric-grid">
          <div class="metric-card"><span>ผู้ใช้</span><strong>${esc(me.username || "-")}</strong></div>
          <div class="metric-card"><span>Candidates</span><strong>${overview.counts?.eligible_candidates || 0}</strong></div>
          <div class="metric-card"><span>GC Pending</span><strong>${overview.gc_pending ? "มี" : "ไม่มี"}</strong></div>
        </div>
      </article>
    </section>
    <section class="three-column">
      <article class="panel">${defs([
        { label: "Session หมดอายุ", value: fmtTime(me.session_expires_at) },
        { label: "Unused days", value: `${overview.policy?.unused_days || 0} วัน` },
        { label: "Min cache age", value: `${overview.policy?.min_cache_age_days || 0} วัน` },
        { label: "Batch limit", value: String(overview.policy?.max_delete_batch || 0) },
      ])}</article>
      <article class="panel">${defs([
        { label: "Low watermark", value: `${overview.policy?.low_watermark_pct || 0}%` },
        { label: "Target free", value: `${overview.policy?.target_free_pct || 0}%` },
        { label: "Emergency free", value: `${overview.policy?.emergency_free_pct || 0}%` },
        { label: "GC hour (UTC)", value: `${overview.policy?.gc_hour_utc || 0}:00` },
      ])}</article>
      <article class="panel">${defs([
        { label: "Registry probe", value: overview.registry?.healthy ? "พร้อม" : "ผิดปกติ" },
        { label: "Upstream probe", value: overview.upstream?.healthy ? "พร้อม" : "ผิดปกติ" },
        { label: "Maintenance mode", value: overview.maintenance?.maintenance_mode ? "เปิด" : "ปิด" },
        { label: "Fallback state", value: fallback.state || "-" },
      ])}</article>
    </section>
    <section class="two-column">
      <article class="panel"><div class="panel-head"><div><h2>Janitor ล่าสุด</h2></div><a href="/jobs">ดูทั้งหมด</a></div>${overview.last_runs?.janitor?.id ? jsonBlock(overview.last_runs.janitor) : '<div class="empty-state">ยังไม่มีประวัติ</div>'}</article>
      <article class="panel"><div class="panel-head"><div><h2>GC ล่าสุด</h2></div><a href="/gc">ไปหน้า GC</a></div>${overview.last_runs?.gc?.id ? jsonBlock(overview.last_runs.gc) : '<div class="empty-state">ยังไม่มีประวัติ</div>'}</article>
    </section>
  `;
  bindMaintenanceButtons(() => renderDashboard(), false);
}

async function renderCache() {
  const data = await api("/api/cache/overview");
  const largest = (data.largest || []).map((item) => `
    <tr>
      <td><a href="/artifact?repo=${encodeURIComponent(item.repo)}&digest=${encodeURIComponent(item.digest)}"><strong>${esc(item.repo)}</strong></a></td>
      <td>${esc(item.tag || "-")}</td>
      <td>${fmtBytes(item.size_bytes)}</td>
      <td>${item.use_count || 0}</td>
      <td>${fmtTime(item.last_used_at)}</td>
      <td>${badgeSet(item)}</td>
    </tr>`).join("");
  const candidates = (data.candidates || []).slice(0, 12).map((item) => `
    <tr>
      <td><a href="/artifact?repo=${encodeURIComponent(item.repo)}&digest=${encodeURIComponent(item.digest)}">${esc(item.repo)}</a></td>
      <td>${esc(item.tag || "-")}</td>
      <td>${fmtBytes(item.size_bytes)}</td>
      <td>${fmtTime(item.last_used_at)}</td>
      <td>${item.use_count || 0}</td>
    </tr>`).join("");
  root.innerHTML = `
    <section class="two-column">
      <article class="panel">${defs([
        { label: "ใช้ไป", value: fmtBytes(data.storage?.used_bytes || 0) },
        { label: "เหลือว่าง", value: fmtBytes(data.storage?.free_bytes || 0) },
        { label: "ใช้ไป (%)", value: `${data.storage?.used_pct || 0}%` },
        { label: "ต้อง reclaim เพิ่ม", value: fmtBytes(data.storage?.bytes_to_target || 0) },
      ])}</article>
      <article class="panel"><div class="panel-head"><div><h2>Fallback snapshot</h2></div><a href="/health">รายละเอียด</a></div>${jsonBlock(data.fallback)}</article>
    </section>
    ${panel("Artifacts ขนาดใหญ่ที่สุด", "ใช้ดูว่าพื้นที่ส่วนใหญ่ถูกใช้ไปกับอะไร", `<div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Size</th><th>Use</th><th>Last used</th><th>Status</th></tr></thead><tbody>${largest || '<tr><td colspan="6"><div class="empty-state">ยังไม่มีข้อมูล</div></td></tr>'}</tbody></table></div>`)}
    ${panel("Cleanup candidates เบื้องต้น", "รายการที่มีสิทธิ์ถูกลบตาม policy ปัจจุบัน", `<div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Size</th><th>Last used</th><th>Use</th></tr></thead><tbody>${candidates || '<tr><td colspan="5"><div class="empty-state">ยังไม่มี candidate</div></td></tr>'}</tbody></table></div>`)}
  `;
}

async function renderArtifacts() {
  const params = currentQuery();
  const search = params.get("search") || "";
  const state = params.get("state") || "all";
  const pinned = params.get("pinned") || "";
  const protectedOnly = params.get("protected") || "";
  const offset = Number(params.get("offset") || "0");
  const limit = 30;
  const data = await api(`/api/artifacts?limit=${limit}&offset=${offset}&search=${encodeURIComponent(search)}&state=${encodeURIComponent(state)}&pinned=${encodeURIComponent(pinned)}&protected=${encodeURIComponent(protectedOnly)}`);
  const rows = (data.items || []).map((item) => `
    <tr>
      <td class="cell-repo"><a href="/artifact?repo=${encodeURIComponent(item.repo)}&digest=${encodeURIComponent(item.digest)}"><strong>${esc(item.repo)}</strong></a></td>
      <td class="cell-tag">${esc(item.tag || "-")}</td>
      <td class="mono cell-digest">${esc(item.digest)}</td>
      <td class="cell-numeric">${fmtBytes(item.size_bytes)}</td>
      <td class="cell-numeric">${item.use_count || 0}</td>
      <td class="cell-date">${fmtTime(item.last_used_at)}</td>
      <td class="cell-status">${badgeSet(item)}</td>
      <td class="cell-actions">
        <div class="inline-actions">
          <button class="ghost-button" data-pin="${item.pinned ? 0 : 1}" data-repo="${esc(item.repo)}" data-digest="${esc(item.digest)}">${item.pinned ? "Unpin" : "Pin"}</button>
          <button class="ghost-button" data-protect="${item.explicit_protected ? 0 : 1}" data-repo="${esc(item.repo)}" data-digest="${esc(item.digest)}">${item.explicit_protected ? "Unprotect" : "Protect"}</button>
        </div>
      </td>
    </tr>`).join("");
  root.innerHTML = `
    ${panel("ค้นหาและกรอง", "คลัง artifact หลักของระบบ", `
      <form id="artifact-filter" class="toolbar">
        <label class="field"><span>ค้นหา</span><input name="search" value="${esc(search)}" placeholder="repo, tag หรือ digest"></label>
        <label class="field"><span>สถานะ</span><select name="state">${["all", "active", "deleted"].map((item) => `<option value="${item}" ${item === state ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <label class="field"><span>Pinned</span><select name="pinned">${["", "true", "false"].map((item) => `<option value="${item}" ${item === pinned ? "selected" : ""}>${item === "" ? "ทั้งหมด" : item}</option>`).join("")}</select></label>
        <label class="field"><span>Explicit protected</span><select name="protected">${["", "true", "false"].map((item) => `<option value="${item}" ${item === protectedOnly ? "selected" : ""}>${item === "" ? "ทั้งหมด" : item}</option>`).join("")}</select></label>
        <div class="field" style="align-self:end"><button class="primary-button" type="submit">ใช้ตัวกรอง</button></div>
      </form>`)}
    ${panel("รายการ Artifacts", "สั่งดู detail, pin และ protect ได้จากหน้านี้", `<div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Digest</th><th>Size</th><th>Use</th><th>Last used</th><th>Status</th><th>Action</th></tr></thead><tbody>${rows || '<tr><td colspan="8"><div class="empty-state">ไม่พบข้อมูล</div></td></tr>'}</tbody></table></div>${pager(data)}`)}
  `;

  root.querySelector("#artifact-filter")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({
      search: form.get("search"),
      state: form.get("state"),
      pinned: form.get("pinned"),
      protected: form.get("protected"),
      offset: 0,
    });
  });
  root.querySelectorAll("[data-pin]").forEach((button) => button.addEventListener("click", async () => {
    await postJSON("/api/artifacts/pin", { repo: button.dataset.repo, digest: button.dataset.digest, pinned: button.dataset.pin === "1" });
    showFlash("อัปเดต pin แล้ว");
    await renderArtifacts();
  }));
  root.querySelectorAll("[data-protect]").forEach((button) => button.addEventListener("click", async () => {
    await postJSON("/api/artifacts/protect", { repo: button.dataset.repo, digest: button.dataset.digest, protected: button.dataset.protect === "1" });
    showFlash("อัปเดต protection แล้ว");
    await renderArtifacts();
  }));
  bindPager(offset, limit);
}

async function renderArtifact() {
  const params = currentQuery();
  const repo = params.get("repo") || "";
  const digest = params.get("digest") || "";
  if (!repo || !digest) {
    root.innerHTML = '<div class="empty-state">ต้องระบุ repo และ digest ใน URL</div>';
    return;
  }
  const [detail, history] = await Promise.all([
    api(`/api/artifacts/detail?repo=${encodeURIComponent(repo)}&digest=${encodeURIComponent(digest)}`),
    api(`/api/artifacts/history?repo=${encodeURIComponent(repo)}&digest=${encodeURIComponent(digest)}`),
  ]);
  const artifact = detail.artifact;
  const events = (history.events || []).map((item) => `
    <tr>
      <td>${fmtTime(item.received_at)}</td>
      <td>${esc(item.action || "-")}</td>
      <td>${esc(item.tag || "-")}</td>
      <td><details><summary>raw</summary>${jsonBlock(item.raw_json)}</details></td>
    </tr>`).join("");
  const logs = (history.logs || []).map((item) => `
    <tr>
      <td>${fmtTime(item.created_at)}</td>
      <td>${esc(item.level)}</td>
      <td>${esc(item.scope)}</td>
      <td>${esc(item.message)}</td>
      <td><details><summary>details</summary>${jsonBlock(item.details)}</details></td>
    </tr>`).join("");
  root.innerHTML = `
    <section class="two-column">
      <article class="panel">
        ${defs([
          { label: "Repo", value: artifact.repo },
          { label: "Digest", value: artifact.digest, mono: true },
          { label: "Tag ล่าสุด", value: artifact.tag || "-" },
          { label: "Size", value: fmtBytes(artifact.size_bytes) },
          { label: "Use count", value: String(artifact.use_count || 0) },
          { label: "First seen", value: fmtTime(artifact.first_seen_at) },
          { label: "Last used", value: fmtTime(artifact.last_used_at) },
        ])}
        <div class="badge-row" style="margin-top:1rem">${badgeSet(artifact)}</div>
        <div class="toolbar" style="margin-top:1rem">
          <button class="ghost-button" id="artifact-pin">${artifact.pinned ? "Unpin" : "Pin"}</button>
          <button class="ghost-button" id="artifact-protect">${artifact.explicit_protected ? "Unprotect" : "Protect"}</button>
        </div>
      </article>
      <article class="panel">
        <div class="panel-head"><div><h2>เหตุผลด้าน cleanup</h2></div></div>
        ${(artifact.blocked_reasons || []).length ? `<div class="badge-row">${artifact.blocked_reasons.map((reason) => pill(reason, "warn")).join("")}</div>` : '<div class="empty-state">artifact นี้เข้าเงื่อนไข candidate ตาม policy ปัจจุบัน</div>'}
        <div class="callout" style="margin-top:1rem"><strong>Tags ที่เคยเห็น:</strong> ${(detail.tags || []).map((tag) => pill(tag)).join(" ") || "ไม่มี"}</div>
        ${artifact.delete_reason ? `<div class="callout" style="margin-top:1rem"><strong>Delete reason:</strong> ${esc(artifact.delete_reason)}</div>` : ""}
      </article>
    </section>
    ${panel("Webhook events ล่าสุด", "ข้อมูลอ้างอิงจาก registry notifications", `<div class="table-wrap"><table><thead><tr><th>เวลา</th><th>Action</th><th>Tag</th><th>Raw</th></tr></thead><tbody>${events || '<tr><td colspan="4"><div class="empty-state">ยังไม่มี event</div></td></tr>'}</tbody></table></div>`)}
    ${panel("กิจกรรมระบบที่เกี่ยวข้อง", "อ้างอิงจาก system logs ภายใน", `<div class="table-wrap"><table><thead><tr><th>เวลา</th><th>Level</th><th>Scope</th><th>Message</th><th>Details</th></tr></thead><tbody>${logs || '<tr><td colspan="5"><div class="empty-state">ยังไม่มี log ที่เกี่ยวข้อง</div></td></tr>'}</tbody></table></div>`)}
  `;
  root.querySelector("#artifact-pin")?.addEventListener("click", async () => {
    await postJSON("/api/artifacts/pin", { repo, digest, pinned: !artifact.pinned });
    showFlash("อัปเดต pin แล้ว");
    await renderArtifact();
  });
  root.querySelector("#artifact-protect")?.addEventListener("click", async () => {
    await postJSON("/api/artifacts/protect", { repo, digest, protected: !artifact.explicit_protected });
    showFlash("อัปเดต protection แล้ว");
    await renderArtifact();
  });
}

async function renderEvents() {
  const params = currentQuery();
  const includeRaw = params.get("include_raw") === "true";
  const offset = Number(params.get("offset") || "0");
  const limit = 30;
  const data = await api(`/api/events?limit=${limit}&offset=${offset}&include_raw=${includeRaw}`);
  const rows = (data.items || []).map((item) => `
    <tr>
      <td class="mono cell-numeric">${item.id}</td>
      <td class="cell-date">${fmtTime(item.received_at)}</td>
      <td>${esc(item.action || "-")}</td>
      <td class="cell-repo">${esc(item.repo || "-")}</td>
      <td class="cell-tag">${esc(item.tag || "-")}</td>
      <td class="mono cell-digest">${esc(item.digest || "-")}</td>
      <td>${includeRaw ? `<details><summary>raw</summary>${jsonBlock(item.raw_json)}</details>` : '<span class="muted">ปิด raw</span>'}</td>
    </tr>`).join("");
  root.innerHTML = `
    ${panel("เหตุการณ์จาก Registry", "ใช้ตรวจสอบ webhook ที่ถูกส่งเข้าระบบ", `
      <form id="events-filter" class="toolbar">
        <label class="checkbox-row"><input type="checkbox" name="include_raw" ${includeRaw ? "checked" : ""}> แสดง raw JSON</label>
        <button class="primary-button" type="submit">รีเฟรช</button>
      </form>
      <div class="table-wrap"><table><thead><tr><th>ID</th><th>เวลา</th><th>Action</th><th>Repo</th><th>Tag</th><th>Digest</th><th>Raw</th></tr></thead><tbody>${rows || '<tr><td colspan="7"><div class="empty-state">ยังไม่มี event</div></td></tr>'}</tbody></table></div>
      ${pager(data)}`)}
  `;
  root.querySelector("#events-filter")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({ include_raw: form.get("include_raw") ? "true" : "", offset: 0 });
  });
  bindPager(offset, limit);
}

async function renderJobs() {
  const params = currentQuery();
  const jobType = params.get("job_type") || "all";
  const status = params.get("status") || "all";
  const offset = Number(params.get("offset") || "0");
  const limit = 25;
  const data = await api(`/api/jobs?limit=${limit}&offset=${offset}&job_type=${encodeURIComponent(jobType)}&status=${encodeURIComponent(status)}`);
  root.innerHTML = `
    ${panel("สถานะงานเบื้องหลัง", "ดูประวัติ cleanup, GC และงานระบบที่เกี่ยวข้อง", `
      <form id="jobs-filter" class="toolbar">
        <label class="field"><span>Job type</span><select name="job_type">${["all", "janitor", "gc"].map((item) => `<option value="${item}" ${item === jobType ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <label class="field"><span>Status</span><select name="status">${["all", "running", "success", "partial", "dry-run", "skipped", "error"].map((item) => `<option value="${item}" ${item === status ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <div class="field" style="align-self:end"><button class="primary-button" type="submit">ใช้ตัวกรอง</button></div>
      </form>
      <div class="table-wrap"><table><thead><tr><th>ID</th><th>Type</th><th>Trigger</th><th>Status</th><th>Started</th><th>Finished</th><th>Details</th></tr></thead><tbody>${renderJobRows(data.items) || '<tr><td colspan="7"><div class="empty-state">ยังไม่มีประวัติ</div></td></tr>'}</tbody></table></div>
      ${pager(data)}`)}
  `;
  root.querySelector("#jobs-filter")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({ job_type: form.get("job_type"), status: form.get("status"), offset: 0 });
  });
  bindPager(offset, limit);
}

async function renderProtections() {
  const params = currentQuery();
  const search = params.get("search") || "";
  const [config, pinned, explicitProtected, lookup] = await Promise.all([
    api("/api/system/config"),
    api("/api/artifacts?limit=25&offset=0&state=active&pinned=true"),
    api("/api/artifacts?limit=25&offset=0&state=active&protected=true"),
    api(`/api/artifacts?limit=25&offset=0&state=active&search=${encodeURIComponent(search)}`),
  ]);

  const manageRows = (lookup.items || []).map((item) => `
    <tr>
      <td><a href="/artifact?repo=${encodeURIComponent(item.repo)}&digest=${encodeURIComponent(item.digest)}">${esc(item.repo)}</a></td>
      <td>${esc(item.tag || "-")}</td>
      <td>${badgeSet(item)}</td>
      <td>
        <div class="inline-actions">
          <button class="ghost-button" data-pin="${item.pinned ? 0 : 1}" data-repo="${esc(item.repo)}" data-digest="${esc(item.digest)}">${item.pinned ? "Unpin" : "Pin"}</button>
          <button class="ghost-button" data-protect="${item.explicit_protected ? 0 : 1}" data-repo="${esc(item.repo)}" data-digest="${esc(item.digest)}">${item.explicit_protected ? "Unprotect" : "Protect"}</button>
        </div>
      </td>
    </tr>`).join("");
  const pinnedRows = (pinned.items || []).map((item) => `<tr><td>${esc(item.repo)}</td><td>${esc(item.tag || "-")}</td><td>${fmtTime(item.last_used_at)}</td><td><button class="ghost-button" data-pin="0" data-repo="${esc(item.repo)}" data-digest="${esc(item.digest)}">Unpin</button></td></tr>`).join("");
  const protectedRows = (explicitProtected.items || []).map((item) => `<tr><td>${esc(item.repo)}</td><td>${esc(item.tag || "-")}</td><td>${fmtTime(item.last_used_at)}</td><td><button class="ghost-button" data-protect="0" data-repo="${esc(item.repo)}" data-digest="${esc(item.digest)}">Unprotect</button></td></tr>`).join("");

  root.innerHTML = `
    <section class="two-column">
      <article class="panel">${defs([
        { label: "Regex repo protection", value: config.protected_repos_regex || "-" },
        { label: "Regex tag protection", value: config.protected_tags_regex || "-" },
        { label: "หมายเหตุ", value: "Regex protection มาจาก policy และแก้รายตัวไม่ได้จากหน้านี้" },
      ])}</article>
      <article class="panel">
        <div class="panel-head"><div><h2>ค้นหาเพื่อจัดการ</h2><p>ใช้ pin/unpin และ protect/unprotect ราย artifact</p></div></div>
        <form id="protect-search" class="toolbar">
          <label class="field"><span>ค้นหา</span><input name="search" value="${esc(search)}" placeholder="repo, tag หรือ digest"></label>
          <div class="field" style="align-self:end"><button class="primary-button" type="submit">ค้นหา</button></div>
        </form>
        <div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Status</th><th>Action</th></tr></thead><tbody>${manageRows || '<tr><td colspan="4"><div class="empty-state">กรอกคำค้นหรือใช้รายการล่าสุด</div></td></tr>'}</tbody></table></div>
      </article>
    </section>
    ${panel("Pinned ล่าสุด", "รายการที่ถูก pin ไว้และจะไม่ถูก cleanup อัตโนมัติ", `<div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Last used</th><th>Action</th></tr></thead><tbody>${pinnedRows || '<tr><td colspan="4"><div class="empty-state">ยังไม่มี pinned artifact</div></td></tr>'}</tbody></table></div>`)}
    ${panel("Explicit protected ล่าสุด", "รายการที่ operator ป้องกันไว้รายตัว", `<div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Last used</th><th>Action</th></tr></thead><tbody>${protectedRows || '<tr><td colspan="4"><div class="empty-state">ยังไม่มี explicit protected artifact</div></td></tr>'}</tbody></table></div>`)}
  `;
  root.querySelector("#protect-search")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({ search: form.get("search") || "", offset: 0 });
  });
  root.querySelectorAll("[data-pin]").forEach((button) => button.addEventListener("click", async () => {
    await postJSON("/api/artifacts/pin", { repo: button.dataset.repo, digest: button.dataset.digest, pinned: button.dataset.pin === "1" });
    showFlash("อัปเดต pin แล้ว");
    await renderProtections();
  }));
  root.querySelectorAll("[data-protect]").forEach((button) => button.addEventListener("click", async () => {
    await postJSON("/api/artifacts/protect", { repo: button.dataset.repo, digest: button.dataset.digest, protected: button.dataset.protect === "1" });
    showFlash("อัปเดต protection แล้ว");
    await renderProtections();
  }));
}

async function renderCleanup() {
  const params = currentQuery();
  const status = params.get("status") || "all";
  const offset = Number(params.get("offset") || "0");
  const limit = 20;
  const [overview, candidates, history] = await Promise.all([
    api("/api/system/overview"),
    api("/api/cleanup/candidates?limit=100"),
    api(`/api/cleanup/history?limit=${limit}&offset=${offset}&status=${encodeURIComponent(status)}`),
  ]);
  const candidateRows = (candidates.items || []).map((item) => `
    <tr>
      <td><a href="/artifact?repo=${encodeURIComponent(item.repo)}&digest=${encodeURIComponent(item.digest)}">${esc(item.repo)}</a></td>
      <td>${esc(item.tag || "-")}</td>
      <td>${fmtBytes(item.size_bytes)}</td>
      <td>${fmtTime(item.last_used_at)}</td>
      <td>${item.use_count || 0}</td>
    </tr>`).join("");
  root.innerHTML = `
    <section class="two-column">
      <article class="panel">
        <div class="status-banner">
          <div>
            <strong>${esc(overview.fallback?.summary || "-")}</strong>
            <p class="muted">${esc(overview.fallback?.details || "-")}</p>
          </div>
          <div class="badge-row">
            ${pill(`fallback: ${overview.fallback?.state || "-"}`, statusTone(overview.fallback?.state))}
            ${pill(`free: ${overview.storage?.free_pct || 0}%`, Number(overview.storage?.free_pct || 0) < Number(overview.policy?.low_watermark_pct || 0) ? "warn" : "success")}
          </div>
        </div>
        <div style="margin-top:1rem">${maintenanceButtons(true)}</div>
      </article>
      <article class="panel">${defs([
        { label: "Eligible candidates", value: String(overview.counts?.eligible_candidates || 0) },
        { label: "Low watermark", value: `${overview.policy?.low_watermark_pct || 0}%` },
        { label: "Target free", value: `${overview.policy?.target_free_pct || 0}%` },
        { label: "Emergency free", value: `${overview.policy?.emergency_free_pct || 0}%` },
      ])}</article>
    </section>
    ${panel("Cleanup candidates", "รายการที่มีสิทธิ์ถูกลบตาม policy ณ ตอนนี้", `<div class="table-wrap"><table><thead><tr><th>Repo</th><th>Tag</th><th>Size</th><th>Last used</th><th>Use</th></tr></thead><tbody>${candidateRows || '<tr><td colspan="5"><div class="empty-state">ยังไม่มี candidate</div></td></tr>'}</tbody></table></div>`)}
    ${panel("ประวัติ Cleanup", "ดู dry-run, skipped, success และ error ย้อนหลัง", `
      <form id="cleanup-filter" class="toolbar">
        <label class="field"><span>Status</span><select name="status">${["all", "running", "success", "partial", "dry-run", "skipped", "error"].map((item) => `<option value="${item}" ${item === status ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <div class="field" style="align-self:end"><button class="primary-button" type="submit">ใช้ตัวกรอง</button></div>
      </form>
      <div class="table-wrap"><table><thead><tr><th>ID</th><th>Type</th><th>Trigger</th><th>Status</th><th>Started</th><th>Finished</th><th>Details</th></tr></thead><tbody>${renderJobRows(history.items) || '<tr><td colspan="7"><div class="empty-state">ยังไม่มีประวัติ cleanup</div></td></tr>'}</tbody></table></div>
      ${pager(history)}`)}
  `;
  root.querySelector("#cleanup-filter")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({ status: form.get("status"), offset: 0 });
  });
  bindMaintenanceButtons(() => renderCleanup(), true);
  bindPager(offset, limit);
}

async function renderGC() {
  const params = currentQuery();
  const status = params.get("status") || "all";
  const offset = Number(params.get("offset") || "0");
  const limit = 20;
  const [gcStatus, history, fallback] = await Promise.all([
    api("/api/gc/status"),
    api(`/api/gc/history?limit=${limit}&offset=${offset}&status=${encodeURIComponent(status)}`),
    api("/api/system/fallback"),
  ]);
  root.innerHTML = `
    <section class="two-column">
      <article class="panel">
        <div class="status-banner">
          <div>
            <strong>สถานะคิว GC: ${gcStatus.pending ? "มีงานรอ" : "ไม่มีงานรอ"}</strong>
            <p class="muted">fallback ปัจจุบัน: ${esc(gcStatus.fallback_state || "-")}</p>
          </div>
          <div class="badge-row">
            ${pill(`GC pending: ${gcStatus.pending ? "yes" : "no"}`, gcStatus.pending ? "warn" : "success")}
            ${pill(`fallback: ${fallback.state || "-"}`, statusTone(fallback.state))}
          </div>
        </div>
        <div style="margin-top:1rem">${maintenanceButtons(false)}</div>
        <div class="toolbar" style="margin-top:1rem">
          <button class="ghost-button" id="clear-gc-flag" type="button" ${gcStatus.pending ? "" : "disabled"}>ล้าง GC request flag</button>
        </div>
      </article>
      <article class="panel"><div class="panel-head"><div><h2>GC ล่าสุด</h2><p>ผลการรันล่าสุด</p></div></div>${gcStatus.last_gc?.id ? jsonBlock(gcStatus.last_gc) : '<div class="empty-state">ยังไม่มีประวัติ</div>'}</article>
    </section>
    ${panel("ประวัติ GC", "ใช้ตรวจสอบ downtime, logs tail และผลลัพธ์ย้อนหลัง", `
      <form id="gc-filter" class="toolbar">
        <label class="field"><span>Status</span><select name="status">${["all", "running", "success", "skipped", "error"].map((item) => `<option value="${item}" ${item === status ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <div class="field" style="align-self:end"><button class="primary-button" type="submit">ใช้ตัวกรอง</button></div>
      </form>
      <div class="table-wrap"><table><thead><tr><th>ID</th><th>Type</th><th>Trigger</th><th>Status</th><th>Started</th><th>Finished</th><th>Details</th></tr></thead><tbody>${renderJobRows(history.items) || '<tr><td colspan="7"><div class="empty-state">ยังไม่มีประวัติ GC</div></td></tr>'}</tbody></table></div>
      ${pager(history)}`)}
  `;
  bindMaintenanceButtons(() => renderGC(), false);
  root.querySelector("#clear-gc-flag")?.addEventListener("click", async () => {
    if (!window.confirm("ยืนยันล้าง GC request flag?")) return;
    await postJSON("/api/gc/clear-request", {});
    showFlash("ล้าง GC request flag แล้ว");
    await renderGC();
  });
  root.querySelector("#gc-filter")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({ status: form.get("status"), offset: 0 });
  });
  bindPager(offset, limit);
}

async function renderHealth() {
  const [fallback, overview] = await Promise.all([api("/api/system/fallback"), api("/api/system/overview")]);
  root.innerHTML = `
    <section class="hero-grid">
      <article class="panel">
        <div class="status-banner">
          <div>
            <strong>${esc(fallback.summary || "-")}</strong>
            <p class="muted">${esc(fallback.details || "-")}</p>
          </div>
          <div class="badge-row">
            ${pill(`state: ${fallback.state || "-"}`, statusTone(fallback.state))}
            ${pill(`cached mode: ${fallback.cached_mode_usable ? "ใช้ได้" : "ใช้ไม่ได้"}`, fallback.cached_mode_usable ? "success" : "danger")}
            ${pill(`destructive: ${fallback.destructive_paused ? "paused" : "ready"}`, fallback.destructive_paused ? "warn" : "success")}
          </div>
        </div>
        <div class="callout" style="margin-top:1rem">
          <strong>Fallback behavior:</strong> เมื่อ upstream มีปัญหา ระบบจะรักษา cache เดิมไว้, หยุดงานลบอัตโนมัติที่เสี่ยง, และเปิดให้ operator เห็นสถานะชัดเจนจากหน้านี้
        </div>
      </article>
      <article class="panel">${defs([
        { label: "Registry healthy", value: fallback.registry?.healthy ? "ใช่" : "ไม่ใช่" },
        { label: "Registry status code", value: String(fallback.registry?.status_code || 0) },
        { label: "Upstream healthy", value: fallback.upstream?.healthy ? "ใช่" : "ไม่ใช่" },
        { label: "Upstream status code", value: String(fallback.upstream?.status_code || 0) },
      ])}</article>
    </section>
    <section class="two-column">
      <article class="panel">${defs([
        { label: "Storage path", value: fallback.storage?.path || "-" },
        { label: "Free", value: fmtBytes(fallback.storage?.free_bytes || 0) },
        { label: "Used", value: fmtBytes(fallback.storage?.used_bytes || 0) },
        { label: "Free %", value: `${fallback.storage?.free_pct || 0}%` },
        { label: "Pressure", value: fallback.storage?.pressure ? "ใช่" : "ไม่ใช่" },
        { label: "Emergency", value: fallback.storage?.emergency ? "ใช่" : "ไม่ใช่" },
      ])}</article>
      <article class="panel">${defs([
        { label: "Maintenance mode", value: fmtBool(fallback.maintenance?.maintenance_mode) },
        { label: "Janitor paused", value: fmtBool(fallback.maintenance?.janitor_paused) },
        { label: "GC paused", value: fmtBool(fallback.maintenance?.gc_paused) },
        { label: "GC running", value: fmtBool(fallback.maintenance?.gc_running) },
        { label: "Janitor running", value: fmtBool(fallback.maintenance?.janitor_running) },
        { label: "Maintenance note", value: fallback.maintenance?.note || "-" },
      ])}</article>
    </section>
    ${panel("Probe details", "รายละเอียดการตรวจสุขภาพล่าสุด", `<div class="two-column"><div class="panel" style="box-shadow:none"><div class="panel-head"><div><h3>Registry</h3></div></div>${jsonBlock(fallback.registry)}</div><div class="panel" style="box-shadow:none"><div class="panel-head"><div><h3>Upstream</h3></div></div>${jsonBlock(fallback.upstream)}</div></div>`)}
    ${panel("Policy snapshot", "ข้อมูลประกอบการตัดสินใจด้าน cleanup และ fallback", jsonBlock(overview.policy))}
  `;
}

async function renderMaintenance() {
  const [state, overview, jobs] = await Promise.all([
    api("/api/maintenance/state"),
    api("/api/system/overview"),
    api("/api/jobs?limit=12&offset=0"),
  ]);
  root.innerHTML = `
    <section class="two-column">
      <article class="panel">
        <div class="panel-head"><div><h2>Maintenance controls</h2><p>ควบคุมการหยุดงานอัตโนมัติและใส่บันทึกประกอบได้จากหน้านี้</p></div></div>
        <form id="maintenance-form" class="form-grid">
          <label class="checkbox-row"><input type="checkbox" name="maintenance_mode" ${state.maintenance_mode ? "checked" : ""}> เปิด maintenance mode</label>
          <label class="checkbox-row"><input type="checkbox" name="janitor_paused" ${state.janitor_paused ? "checked" : ""}> Pause janitor</label>
          <label class="checkbox-row"><input type="checkbox" name="gc_paused" ${state.gc_paused ? "checked" : ""}> Pause GC</label>
          <label><span>บันทึกสำหรับ operator</span><textarea name="note" rows="4" placeholder="เช่น กำลังขยายดิสก์ หรือหยุดเพื่อ maintenance">${esc(state.note || "")}</textarea></label>
          <div class="inline-actions">
            <button class="primary-button" type="submit">บันทึกสถานะ</button>
            <button class="ghost-button" type="button" id="maintenance-reset">ล้างข้อความ</button>
          </div>
        </form>
      </article>
      <article class="panel">
        <div class="panel-head"><div><h2>สถานะปัจจุบัน</h2><p>ดูสัญญาณ runtime ที่มีผลต่อการปฏิบัติการ</p></div></div>
        ${defs([
          { label: "Fallback state", value: overview.fallback?.state || "-" },
          { label: "Maintenance mode", value: fmtBool(state.maintenance_mode) },
          { label: "Janitor paused", value: fmtBool(state.janitor_paused) },
          { label: "GC paused", value: fmtBool(state.gc_paused) },
          { label: "Janitor running", value: fmtBool(overview.signals?.janitor_running) },
          { label: "GC running", value: fmtBool(overview.signals?.gc_running) },
          { label: "Updated at", value: fmtTime(state.updated_at) },
        ])}
        <div style="margin-top:1rem">${maintenanceButtons(true)}</div>
      </article>
    </section>
    ${panel("งานล่าสุด", "ช่วยให้เห็นผลจาก action ที่เพิ่งสั่ง", `<div class="table-wrap"><table><thead><tr><th>ID</th><th>Type</th><th>Trigger</th><th>Status</th><th>Started</th><th>Finished</th><th>Details</th></tr></thead><tbody>${renderJobRows(jobs.items) || '<tr><td colspan="7"><div class="empty-state">ยังไม่มีประวัติ</div></td></tr>'}</tbody></table></div>`)}
  `;
  root.querySelector("#maintenance-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const payload = {
      maintenance_mode: !!form.get("maintenance_mode"),
      janitor_paused: !!form.get("janitor_paused"),
      gc_paused: !!form.get("gc_paused"),
      note: String(form.get("note") || ""),
    };
    await postJSON("/api/maintenance/state", payload);
    showFlash("บันทึก maintenance state แล้ว");
    await renderMaintenance();
  });
  root.querySelector("#maintenance-reset")?.addEventListener("click", () => {
    root.querySelector("textarea[name='note']").value = "";
  });
  bindMaintenanceButtons(() => renderMaintenance(), true);
}

async function renderLogs() {
  const params = currentQuery();
  const level = params.get("level") || "all";
  const scope = params.get("scope") || "all";
  const actor = params.get("actor") || "";
  const search = params.get("search") || "";
  const live = params.get("live") === "true";
  const offset = Number(params.get("offset") || "0");
  const limit = 50;
  const data = await api(`/api/system/logs?limit=${limit}&offset=${offset}&level=${encodeURIComponent(level)}&scope=${encodeURIComponent(scope)}&actor=${encodeURIComponent(actor)}&search=${encodeURIComponent(search)}`);
  const latestID = Math.max(0, ...(data.items || []).map((item) => Number(item.id || 0)));
  root.innerHTML = `
    ${panel("Logs และกิจกรรมระบบ", "รองรับการค้นหา, กรอง และดูแบบ near-live", `
      <form id="logs-filter" class="toolbar">
        <label class="field"><span>Level</span><select name="level">${["all", "info", "warn", "error"].map((item) => `<option value="${item}" ${item === level ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <label class="field"><span>Scope</span><select name="scope">${["all", "startup", "auth", "webhook", "artifact", "janitor", "gc", "health", "maintenance"].map((item) => `<option value="${item}" ${item === scope ? "selected" : ""}>${item}</option>`).join("")}</select></label>
        <label class="field"><span>Actor</span><input name="actor" value="${esc(actor)}" placeholder="username"></label>
        <label class="field"><span>Search</span><input name="search" value="${esc(search)}" placeholder="message หรือ details"></label>
        <label class="checkbox-row"><input type="checkbox" name="live" ${live ? "checked" : ""}> near-live</label>
        <div class="field" style="align-self:end"><button class="primary-button" type="submit">ใช้ตัวกรอง</button></div>
      </form>
      <div class="table-wrap"><table><thead><tr><th>ID</th><th>เวลา</th><th>Level</th><th>Scope</th><th>Actor</th><th>Message</th><th>Details</th></tr></thead><tbody id="logs-body">${renderLogRows(data.items) || '<tr><td colspan="7"><div class="empty-state">ยังไม่มี log</div></td></tr>'}</tbody></table></div>
      ${pager(data)}`)}
  `;

  root.querySelector("#logs-filter")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setQuery({
      level: form.get("level"),
      scope: form.get("scope"),
      actor: form.get("actor"),
      search: form.get("search"),
      live: form.get("live") ? "true" : "",
      offset: 0,
    });
  });
  bindPager(offset, limit);

  if (live && offset === 0) {
    let lastID = latestID;
    startLiveTimer(async () => {
      const next = await api(`/api/system/logs?limit=100&offset=0&level=${encodeURIComponent(level)}&scope=${encodeURIComponent(scope)}&actor=${encodeURIComponent(actor)}&search=${encodeURIComponent(search)}&after_id=${lastID}`);
      if (!(next.items || []).length) return;
      lastID = Math.max(lastID, ...next.items.map((item) => Number(item.id || 0)));
      const tbody = root.querySelector("#logs-body");
      if (!tbody) return;
      const empty = tbody.querySelector(".empty-state");
      if (empty) tbody.innerHTML = "";
      tbody.insertAdjacentHTML("afterbegin", renderLogRows(next.items));
      enhanceTables(root);
      while (tbody.children.length > 200) tbody.removeChild(tbody.lastElementChild);
    }, 4000);
  }
}

async function renderSettings() {
  const [config, fallback] = await Promise.all([api("/api/system/config"), api("/api/system/fallback")]);
  root.innerHTML = `
    <section class="three-column">
      <article class="panel">${defs([
        { label: "Registry URL", value: config.registry_url || "-", mono: true },
        { label: "Public base URL", value: config.public_base_url || "-", mono: true },
        { label: "Listen port", value: String(config.listen_port || config.control_port || 8080) },
        { label: "Cookie secure", value: fmtBool(config.cookie_secure) },
      ])}</article>
      <article class="panel">${defs([
        { label: "Session TTL", value: `${config.session_ttl_hours || 0} ชั่วโมง` },
        { label: "Session refresh", value: `${config.session_refresh_minutes || 0} นาที` },
        { label: "Login max attempts", value: String(config.login_max_attempts || 0) },
        { label: "Login lock", value: `${config.login_lock_minutes || 0} นาที` },
      ])}</article>
      <article class="panel">${defs([
        { label: "Current fallback", value: fallback.state || "-" },
        { label: "GC request flag", value: config.gc_request_flag || "-", mono: true },
        { label: "SQLite path", value: config.sqlite_path || "-", mono: true },
        { label: "Registry data path", value: config.registry_data_path || "-", mono: true },
      ])}</article>
    </section>
    ${panel("Cleanup policy", "ค่า policy ที่ runtime ใช้งานอยู่จริง", defs([
      { label: "Dry run default", value: fmtBool(config.dry_run) },
      { label: "Janitor interval", value: `${config.janitor_interval_seconds || 0} วินาที` },
      { label: "Max delete batch", value: String(config.max_delete_batch || 0) },
      { label: "Unused days", value: `${config.unused_days || 0} วัน` },
      { label: "Min cache age", value: `${config.min_cache_age_days || 0} วัน` },
      { label: "Low watermark", value: `${config.low_watermark_pct || 0}%` },
      { label: "Target free", value: `${config.target_free_pct || 0}%` },
      { label: "Emergency free", value: `${config.emergency_free_pct || 0}%` },
      { label: "GC hour UTC", value: `${config.gc_hour_utc || 0}:00` },
    ]))}
    ${panel("Protection และ fallback", "ใช้เพื่ออธิบายว่าระบบจะเก็บหรือลบอะไรเมื่อไร", defs([
      { label: "Protected repos regex", value: config.protected_repos_regex || "-" },
      { label: "Protected tags regex", value: config.protected_tags_regex || "-" },
      { label: "Upstream health URL", value: config.upstream_health_url || "-", mono: true },
      { label: "Health interval", value: `${config.health_check_interval_seconds || 0} วินาที` },
      { label: "Upstream timeout", value: `${config.upstream_timeout_seconds || 0} วินาที` },
    ]))}
    ${panel("Retention", "อายุการเก็บ metadata ภายในระบบ", defs([
      { label: "Log retention", value: `${config.log_retention_days || 0} วัน` },
      { label: "Event retention", value: `${config.event_retention_days || 0} วัน` },
      { label: "Job retention", value: `${config.job_retention_days || 0} วัน` },
      { label: "Bootstrap username", value: config.bootstrap_username || "-" },
    ]))}
  `;
}

function passwordForm(title, subtitle, forced) {
  root.innerHTML = panel(title, subtitle, `
    <form id="password-form" class="form-grid">
      <label><span>รหัสผ่านปัจจุบัน</span><input type="password" name="current_password" autocomplete="current-password" required></label>
      <label><span>รหัสผ่านใหม่</span><input type="password" name="new_password" autocomplete="new-password" required></label>
      <label><span>ยืนยันรหัสผ่านใหม่</span><input type="password" name="confirm_password" autocomplete="new-password" required></label>
      <div class="callout"><strong>กติกา:</strong> อย่างน้อย 12 ตัวอักษร, ต้องมีตัวอักษรและตัวเลข, และไม่ควรใช้ชื่อผู้ใช้เป็นรหัสผ่าน</div>
      ${forced ? '<div class="callout"><strong>บังคับเปลี่ยนรหัสผ่าน:</strong> หลังเปลี่ยนแล้วจึงจะเข้าใช้งานหน้าจออื่นได้</div>' : ""}
      <div class="inline-actions">
        <button class="primary-button" type="submit">บันทึกรหัสผ่านใหม่</button>
        ${forced ? "" : '<a class="ghost-button" href="/dashboard">ยกเลิก</a>'}
      </div>
    </form>
  `);
  root.querySelector("#password-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const result = await postJSON("/api/auth/change-password", {
      current_password: String(form.get("current_password") || ""),
      new_password: String(form.get("new_password") || ""),
      confirm_password: String(form.get("confirm_password") || ""),
    });
    showFlash("เปลี่ยนรหัสผ่านสำเร็จ");
    setTimeout(() => {
      window.location.href = result.redirect || "/dashboard";
    }, 300);
  });
}

async function renderPassword() {
  passwordForm("เปลี่ยนรหัสผ่าน", "ใช้หน้านี้สำหรับเปลี่ยนรหัสผ่านภายหลังการติดตั้ง", false);
}

async function renderForcePassword() {
  passwordForm("เปลี่ยนรหัสผ่านครั้งแรก", "ระบบบังคับให้เปลี่ยนรหัสผ่านก่อนใช้งานส่วนอื่น เพื่อความปลอดภัย", true);
}

const pageRenderers = {
  dashboard: renderDashboard,
  cache: renderCache,
  artifacts: renderArtifacts,
  artifact: renderArtifact,
  events: renderEvents,
  jobs: renderJobs,
  protections: renderProtections,
  cleanup: renderCleanup,
  gc: renderGC,
  health: renderHealth,
  maintenance: renderMaintenance,
  logs: renderLogs,
  settings: renderSettings,
  password: renderPassword,
  "force-password": renderForcePassword,
};

window.__pageReload = async () => {
  stopLiveTimer();
  clearFlash();
  const renderer = pageRenderers[pageId];
  if (!renderer) {
    root.innerHTML = '<div class="empty-state">ไม่พบหน้าที่ต้องการ</div>';
    return;
  }
  try {
    await renderer();
    enhanceTables(root);
  } catch (error) {
    console.error(error);
    root.innerHTML = `<div class="empty-state">โหลดข้อมูลไม่สำเร็จ กรุณาลองอีกครั้ง</div>`;
    showFlash(error.message || "โหลดข้อมูลไม่สำเร็จ", "error");
  }
};

window.__pageReload();
