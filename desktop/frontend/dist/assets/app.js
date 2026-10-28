// app.js 面板逻辑：语料状态 / 条文检索 / 人工确认（M1）。
// 全部调用 api.js 的绑定；写操作只有 Judge/BulkApply，且库层强制署名审计。
"use strict";

/* ---------- 导航 ---------- */
document.querySelectorAll(".nav:not(.off)").forEach((b) =>
  b.addEventListener("click", () => {
    document.querySelectorAll(".nav").forEach((x) => x.classList.toggle("active", x === b));
    for (const p of ["status", "search", "confirm", "tasks"]) {
      $("#panel-" + p).classList.toggle("hidden", p !== b.dataset.panel);
    }
    if (b.dataset.panel === "status") loadStatus();
    if (b.dataset.panel === "confirm") { loadQueue(); loadStats(); }
    if (b.dataset.panel === "tasks") { loadTasks(); loadTaskInit(); }
  })
);

/* ---------- 面板一：语料状态 ---------- */
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
    .map(([k, n]) => `<div class="card"><b>${num(n)}</b><span>${k}</span></div>`)
    .join("");
  $("#level-table tbody").innerHTML = (v.by_level || [])
    .map((r) => `<tr><td>${r.level}</td><td>${num(r.pending)}</td><td>${num(r.confirmed)}</td><td>${num(r.rejected)}</td></tr>`)
    .join("");
  $("#provenance").textContent =
    `数据目录：${v.data_dir}\n产出者：${v.producer_version}\nmanifest 生成：${v.manifest_at || "—"}\n许可：${v.license || "—"}`;
  await loadReports();
  await loadReviews();
}

async function loadReports() {
  const reps = (await app().RecentReports()) || [];
  $("#reports").innerHTML = reps.length ? reps.map((r) => {
    const fails = (r.top_failures || [])
      .map((f) => `<span class="pill">${escapeHTML(f.reason)} ×${f.count}</span>`).join("");
    const cls = r.failed > 0 ? " hasfail" : "";
    return `<div class="rep${cls}">
      <div class="rep-h"><b>${r.source}</b> <span class="muted">${escapeHTML((r.path || "").split("/").pop())} · ${r.modified_at || ""}</span></div>
      <div class="rep-n">共 ${num(r.total)} · 成功 ${num(r.ok)} · <b class="${r.failed ? "bad" : ""}">失败 ${num(r.failed)}</b> · 跳过 ${num(r.skipped)}${r.other ? ` · 其他 ${r.other}` : ""}</div>
      ${fails ? `<div class="rep-f">${fails}</div>` : ""}
      ${r.note ? `<div class="muted">${escapeHTML(r.note)}</div>` : ""}
    </div>`;
  }).join("") : '<p class="muted">尚无 incoming/ 报告。</p>';
}

async function loadReviews() {
  const revs = (await app().RecentReviews(15)) || [];
  $("#reviews").innerHTML = revs.length ? revs.map((r) =>
    `<div class="rev"><b>${escapeHTML(r.law_name || r.doc_id)}</b> ${r.action} ${r.from_status}→${r.to_status}
      <span class="muted">by ${escapeHTML(r.reviewer)}：${escapeHTML(r.reason)} @${r.created_at}</span></div>`).join("")
    : '<p class="muted">尚无审计流转。</p>';
}

/* ---------- 面板二：条文检索 ---------- */
async function doSearch() {
  const q = $("#q").value.trim();
  if (!q) return toast("输入检索词");
  const rows = (await app().SearchArticles(q, 50)) || [];
  $("#search-info").textContent = `命中 ${rows.length} 条（FTS5 原样透出，无重排）`;
  $("#search-list").innerHTML = rows.map((r) =>
    `<div class="item" data-doc="${escapeHTML(r.doc_id)}" data-law="${escapeHTML(r.law_name)}" data-art="${escapeHTML(r.article_no)}">
      <div class="t">${escapeHTML(r.law_name || r.doc_id)} · ${escapeHTML(r.article_no)}</div>
      <div class="m">${escapeHTML(r.snippet)}</div>
      <div class="m">${escapeHTML(r.doc_id)} · ${escapeHTML(r.level || "")} ${escapeHTML(r.version_no || "")}</div>
    </div>`).join("") || '<p class="muted" style="padding:10px">无命中。</p>';
  document.querySelectorAll("#search-list .item").forEach((el) =>
    el.addEventListener("click", async () => {
      document.querySelectorAll("#search-list .item").forEach((x) => x.classList.toggle("sel", x === el));
      if (!$("#c-law").value) { $("#c-law").value = el.dataset.law || ""; $("#c-art").value = el.dataset.art || ""; }
      renderPreview(await app().Preview(el.dataset.doc), $("#search-preview"));
    })
  );
}

