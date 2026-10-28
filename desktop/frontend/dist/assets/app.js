// OpenLaw 语料台前端：只经 Wails 绑定调用 Go 方法（window.go.main.App.*），
// 不直连数据库、不发网络请求——写路径全部在 Go 侧（与 CLI 同一代码路径）。
"use strict";

const $ = (sel) => document.querySelector(sel);
const app = () => window.go && window.go.main && window.go.main.App;

let queue = [];
let selected = null;

function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.style.display = "block";
  setTimeout(() => (t.style.display = "none"), 4000);
}

document.querySelectorAll(".tab").forEach((b) =>
  b.addEventListener("click", () => {
    document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x === b));
    $("#tab-status").classList.toggle("hidden", b.dataset.tab !== "status");
    $("#tab-confirm").classList.toggle("hidden", b.dataset.tab !== "confirm");
    if (b.dataset.tab === "status") loadStatus();
  })
);

async function loadStatus() {
  if (!app()) return;
  const v = await app().Status();
  $("#schema-badge").textContent = `schema v${v.schema_version}（支持 ${v.schema_min}–${v.schema_max}）`;
  $("#status-err").textContent = v.open_err ? "语料打开失败：" + v.open_err : "";
  const cards = [
    ["法规", v.laws], ["版本", v.versions], ["条文", v.articles], ["文档", v.documents],
    ["pending", v.pending], ["confirmed", v.confirmed], ["rejected", v.rejected],
    ["gov.cn 主站", v.govcn], ["gov.cn 地方政府", v.govlocal],
  ];
  $("#status-cards").innerHTML = cards
    .map(([k, n]) => `<div class="card"><b>${(n ?? 0).toLocaleString()}</b><span>${k}</span></div>`)
    .join("");
  $("#level-table tbody").innerHTML = (v.by_level || [])
    .map((r) => `<tr><td>${r.level}</td><td>${r.pending}</td><td>${r.confirmed}</td><td>${r.rejected}</td></tr>`)
    .join("");
  $("#provenance").textContent =
    `数据目录：${v.data_dir}\n产出者：${v.producer_version}\nmanifest 生成：${v.manifest_at || "—"}\n许可：${v.license || "—"}`;
}

async function loadQueue() {
  if (!app()) return;
  queue = (await app().PendingList($("#filter").value || "", 200, 0)) || [];
  $("#queue-info").textContent = `本页 ${queue.length} 条（上限 200）`;
  $("#queue-list").innerHTML = queue
    .map(
      (p) => `<div class="item" data-id="${p.doc_id}">
        <div class="t">${p.title || p.doc_id}</div>
        <div class="m">${p.doc_id} · ${p.level} · ${p.issuer || "—"} · 条文 ${p.articles} · ${p.source_type || "—"}</div>
      </div>`
    )
    .join("");
  document.querySelectorAll(".item").forEach((el) =>
    el.addEventListener("click", () => {
      document.querySelectorAll(".item").forEach((x) => x.classList.toggle("sel", x === el));
      selected = el.dataset.id;
      showPreview(selected);
    })
  );
}

async function showPreview(docID) {
  const p = await app().Preview(docID);
  $("#preview").innerHTML = `
    <div class="meta">${p.doc_id} · ${p.confirm_status} · ${p.source_type || "—"}<br/>
      来源：<a href="#">${p.source_url || "—"}</a> · 公布日：${p.publish_date || "—"} · sha256 ${p.sha256.slice(0, 16)}…<br/>
      留证：${p.file_path}${p.truncated ? "（正文截断显示）" : ""}</div>
    ${p.missing ? `<div class="err">${p.missing}</div>` : ""}
    <pre>${escapeHTML(p.content)}</pre>
    <div class="log">审计历史：${(p.review_log || []).join("；") || "尚无流转"}</div>`;
}

function escapeHTML(s) {
  return (s || "").replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}

async function judge(verdict) {
  if (!selected) return toast("先点选左侧条目");
  const reviewer = $("#reviewer").value.trim();
  const reason = $("#reason").value.trim();
  try {
    const sum = await app().Judge([selected], verdict, reviewer, reason);
    toast(`落判完成：确认 ${sum.confirmed} 驳回 ${sum.rejected} 重置 ${sum.reset} 跳过 ${sum.skipped}`
      + (sum.failures && sum.failures.length ? ` 失败 ${sum.failures.length}：${sum.failures[0]}` : ""));
    await loadQueue();
    if (queue.some((p) => p.doc_id === selected)) showPreview(selected);
    else { selected = null; $("#preview").innerHTML = '<p class="muted">已裁决，移出队列。</p>'; }
  } catch (e) {
    toast("落判失败：" + (e.message || e));
  }
}

$("#btn-load").addEventListener("click", loadQueue);
$("#btn-confirm").addEventListener("click", () => judge("confirm"));
$("#btn-reject").addEventListener("click", () => judge("reject"));
$("#btn-reset").addEventListener("click", () => judge("reset"));

(function waitBindings() {
  if (app()) { loadStatus(); loadQueue(); return; }
  setTimeout(waitBindings, 100);
})();