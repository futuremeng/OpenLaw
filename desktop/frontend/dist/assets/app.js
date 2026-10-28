// app.js 「开源法律法规数据库」前端逻辑：
//   公众模式（默认）：查法规（搜索为中心）+ 按目录浏览（类别→法规→条款）；
//   专业模式：库总览 / 法规检索 / 人工复核 / 数据采集与更新。
// 全部数据经 api.js → Go 只读/受控写路径，前端不做任何裁决。
"use strict";

const $ = (s) => document.querySelector(s);
const num = (n) => (n == null ? "0" : Number(n).toLocaleString("zh-CN"));
function escapeHTML(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
let toastTimer = null;
function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove("show"), 3500);
}

/* ---------- 模式与导航 ---------- */
let mode = localStorage.getItem("ol-mode") || "public";
function showPanel(p) {
  document.querySelectorAll(".nav").forEach((x) => x.classList.toggle("active", x.dataset.panel === p));
  document.querySelectorAll("main section").forEach((s) => s.classList.toggle("hidden", s.id !== "panel-" + p));
  if (p === "home") loadHome();
  if (p === "browse") loadLaws();
  if (p === "overview") loadOverview();
  if (p === "confirm") { loadStats(); loadQueue(); }
  if (p === "collect") { loadSources(); loadTasks(); loadTasksTick(); }
}
function applyMode() {
  document.querySelectorAll(".nav").forEach((b) => (b.hidden = b.dataset.mode !== mode));
  $("#mode-public").classList.toggle("active", mode === "public");
  $("#mode-pro").classList.toggle("active", mode === "pro");
  showPanel(mode === "public" ? "home" : "overview");
}
document.querySelectorAll(".nav").forEach((b) => b.addEventListener("click", () => showPanel(b.dataset.panel)));
$("#mode-public").addEventListener("click", () => { mode = "public"; localStorage.setItem("ol-mode", mode); applyMode(); });
$("#mode-pro").addEventListener("click", () => { mode = "pro"; localStorage.setItem("ol-mode", mode); applyMode(); });

/* ---------- 共享组件：徽章 / 法规要素 / 预览 / 引用格式 ---------- */
const FLAG_TEXT = { inferred_date: "公布日期由文本推断", whole_text: "全文未分条", no_publish_date: "缺少公布日期" };
const KIND_TEXT = { "govcn-import": "官方原文获取", "govlist-browse": "官方规章目录抓取", "pack-build": "生成数据包", "pack-verify": "校验数据包", "pack-import": "导入数据包" };
const SRC_TEXT = { govcn: "国家行政法规库", govgz: "国家规章库", govlocal: "地方规章", govlist: "规章目录", batch: "批量导入" };