async function doCite() {
  const out = $("#cite-out");
  out.textContent = "核验中…";
  const c = await app().CheckCitation($("#c-law").value.trim(), $("#c-art").value.trim(),
    $("#c-quote").value, $("#c-date").value.trim());
  const issues = (c.chain_issues || []).map((i) => (i.step || "") + " " + (i.detail || "")).join("；");
  out.textContent = [
    c.hit ? `✓ 命中（相似度 ${(c.similarity * 100).toFixed(1)}%）` : `✗ 未命中${c.error_code ? " [" + c.error_code + "]" : ""}`,
    c.reason ? "原因：" + c.reason : "",
    c.version_no ? `命中版本：${c.version_no}（doc ${c.doc_id}）` : "",
    c.expired ? `⚠ 该版本已失效；当前有效版本：${c.current_version}` : "",
    c.confirm_status && c.confirm_status !== "confirmed" ? `⚠ 确认状态：${c.confirm_status}（待人工确认）` : "",
    issues ? "断链问题：" + issues : "",
    c.content ? "原文：" + c.content : "",
  ].filter(Boolean).join("\n");
}

/* ---------- 预览（检索/确认共用） ---------- */
function renderPreview(p, target) {
  let box = target.querySelector(".pv");
  if (!box) { box = document.createElement("div"); box.className = "pv"; target.prepend(box); }
  box.innerHTML = `
    <div class="meta">${escapeHTML(p.doc_id)} · ${escapeHTML(p.confirm_status)} · ${escapeHTML(p.source_type || "—")}<br/>
      来源：${escapeHTML(p.source_url || "—")} · 公布日：${escapeHTML(p.publish_date || "—")} · sha256 ${escapeHTML((p.sha256 || "").slice(0, 16))}…<br/>
      留证：${escapeHTML(p.file_path)}${p.truncated ? "（正文截断显示）" : ""}</div>
    ${p.missing ? `<div class="err">${escapeHTML(p.missing)}</div>` : ""}
    <pre>${escapeHTML(p.content)}</pre>
    <div class="log">审计历史：${(p.review_log || []).map(escapeHTML).join("；") || "尚无流转"}</div>`;
}

/* ---------- 面板三：人工确认 ---------- */
let queue = [];
let selected = null;

async function loadStats() {
  const st = await app().QueueStats();
  if (!st) return;
  $("#qstats").innerHTML = `
    <span>文档 ${num(st.total)}</span><span class="bad">pending ${num(st.pending)}</span>
    <span class="ok">confirmed ${num(st.confirmed)}</span><span>rejected ${num(st.rejected)}</span>
    <span class="muted">风险面：日期推断 ${st.risk.inferred_date} · 全文即一节 ${st.risk.whole_text} · 缺公布日 ${st.risk.no_publish_date} · 审计流转 ${st.reviewed}</span>`;
}

async function loadQueue() {
  queue = (await app().PendingList($("#filter").value || "", 200, 0)) || [];
  $("#queue-info").textContent = `本页 ${queue.length} 条（上限 200）`;
  $("#queue-list").innerHTML = queue.map((p) => {
    const flags = (p.flags || []).map((f) => `<span class="pill warn">${escapeHTML(f)}</span>`).join("");
    return `<div class="item" data-id="${escapeHTML(p.doc_id)}">
      <div class="t">${escapeHTML(p.title || p.doc_id)} ${flags}</div>
      <div class="m">${escapeHTML(p.doc_id)} · ${escapeHTML(p.level)} · ${escapeHTML(p.issuer || "—")} · 条文 ${p.articles} · ${escapeHTML(p.source_type || "—")}</div>
    </div>`;
  }).join("");
  document.querySelectorAll("#queue-list .item").forEach((el) =>
    el.addEventListener("click", async () => {
      document.querySelectorAll("#queue-list .item").forEach((x) => x.classList.toggle("sel", x === el));
      selected = el.dataset.id;
      renderPreview(await app().Preview(selected), $("#preview"));
    })
  );
}

async function judge(verdict) {
  if (!selected) return toast("先点选左侧条目");
  const reviewer = $("#reviewer").value.trim();
  const reason = $("#reason").value.trim();
  try {
    const sum = await app().Judge([selected], verdict, reviewer, reason);
    toastSum(`落判完成`, sum);
    await loadQueue(); await loadStats();
    if (queue.some((p) => p.doc_id === selected)) renderPreview(await app().Preview(selected), $("#preview"));
    else { selected = null; const b = $("#preview .pv"); if (b) b.remove(); }
  } catch (e) { toast("落判失败：" + (e.message || e)); }
}

function toastSum(prefix, sum) {
  if (!sum) return toast(prefix + " 失败");
  toast(`${prefix}：确认 ${sum.confirmed} 驳回 ${sum.rejected} 重置 ${sum.reset} 跳过 ${sum.skipped}`
    + (sum.failures && sum.failures.length ? `；失败 ${sum.failures.length} 条（首条：${sum.failures[0]}）` : ""));
}