function statusBadge(s) {
  if (s === "confirmed") return '<span class="badge ok">已复核</span>';
  if (s === "rejected") return '<span class="badge no">已驳回</span>';
  return '<span class="badge pend">尚未复核</span>';
}
function validBadge(eff, exp) {
  if (exp) return `<span class="badge exp">已失效（${escapeHTML(exp)} 起）</span>`;
  return eff ? '<span class="badge ok">现行有效</span>' : "";
}
function metaLine(p) {
  const b = [];
  if (p.publish_date) b.push("公布 " + p.publish_date);
  if (p.effective_date) b.push("施行 " + p.effective_date);
  if (p.expire_date) b.push("失效 " + p.expire_date);
  if (p.issuer) b.push("发布机关：" + p.issuer);
  if (p.level) b.push(p.level);
  return b.join(" · ");
}
function citationOf(p, articleNo) {
  let s = "《" + (p.law_name || p.doc_id) + "》" + (articleNo || "");
  const n = [];
  if (p.publish_date) n.push(p.publish_date + " 公布");
  if (p.effective_date) n.push(p.effective_date + " 起施行");
  if (p.expire_date) n.push(p.expire_date + " 失效");
  if (p.version_no) n.push("版本：" + p.version_no);
  if (n.length) s += "（" + n.join("，") + "）";
  return s + "。摘自开源法律法规数据库（官方渠道抓取留证），引用前请核对官方原文。";
}
function legalHTML(p, articleNo) {
  return `<div class="pv">
    <h3>${escapeHTML(p.law_name || p.doc_id)} ${statusBadge(p.confirm_status)} ${validBadge(p.effective_date, p.expire_date)}</h3>
    <div class="meta">${escapeHTML(metaLine(p))}</div>
    <div class="meta">原文出处：<a href="${escapeHTML(p.source_url || "#")}" target="_blank" rel="noopener">${escapeHTML(p.source_url || "（无链接）")}</a> · 条目编号 ${escapeHTML(p.doc_id)}</div>
    ${p.confirm_status === "pending" ? '<div class="warnbox">⚠ 本条目尚未人工复核，引用前请核对官方原文。</div>' : ""}
    <button class="copycite" data-cite="${escapeHTML(citationOf(p, articleNo))}">复制引用格式</button>
    <pre>${escapeHTML(p.content)}</pre>
    ${p.truncated ? '<p class="muted">（内容较长，已截断显示）</p>' : ""}
    ${p.missing ? '<p class="bad">' + escapeHTML(p.missing) + "</p>" : ""}
  </div>`;
}
function bindCopy(el) {
  const b = el.querySelector(".copycite");
  if (b) b.addEventListener("click", () => navigator.clipboard.writeText(b.dataset.cite).then(() => toast("引用格式已复制到剪贴板")));
}
function openModal(p, articleNo) {
  const box = $("#modal-body");
  box.innerHTML = legalHTML(p, articleNo);
  bindCopy(box);
  $("#modal").classList.remove("hidden");
}
$("#modal-close").addEventListener("click", () => $("#modal").classList.add("hidden"));
$("#modal").addEventListener("click", (e) => { if (e.target.id === "modal") $("#modal").classList.add("hidden"); });


/* ---------- 公众 · 查法规（搜索为中心） ---------- */
async function loadHome() {
  try {
    const v = await app().Status();
    const levels = ["行政法规", "部门规章", "地方政府规章"];
    let html = levels.map((lv) => {
      const r = (v.by_level || []).find((x) => x.level === lv) || { pending: 0, confirmed: 0, rejected: 0 };
      return `<div class="card big"><b>${num(r.pending + r.confirmed + r.rejected)}</b><span>${lv}</span><span class="muted">已复核 ${num(r.confirmed)}</span></div>`;
    }).join("");
    const lf = Object.values(v.last_fetch || {}).sort().reverse()[0];
    if (lf) html += `<div class="card big"><b>${escapeHTML(lf.slice(0, 10))}</b><span>数据最近更新</span></div>`;
    $("#h-cards").innerHTML = html;
  } catch (e) { /* 首页卡片失败不阻塞搜索 */ }
}
async function homeSearch() {
  const q = $("#hq").value.trim();
  if (!q) return;
  $("#h-results").innerHTML = '<p class="muted">正在检索…</p>';
  try {
    const rows = (await app().SearchArticles(q, 20)) || [];
    $("#h-results").innerHTML = rows.length ? rows.map((r) => `
      <div class="lawcard" data-doc="${escapeHTML(r.doc_id)}">
        <div class="t">${escapeHTML(r.law_name)} <span class="pill">${escapeHTML(r.level)}</span> ${statusBadge(r.confirm_status)} ${validBadge(r.effective_date, r.expire_date)}</div>
        <div class="snip">${escapeHTML(r.snippet)}</div>
        <div class="m">${escapeHTML(r.article_no)}${r.effective_date ? " · " + r.effective_date + " 起施行" : ""} · <b class="view">查看原文 →</b></div>
      </div>`).join("") : '<p class="muted">没有找到相关条文，试试更短的关键词。</p>';
    document.querySelectorAll("#h-results .lawcard").forEach((el) =>
      el.addEventListener("click", async () => openModal(await app().Preview(el.dataset.doc))));
  } catch (e) {
    $("#h-results").innerHTML = '<p class="bad">检索失败：' + escapeHTML(e.message || e) + "</p>";
  }
}
$("#btn-hsearch").addEventListener("click", homeSearch);
$("#hq").addEventListener("keydown", (e) => { if (e.key === "Enter") homeSearch(); });

/* ---------- 公众 · 按目录浏览 ---------- */
async function loadLaws() {
  try {
    const rows = (await app().BrowseLaws($("#b-level").value, $("#b-kw").value.trim(), 300, 0)) || [];
    $("#law-list").innerHTML = rows.length ? rows.map((r) => `
      <div class="item" data-law="${escapeHTML(r.law_id)}" data-name="${escapeHTML(r.name)}">
        <div class="t">${escapeHTML(r.name)} ${statusBadge(r.status)} ${validBadge(r.effective_date, r.expire_date)}</div>
        <div class="m">${escapeHTML(r.level)} · ${escapeHTML(r.issuer || "发布机关未标注")}${r.effective_date ? " · " + r.effective_date + " 起施行" : ""}</div>
      </div>`).join("") : '<p class="muted">该类别下暂无收录法规。</p>';
    document.querySelectorAll("#law-list .item").forEach((el) =>
      el.addEventListener("click", () => openLaw(el.dataset.law, el.dataset.name)));
  } catch (e) {
    $("#law-list").innerHTML = '<p class="bad">读取失败：' + escapeHTML(e.message || e) + "</p>";
  }
}
async function openLaw(lawID, name) {
  document.querySelectorAll("#law-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.law === lawID));
  const vs = (await app().LawVersions(lawID)) || [];
  const arts = (await app().BrowseArticles(lawID)) || [];
  const tl = vs.map((x) => `<div class="tl-row"><b>${escapeHTML(x.version_no)}</b> ${escapeHTML(x.effective)} 起施行${x.expire ? "，" + escapeHTML(x.expire) + " 失效" : "，现行有效"} ${statusBadge(x.status)}</div>`).join("");
  const list = arts.map((x) => `<div class="item art" data-doc="${escapeHTML(x.doc_id)}" data-no="${escapeHTML(x.article_no)}"><div class="t">${escapeHTML(x.article_no)}</div><div class="m">${escapeHTML(x.snippet)}…</div></div>`).join("");
  $("#law-detail").innerHTML = `<h3>${escapeHTML(name)}</h3>
    <h4>修订沿革</h4><div class="tl">${tl || '<p class="muted">暂无版本记录。</p>'}</div>
    <h4>条款（${arts.length} 条）</h4>${list || '<p class="muted">该法规暂未收录分条文本（可在「查法规」中全文检索）。</p>'}`;
  document.querySelectorAll("#law-detail .art").forEach((el) =>
    el.addEventListener("click", async () => openModal(await app().Preview(el.dataset.doc), el.dataset.no)));
}
$("#btn-browse").addEventListener("click", loadLaws);
$("#b-level").addEventListener("change", loadLaws);
$("#b-kw").addEventListener("keydown", (e) => { if (e.key === "Enter") loadLaws(); });

/* ---------- 专业 · 库总览 ---------- */
async function loadOverview() {
  try {
    const v = await app().Status();
    $("#status-err").textContent = v.open_err || "";
    $("#schema-badge").textContent = "数据库版本 " + (v.schema_version || "?");
    const lf = Object.values(v.last_fetch || {}).sort().reverse()[0];
    $("#ov-hero").innerHTML = `
      <div class="card big"><b>${num(v.laws)}</b><span>收录法规</span></div>
      <div class="card big"><b>${num(v.confirmed)}</b><span>已复核可用</span></div>
      <div class="card big warn"><b>${num(v.pending)}</b><span>待人工复核</span></div>
      <div class="card big"><b>${escapeHTML(lf ? lf.slice(0, 10) : "—")}</b><span>数据最近更新</span></div>`;
    let acts = "";
    if (v.pending > 0) acts += `<span>${num(v.pending)} 条新条目等待人工复核</span><button data-go="confirm">去复核 →</button>`;
    acts += `<button data-go="collect">采集与更新 →</button>`;
    $("#ov-actions").innerHTML = acts;
    document.querySelectorAll("#ov-actions [data-go]").forEach((b) => b.addEventListener("click", () => showPanel(b.dataset.go)));
    $("#level-table tbody").innerHTML = (v.by_level || []).map((r) =>
      `<tr><td>${escapeHTML(r.level)}</td><td>${num(r.pending)}</td><td>${num(r.confirmed)}</td><td>${num(r.rejected)}</td></tr>`).join("");
    const reps = (await app().RecentReports(5)) || [];
    $("#reports").innerHTML = reps.map((r) => `<div class="rep"><b>${escapeHTML(SRC_TEXT[r.source] || r.source)}</b> ${escapeHTML(r.modified_at || "")}<br><span class="muted">成功 ${num(r.ok)} · 失败 ${num(r.failed)}${r.pages ? " · 页数 " + num(r.pages) : ""}</span></div>`).join("") || '<p class="muted">暂无采集记录。</p>';
    const revs = (await app().RecentReviews(5)) || [];
    $("#reviews").innerHTML = revs.map((r) => `<div class="rep">${escapeHTML(r.created_at || "")} · ${escapeHTML(r.reviewer)} <span class="muted">${escapeHTML(r.action === "confirm" ? "确认" : r.action === "reject" ? "驳回" : "重新复核")} ${escapeHTML(r.doc_id)}</span></div>`).join("") || '<p class="muted">暂无复核记录。</p>';
  } catch (e) {
    $("#status-err").textContent = "读取失败：" + (e.message || e);
  }
}


/* ---------- 专业 · 法规检索 ---------- */
$("#search-preview").insertAdjacentHTML("afterbegin", '<div id="sp-slot"></div>');
async function doSearch() {
  const q = $("#q").value.trim();
  if (!q) return;
  $("#search-info").textContent = "检索中…";
  try {
    let rows = (await app().SearchArticles(q, 50)) || [];
    const lv = $("#s-level").value;
    if (lv) rows = rows.filter((r) => r.level === lv);
    if ($("#s-confirmed").checked) rows = rows.filter((r) => r.confirm_status === "confirmed");
    $("#search-info").textContent = `命中 ${rows.length} 条（全文检索，未重排序）`;
    $("#search-list").innerHTML = rows.map((r) => `
      <div class="item" data-doc="${escapeHTML(r.doc_id)}" data-no="${escapeHTML(r.article_no)}" data-law="${escapeHTML(r.law_name)}">
        <div class="t">${escapeHTML(r.law_name)} <span class="pill">${escapeHTML(r.level)}</span> ${statusBadge(r.confirm_status)} ${validBadge(r.effective_date, r.expire_date)}</div>
        <div class="m">${escapeHTML(r.article_no)}${r.effective_date ? " · " + r.effective_date + " 起施行" : ""}</div>
        <div class="m">${escapeHTML(r.snippet)}</div>
      </div>`).join("") || '<p class="muted">无命中。</p>';
    document.querySelectorAll("#search-list .item").forEach((el) =>
      el.addEventListener("click", () => showSearchPreview(el.dataset.doc, el.dataset.no, el.dataset.law)));
  } catch (e) {
    $("#search-info").textContent = "检索失败：" + (e.message || e);
  }
}
async function showSearchPreview(docID, articleNo, lawName) {
  document.querySelectorAll("#search-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.doc === docID));
  const p = await app().Preview(docID);
  const slot = $("#sp-slot");
  slot.innerHTML = legalHTML(p, articleNo);
  bindCopy(slot);
  if (p.law_id) {
    const vs = (await app().LawVersions(p.law_id)) || [];
    if (vs.length > 1) {
      slot.insertAdjacentHTML("beforeend", `<h4>修订沿革</h4><div class="tl">` + vs.map((x) =>
        `<div class="tl-row"><b>${escapeHTML(x.version_no)}</b> ${escapeHTML(x.effective)} 起施行${x.expire ? "，" + escapeHTML(x.expire) + " 失效" : "，现行有效"} ${statusBadge(x.status)}</div>`).join("") + "</div>");
    }
  }
  if (lawName && !$("#c-law").value) $("#c-law").value = lawName;
  if (articleNo && !$("#c-art").value) $("#c-art").value = articleNo;
  slot.scrollIntoView({ block: "start" });
}
$("#btn-search").addEventListener("click", doSearch);
$("#q").addEventListener("keydown", (e) => { if (e.key === "Enter") doSearch(); });
async function doCite() {
  $("#cite-out").textContent = "核对中…";
  try {
    const c = await app().CheckCitation($("#c-law").value.trim(), $("#c-art").value.trim(), $("#c-quote").value, $("#c-date").value.trim());
    if (!c.hit) {
      $("#cite-out").textContent = "未找到对应条文" + (c.reason ? "：" + c.reason : "") + "。请核对法规名称、条款号与引用文本。";
      return;
    }
    let out = `命中：与留证原文相似度 ${(c.similarity * 100).toFixed(1)}%（条目 ${c.doc_id}，版本 ${c.version_no}，${c.confirm_status === "confirmed" ? "已复核" : "尚未复核"}）。`;
    if (c.expired) out += ` 注意：该版本已失效，现行版本为「${c.current_version}」，请核实现行条文。`;
    if (c.chain_issues && c.chain_issues.length) out += " 来源链提示：" + c.chain_issues.map((i) => i.code).join("、") + "。";
    out += " 以下为留证原文，供逐字比对：\n" + (c.content || "");
    $("#cite-out").textContent = out;
  } catch (e) {
    $("#cite-out").textContent = "核对失败：" + (e.message || e);
  }
}
$("#btn-cite").addEventListener("click", doCite);


/* ---------- 专业 · 人工复核 ---------- */
let queue = [];
let selectedDoc = null;
async function loadStats() {
  try {
    const st = await app().QueueStats();
    const risk = Object.entries(st.risk || {}).map(([k, n]) => `<span class="pill">${escapeHTML(FLAG_TEXT[k] || k)} ${num(n)}</span>`).join("");
    $("#qstats").innerHTML = `<span>待复核 <b>${num(st.pending)}</b></span><span>已复核确认 <b>${num(st.confirmed)}</b></span><span>已驳回 <b>${num(st.rejected)}</b></span><span class="muted">来源提示：</span>${risk || '<span class="muted">无</span>'}`;
  } catch (e) { $("#qstats").textContent = "统计读取失败：" + (e.message || e); }
}
async function loadQueue() {
  const f = $("#filter").value.trim();
  try {
    queue = (await app().PendingList(f, 300, 0)) || [];
    $("#queue-info").textContent = `队列 ${queue.length} 条（最多显示 300）`;
    $("#queue-list").innerHTML = queue.map((r) => `
      <div class="item ${r.doc_id === selectedDoc ? "sel" : ""}" data-doc="${escapeHTML(r.doc_id)}">
        <div class="t">${escapeHTML(r.law_name || r.doc_id)} ${statusBadge("pending")}</div>
        <div class="m">${escapeHTML(r.doc_id)} · ${escapeHTML(r.source_type || "")}${r.publish_date ? " · 公布 " + r.publish_date : ""}</div>
        <div class="m">${(r.flags || []).map((x) => `<span class="pill warn">${escapeHTML(FLAG_TEXT[x] || x)}</span>`).join(" ")}</div>
      </div>`).join("") || '<p class="muted">队列为空（或过滤无匹配）。</p>';
    document.querySelectorAll("#queue-list .item").forEach((el) => el.addEventListener("click", () => showPreview(el.dataset.doc)));
  } catch (e) { $("#queue-list").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
async function showPreview(docID) {
  selectedDoc = docID;
  document.querySelectorAll("#queue-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.doc === docID));
  const p = await app().Preview(docID);
  const el = $("#preview");
  el.innerHTML = legalHTML(p) + `<h4>复核记录</h4><ul class="log">${(p.review_log || []).map((x) => "<li>" + escapeHTML(x) + "</li>").join("") || '<li class="muted">暂无</li>'}</ul>`;
  bindCopy(el);
}
async function judge(action) {
  if (!selectedDoc) return toast("请先在左侧点选条目");
  const reviewer = $("#reviewer").value.trim(), reason = $("#reason").value.trim();
  try {
    const sum = await app().Judge([selectedDoc], action, reviewer, reason);
    toast(`已处理：确认 ${sum.confirmed} · 驳回 ${sum.rejected} · 重新复核 ${sum.reset}`);
    loadStats(); loadQueue();
  } catch (e) { toast("操作失败：" + (e.message || e)); }
}
$("#btn-confirm").addEventListener("click", () => judge("confirm"));
$("#btn-reject").addEventListener("click", () => judge("reject"));
$("#btn-reset").addEventListener("click", () => judge("reset"));
$("#btn-load").addEventListener("click", () => { loadStats(); loadQueue(); });


/* ---------- 专业 · 人工复核 · 批量 ---------- */
async function bulkPreview() {
  try {
    const n = await app().BulkPreview($("#b-source").value.trim(), $("#b-level2").value.trim(), $("#b-verdict").value);
    $("#bulk-info").textContent = `影响范围：${num(n)} 条`;
  } catch (e) { $("#bulk-info").textContent = "失败：" + (e.message || e); }
}
async function bulkApply() {
  const reviewer = $("#reviewer").value.trim(), reason = $("#reason").value.trim();
  if (!reviewer || !reason) return toast("批量复核须填写复核人与结论依据");
  try {
    const sum = await app().BulkApply($("#b-source").value.trim(), $("#b-level2").value.trim(), $("#b-verdict").value, reviewer, reason);
    toast(`批量完成：确认 ${num(sum.confirmed)} · 驳回 ${num(sum.rejected)} · 重新复核 ${num(sum.reset)}`);
    loadStats(); loadQueue();
  } catch (e) { toast("批量失败：" + (e.message || e)); }
}
$("#btn-bulk-preview").addEventListener("click", bulkPreview);
$("#btn-bulk-apply").addEventListener("click", bulkApply);

/* ---------- 专业 · 数据采集与更新 ---------- */
async function loadSources() {
  try {
    const v = await app().Status();
    const lf = v.last_fetch || {};
    const srcs = [["govcn", "国家行政法规库"], ["govgz", "国家规章库"], ["govlocal", "地方规章"]];
    $("#src-cards").innerHTML = srcs.map((s) => `<div class="card"><b>${escapeHTML(lf[s[0]] ? lf[s[0]].slice(0, 10) : "尚未抓取")}</b><span>${s[1]}</span></div>`).join("");
    const bs = await app().BrowserStatus();
    $("#browser-hint").textContent = bs.enabled ? "BrowserOS 已连接，可抓取官方规章目录。" : (bs.guidance || "BrowserOS 未配置，目录抓取不可用。");
    const lists = (await app().GovcnLists()) || [];
    $("#g-list").innerHTML = lists.map((l) => `<option value="${escapeHTML(l.path)}">${escapeHTML(l.name || l.path)}</option>`).join("") || '<option value="">（未找到已导出清单文件）</option>';
  } catch (e) { $("#src-cards").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
let taskTimer = null;
function renderTasks(tasks) {
  const st = { running: "进行中", done: "完成", failed: "失败", cancelled: "已取消" };
  $("#task-list").innerHTML = (tasks || []).map((t) => `
    <div class="task ${escapeHTML(t.state)}"><b>${escapeHTML(KIND_TEXT[t.kind] || t.kind)} #${t.id}</b><span class="state">${st[t.state] || escapeHTML(t.state)}</span>
    <div class="m">${escapeHTML(t.progress || t.detail || "")}</div>
    <div class="m">${escapeHTML(t.started_at)}${t.ended_at ? " → " + escapeHTML(t.ended_at) : ""} · ${escapeHTML(t.params)}</div></div>`).join("") || '<p class="muted">暂无任务记录。</p>';
}
async function loadTasks() { try { renderTasks(await app().TaskList()); } catch (e) { /* 绑定未就绪 */ } }
function loadTasksTick() {
  if (taskTimer) return;
  taskTimer = setInterval(loadTasks, 1500);
}
$("#btn-cancel-task").addEventListener("click", async () => toast((await app().CancelTask()) ? "已请求取消，已获取部分将如实保留" : "当前没有进行中的任务"));
$("#btn-govcn").addEventListener("click", async () => {
  try {
    const info = await app().StartGovcnTask({
      source: $("#g-source").value, list_path: $("#g-list").value,
      delay_sec: Number($("#g-delay").value) || 2, limit: Number($("#g-limit").value) || 0,
      dry_run: $("#g-dry").checked, local: $("#g-local").checked
    });
    toast(`任务 #${info.id} 已开始`); loadTasks();
  } catch (e) { toast("无法开始：" + (e.message || e)); }
});
$("#btn-govlist").addEventListener("click", async () => {
  try {
    const info = await app().StartGovListTask({
      cat: $("#l-cat").value, max_pages: Number($("#l-pages").value) || 10,
      start_page: Number($("#l-start").value) || 1, delay_ms: Number($("#l-delay").value) || 2500,
      empty_stop: Number($("#l-empty").value) || 10
    });
    toast(`任务 #${info.id} 已开始`); loadTasks();
  } catch (e) { toast("无法开始：" + (e.message || e)); }
});


/* ---------- 专业 · 高级设置（自检 / 完整性档案 / 离线数据包） ---------- */
async function loadManifest() {
  try {
    const m = await app().ManifestStatus();
    $("#manifest-box").innerHTML = m.exists
      ? `<span>完整性档案：${m.verify_err ? "校验未通过 — " + escapeHTML(m.verify_err) : "校验通过 ✓"}</span>
         <span>生成时间 ${escapeHTML(m.generated_at)} · 文件 ${num(m.files)} 个</span>
         <span>来源版本 ${escapeHTML(m.producer_version || "未知")} · 数据库版本 ${escapeHTML(m.schema_version || "?")} · 许可 ${escapeHTML(m.license || "—")}</span>`
      : '<span class="bad">完整性档案缺失</span><span>尚未生成离线数据包（CLI：openlaw-pack build），数据自检仅做留证完整性扫描。</span>';
  } catch (e) { $("#manifest-box").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
$("#btn-check").addEventListener("click", async () => {
  $("#check-info").textContent = "自检中（只读，可能需要十几秒）…";
  try {
    const c = await app().CorpusCheck();
    loadManifest();
    const rows = (c.broken || []).map((b) => `<div class="rep bad">${escapeHTML(b.doc_id || "")} ${escapeHTML(b.file_path || "")} ${escapeHTML(b.reason || "")}</div>`).join("");
    $("#check-out").innerHTML = `<div class="rep">${c.broken_count ? "留证完整性问题 " + num(c.broken_count) + " 条" : "留证文件全部完整 ✓"}${c.truncated ? "（明细仅显示前 100 条）" : ""}</div>` + rows;
    $("#check-info").textContent = "自检完成";
  } catch (e) { $("#check-info").textContent = "自检失败：" + (e.message || e); }
});
$("#btn-pack-build").addEventListener("click", async () => {
  try { const t = await app().StartPackBuildTask($("#p-dir").value.trim(), $("#p-key").value.trim(), $("#p-license").value.trim(), $("#p-attrib").value.trim()); toast(`数据包生成 #${t.id} 已开始`); loadTasks(); } catch (e) { toast("无法开始：" + (e.message || e)); }
});
$("#btn-pack-verify").addEventListener("click", async () => {
  const out = await app().PackVerify($("#p-dir").value.trim());
  toast("校验结果：" + out.slice(0, 120));
});
$("#btn-pack-import").addEventListener("click", async () => {
  try { const t = await app().StartPackImportTask($("#p-dir").value.trim()); toast(`数据包导入 #${t.id} 已开始（新条目进入待复核）`); loadTasks(); } catch (e) { toast("无法开始：" + (e.message || e)); }
});
$("#btn-risk-reload").addEventListener("click", async () => { await app().ReloadRiskCache(); toast("来源提示缓存已重载"); });
async function loadSettings() {
  try {
    const v = await app().Status();
    $("#settings-box").innerHTML = `<span>数据目录：${escapeHTML(v.data_dir)}</span>
      <span>数据库版本 ${escapeHTML(v.schema_version || "?")}（本程序支持 ${escapeHTML(v.schema_min)}–${escapeHTML(v.schema_max)}）</span>
      <span>数据来源版本：${escapeHTML(v.producer_version || "未知")}</span>`;
  } catch (e) { /* 已在别处报错 */ }
}

/* ---------- 启动 ---------- */
document.addEventListener("DOMContentLoaded", () => {
  loadManifest(); loadSettings();
  waitBindings(() => applyMode());
});