let bulkMatched = -1;
async function bulkPreview() {
  bulkMatched = await app().BulkPreview($("#b-source").value.trim(), $("#b-level").value.trim(), $("#b-verdict").value);
  $("#bulk-info").textContent = `dry-run 命中 ${bulkMatched} 条（未写库）`;
}

async function bulkApply() {
  if (bulkMatched < 0) return toast("先 dry-run 计数，看清命中再执行");
  if (!confirm(`确认对 ${bulkMatched} 条执行 ${$("#b-verdict").value}？署名与依据将写入 review_log。`)) return;
  try {
    const sum = await app().BulkApply($("#b-source").value.trim(), $("#b-level").value.trim(),
      $("#b-verdict").value, $("#reviewer").value.trim(), $("#reason").value.trim());
    toastSum("批量落判", sum);
    bulkMatched = -1; $("#bulk-info").textContent = "";
    await loadQueue(); await loadStats();
  } catch (e) { toast("批量失败：" + (e.message || e)); }
}

/* ---------- 面板四：采集任务 ---------- */
async function loadTasks() {
  const ts = (await app().TaskList()) || [];
  $("#task-list").innerHTML = ts.length
    ? ts.map((t) => `<div class="task ${t.state}">
        <b>#${t.id} ${escapeHTML(t.kind)}</b> <span class="pill st-${t.state}">${t.state}</span>
        <div class="m">${escapeHTML(t.params)} · 起 ${t.started_at}${t.ended_at ? " · 止 " + t.ended_at : ""}</div>
        ${t.progress ? `<div class="muted">进度：${escapeHTML(t.progress)}</div>` : ""}
        ${t.detail ? `<div class="bad">错误：${escapeHTML(t.detail)}</div>` : ""}
      </div>`).join("")
    : '<p class="muted">尚无任务。</p>';
}

async function loadTaskInit() {
  const st = await app().BrowserStatus();
  $("#browser-status").innerHTML = st.enabled
    ? '<span class="ok">BrowserOS neo 通道已配置（OPENLAW_BROWSEROS_ENDPOINT）</span>'
    : `<span class="bad">BrowserOS 未配置/不可用</span><span class="muted">${escapeHTML(st.guidance)}</span>`;
  const lists = (await app().GovcnLists()) || [];
  $("#g-list").innerHTML = lists.length
    ? lists.map((l) => `<option value="${escapeHTML(l.path)}">${escapeHTML(l.name)} · ${l.mtime}</option>`).join("")
    : '<option value="">（未找到清单——先用浏览器脚本人工导出 list.json）</option>';
}

async function startGovcnTask() {
  try {
    const info = await app().StartGovcnTask({
      source: $("#g-source").value, list_path: $("#g-list").value,
      delay_sec: +$("#g-delay").value, limit: +$("#g-limit").value,
      dry_run: $("#g-dry").checked, local: $("#g-local").checked,
    });
    toast(`任务已提交 #${info.id} ${info.kind}（${info.params}）`);
    loadTasks();
  } catch (e) { toast("提交被拒：" + (e.message || e)); }
}

async function startGovListTask() {
  try {
    const info = await app().StartGovListTask({
      cat: $("#l-cat").value, max_pages: +$("#l-pages").value, start_page: +$("#l-start").value,
      delay_ms: +$("#l-delay").value, empty_stop: +$("#l-empty").value,
    });
    toast(`任务已提交 #${info.id} ${info.kind}（${info.params}）`);
    loadTasks();
  } catch (e) { toast("提交被拒：" + (e.message || e)); }
}

/* ---------- 绑定与启动 ---------- */
$("#btn-search").addEventListener("click", doSearch);
$("#q").addEventListener("keydown", (e) => { if (e.key === "Enter") doSearch(); });
$("#btn-cite").addEventListener("click", doCite);
$("#btn-load").addEventListener("click", loadQueue);
$("#btn-confirm").addEventListener("click", () => judge("confirm"));
$("#btn-reject").addEventListener("click", () => judge("reject"));
$("#btn-reset").addEventListener("click", () => judge("reset"));
$("#btn-bulk-preview").addEventListener("click", bulkPreview);
$("#btn-bulk-apply").addEventListener("click", bulkApply);
$("#btn-govcn").addEventListener("click", startGovcnTask);
$("#btn-govlist").addEventListener("click", startGovListTask);
$("#btn-cancel-task").addEventListener("click", async () => {
  toast((await app().CancelTask()) ? "已请求取消（在限速点安全停下，已采部分如实保留）" : "当前没有运行中的任务");
});

waitBindings(() => {
  loadStatus();
  // 任务进度经 Wails 事件推送；事件不可用时（如浏览器直开）3s 轮询兜底。
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn("task", () => loadTasks());
  } else {
    setInterval(() => { if (!$("#panel-tasks").classList.contains("hidden")) loadTasks(); }, 3000);
  }
});