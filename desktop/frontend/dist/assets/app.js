// app.js 「开源法律法规数据库」前端逻辑：
//   公众模式（默认）：查法规（搜索为中心）+ 按条件筛选（维度组合→法规→条款）；
//   专业模式：库总览 / 法规检索 / 按条件筛选（多维收窄）/ 智能审核（双智能体自审，
//   人工只处理例外纠正）/ 获取最新法规 / 地域标引。
// 三栏布局：左列导航与筛选栏、中列面板与列表、右列共享详情（原文/条款/例外纠正）。
// 三栏布局：左列导航与筛选、中列面板列表、右列共享详情。
// 全部数据经 api.js → Go 只读/受控写路径，前端不做任何裁决。
// 用户可见文案遵守 docs/design-ui-language.md（目标语言、渐进披露、诚实展示）。
"use strict";

const $ = (s) => document.querySelector(s);
const num = (n) => (n == null ? "0" : Number(n).toLocaleString("zh-CN"));
// compact：侧栏计数徽标用的紧凑写法（3.6 万）。精确值一律挂在 title 里，
// 徽标只做「这里需要你」的定位线索，不作为结论展示（诚实展示纪律）。
function compact(n) {
  n = Number(n) || 0;
  if (n < 10000) return num(n);
  const w = n / 10000;
  return (w >= 100 ? Math.round(w) : Math.round(w * 10) / 10) + " 万";
}
/* ---------- 图标：16px 线性、跟随文字色 ----------
   图标只承载识别（这是导航、这是分面、这是原文），结论一律仍是文字与数字——
   诚实展示纪律不因引入图标而松动。HTML 里写 data-icon，SVG 在此集中注入，
   界面文件不堆图标源码，也保证深浅色下自动跟随前景色。 */
const ICON_PATHS = {
  home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5.5 9.5V20h13V9.5"/>',
  search: '<circle cx="11" cy="11" r="6.5"/><path d="M16 16l4.5 4.5"/>',
  filter: '<path d="M4 7h16M7 12h10M10 17h4"/>',
  list: '<path d="M4 6h16M4 12h16M4 18h16"/>',
  chart: '<path d="M4 20V10M10 20V4M16 20v-7M21 20H3"/>',
  file: '<path d="M6.5 3H15l3.5 3.5V21h-12z"/><path d="M14.5 3v4.5H19"/>',
  shield: '<path d="M12 3l7 3v6c0 4.4-2.9 7.4-7 9-4.1-1.6-7-4.6-7-9V6z"/><path d="M9 12l2 2 4-4"/>',
  pin: '<path d="M12 21s7-6.4 7-11a7 7 0 1 0-14 0c0 4.6 7 11 7 11z"/><circle cx="12" cy="10" r="2.4"/>',
  download: '<path d="M12 4v10M8 10.5l4 4 4-4"/><path d="M4.5 19h15"/>',
  wrench: '<path d="M14.5 4.5a4.5 4.5 0 0 0 5 5l-9 9a3.5 3.5 0 0 1-5-5z"/><path d="M18 4.5l1.5 1.5"/>',
  db: '<ellipse cx="12" cy="6.5" rx="7" ry="3"/><path d="M5 6.5v11c0 1.7 3.1 3 7 3s7-1.3 7-3v-11"/>',
  alert: '<path d="M12 4l8.5 16h-17z"/><path d="M12 10v4M12 17h.01"/>',
  refresh: '<path d="M20 12a8 8 0 1 1-2.9-6.2"/><path d="M20 4.5V9h-4.5"/>',
  chevron: '<path d="M6 9.5l6 6 6-6"/>',
  rows: '<path d="M4 6.5h16M4 12h16M4 17.5h16"/>',
  wide: '<path d="M4 5v14M20 5v14M8 12h8"/><path d="M11 9l-3 3 3 3M13 9l3 3-3 3"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M18.7 5.3l-1.4 1.4M6.7 17.3l-1.4 1.4"/>',
  moon: '<path d="M20 14.5A8.5 8.5 0 1 1 9.5 4a7 7 0 0 0 10.5 10.5z"/>',
  auto: '<circle cx="12" cy="12" r="8"/><path d="M12 4a8 8 0 0 1 0 16z"/>',
};
function icon(name, cls) {
  const d = ICON_PATHS[name];
  return d ? '<svg class="ic ' + (cls || "") + '" viewBox="0 0 24 24" aria-hidden="true">' + d + "</svg>" : "";
}
// applyIcons：把 data-icon 声明换成 SVG。已注入过的元素跳过（分面重渲染时会被清掉重建）。
function applyIcons(root) {
  (root || document).querySelectorAll("[data-icon]").forEach((el) => {
    if (el.firstElementChild && el.firstElementChild.classList.contains("ic")) return;
    el.insertAdjacentHTML("afterbegin", icon(el.dataset.icon));
  });
}
// 外观：跟随系统 / 浅色 / 深色。手动选择写入 html[data-theme]，样式层据此覆盖
// prefers-color-scheme（docs/design-ui-language.md：安静克制，深色与浅色同一套语义令牌）。
// 顶栏只放一个图标按钮：图标随当前外观，白话说明挂在 title 里。
let theme = localStorage.getItem("ol-theme") || "system";
const THEME_TEXT = { system: "外观 · 跟随系统", light: "外观 · 浅色", dark: "外观 · 深色" };
function applyTheme() {
  if (theme === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
  const b = $("#theme-toggle");
  if (b) {
    b.title = THEME_TEXT[theme] || THEME_TEXT.system;
    b.innerHTML = icon(theme === "light" ? "sun" : theme === "dark" ? "moon" : "auto");
  }
}
function cycleTheme() {
  theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
  localStorage.setItem("ol-theme", theme);
  applyTheme();
}
// 密度与阅读：一屏能看多少条目、原文读起来多大字号，都由用户定，选择持久化。
// 这是显示层的事，绝不影响数据口径（列表取列条件与分面计数都不因此变化）。
let density = localStorage.getItem("ol-density") || "comfortable";
function applyDensity() {
  if (density === "compact") document.documentElement.dataset.density = "compact";
  else delete document.documentElement.dataset.density;
  const b = $("#t-density");
  if (!b) return;
  b.setAttribute("aria-pressed", density === "compact" ? "true" : "false");
  b.title = density === "compact" ? "紧凑模式已开启：点击恢复常规密度" : "紧凑模式：收小行高与间距，一屏看更多条目";
}
const FS_STEPS = [13.5, 14.5, 15.5, 17, 18.5, 20];
// 未记录过就用默认档（15.5px）；记录过但不在档位表里（旧版本遗留）同样回落到默认档。
let fsIdx = FS_STEPS.indexOf(parseFloat(localStorage.getItem("ol-read-size")));
if (fsIdx < 0) fsIdx = 2;
let wideRead = localStorage.getItem("ol-read-wide") !== "0";
function applyReading() {
  document.documentElement.style.setProperty("--read-size", FS_STEPS[fsIdx] + "px");
  document.querySelectorAll(".law, .preview").forEach((el) => el.classList.toggle("narrow", !wideRead));
  const b = $("#b-wide");
  if (b) {
    b.setAttribute("aria-pressed", wideRead ? "true" : "false");
    b.title = wideRead ? "阅读行宽：宽行（点击收窄，逐条读更省力）" : "阅读行宽：窄行（点击加宽）";
  }
}
function stepFontSize(dir) {
  fsIdx = Math.min(FS_STEPS.length - 1, Math.max(0, fsIdx + dir));
  localStorage.setItem("ol-read-size", String(FS_STEPS[fsIdx]));
  applyReading();
}
// fmtWhen：ISO UTC → 本机时间「YYYY-MM-DD HH:mm」。界面一律显示本机时间
// （docs/design-ui-language.md：主视图禁止 ISO UTC 时间戳）；原始串留在 title 里供核对，
// 不做任何美化或改写。
function fmtWhen(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return String(iso);
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
}
// utcTitle：把原始 UTC 串挂到 title 属性，白话显示与技术留证并存。
function utcTitle(iso) {
  return iso ? ' title="原始时间（UTC）：' + escapeHTML(iso) + '"' : "";
}
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
// 顶栏面包屑：当前位置 + 当前生效的筛选摘要。用户随时能看到「自己在哪、看的是不是筛过的」。
const PANEL_TITLE = {
  home: "关键词检索", browse: "按条件筛选", overview: "库总览", search: "法规检索",
  review: "智能审核", region: "地域标引", collect: "获取最新法规", settings: "设置",
};
function setCrumb(p) {
  const cur = $("#crumb-cur");
  if (cur) cur.textContent = PANEL_TITLE[p] || "";
  const ctx = $("#crumb-ctx");
  if (ctx) ctx.textContent = p === "browse" ? "· " + activeFilterText() : "";
}
function showPanel(p) {
  document.querySelectorAll(".nav").forEach((x) => x.classList.toggle("active", x.dataset.panel === p));
  document.querySelectorAll("main section").forEach((s) => s.classList.toggle("hidden", s.id !== "panel-" + p));
  setCrumb(p);
  // 三栏职责：筛选栏只服务「按条件筛选」，其余面板收起左列筛选栏。
  const flt = $("#filters");
  if (flt) flt.classList.toggle("hidden", p !== "browse");
  if (p === "home") loadHome();
  if (p === "browse") loadLaws();
  if (p === "overview") loadOverview();
  if (p === "review") loadReview();
  if (p === "region") { loadRegionStats(); loadRegionQueue(); }
  if (p === "collect") { loadSources(); loadTasks(); loadTasksTick(); }
}
// 重新加载当前可见面板（地区等全局偏好变更后需要刷新列表）。
function reloadViewing() {
  const cur = document.querySelector(".nav.active");
  if (cur) showPanel(cur.dataset.panel);
}
function applyMode() {
  document.querySelectorAll(".nav").forEach((b) => (b.hidden = b.dataset.mode !== mode));
  // 分组标题随模式收起：公众模式下不得出现只剩标题的空分组。
  document.querySelectorAll(".nav-group").forEach((g) => (g.hidden = !!g.dataset.mode && g.dataset.mode !== mode));
  $("#mode-public").classList.toggle("active", mode === "public");
  $("#mode-pro").classList.toggle("active", mode === "pro");
  showPanel(mode === "public" ? "home" : "overview");
}
document.querySelectorAll(".nav").forEach((b) => b.addEventListener("click", () => showPanel(b.dataset.panel)));
$("#mode-public").addEventListener("click", () => { mode = "public"; localStorage.setItem("ol-mode", mode); applyMode(); });
$("#mode-pro").addEventListener("click", () => { mode = "pro"; localStorage.setItem("ol-mode", mode); applyMode(); });
$("#theme-toggle").addEventListener("click", cycleTheme);
$("#t-density").addEventListener("click", () => {
  density = density === "compact" ? "comfortable" : "compact";
  localStorage.setItem("ol-density", density);
  applyDensity();
});
// 阅读控制只改显示层：字号档位与行宽持久化，取列条件与分面计数不受影响。
// 阅读控制挂在右列详情面板头（原文在右列展开）。
$("#b-fs-down").addEventListener("click", () => stepFontSize(-1));
$("#b-fs-up").addEventListener("click", () => stepFontSize(1));
$("#b-wide").addEventListener("click", () => {
  wideRead = !wideRead;
  localStorage.setItem("ol-read-wide", wideRead ? "1" : "0");
  applyReading();
});

// setNavCount：侧栏导航挂实时计数（侧栏不只做导航，还要显示「哪里需要你」）。
// 计数读不到时清空徽标而不是留旧值；精确条数挂在 title 里供核对。
function setNavCount(id, n, warn) {
  const el = $(id);
  if (!el) return;
  // 读不到（null/NaN）与确实为 0 都不显示徽标：前者是未知，后者是无需提醒；
  // 两种情况面板内都如实说明（「均已人工复核」/读取失败原因），不靠徽标传达。
  if (n == null || !isFinite(n) || Number(n) === 0) { el.textContent = ""; el.removeAttribute("title"); return; }
  el.textContent = compact(n);
  el.title = num(n) + " 条";
  el.classList.toggle("warn", !!warn);
}

/* ---------- 共享组件：徽章 / 法规要素 / 预览 / 引用格式 ---------- */
const FLAG_TEXT = { inferred_date: "公布日期由文本推断", whole_text: "全文未分条", no_publish_date: "缺少公布日期" };
const KIND_TEXT = { "govcn-import": "获取官方原文", "govlist-browse": "获取官方规章目录", "pack-build": "生成数据包", "pack-verify": "校验数据包", "pack-import": "导入数据包", "agent-review": "智能审核" };
const SRC_TEXT = { govcn: "国家行政法规库", govgz: "国家规章库", govlocal: "地方规章", govlist: "规章目录", batch: "批量导入" };

// statusBadge：复核状态徽章。确认来源必须区分显示（诚实展示纪律）：
// agent = 智能体确认、human = 人工确认、空 = 历史数据（来源未记录，不得冒充智能体确认）。
function statusBadge(s, src) {
  if (s === "confirmed") {
    if (src === "agent") return '<span class="badge agent">已复核 · 智能体确认</span>';
    if (src === "human") return '<span class="badge human">已复核 · 人工确认</span>';
    return '<span class="badge ok">已复核</span>';
  }
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
  return s + "。摘自开源法律法规数据库（官方渠道获取留证），引用前请核对官方原文。";
}
function legalHTML(p, articleNo) {
  return `<div class="pv">
    <h3>${escapeHTML(p.law_name || p.doc_id)} ${statusBadge(p.confirm_status, p.confirm_source)} ${validBadge(p.effective_date, p.expire_date)}</h3>
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
/* ---------- 右列：共享详情面板 ----------
   原文、条款、例外详情都在右列展开（三栏：左=导航与筛选、中=列表、右=详情）。
   「一键纠正」工具条只在例外详情下出现；关闭即收起右列。 */
function showDetail(title) {
  $("#detail").classList.remove("hidden");
  $("#detail-title").textContent = title || "详情";
  const b = $("#detail-body");
  b.innerHTML = "";
  applyReading();
  return b;
}
function hideDetail() {
  $("#detail").classList.add("hidden");
  $("#fix-bar").classList.add("hidden");
}
function openPreview(docID, articleNo, back) {
  app().Preview(docID).then((p) => {
    const box = showDetail("法规原文");
    box.innerHTML = legalHTML(p, articleNo) +
      (back ? '<p><button class="muted" id="detail-back">← 返回条款列表</button></p>' : "");
    bindCopy(box);
    const bb = $("#detail-back");
    if (bb) bb.addEventListener("click", back);
  }).catch((e) => {
    showDetail("详情").innerHTML = '<p class="bad">读取失败：' + escapeHTML(e.message || e) + "</p>";
  });
}
$("#detail-close").addEventListener("click", hideDetail);


/* ---------- 地区（坐标取自系统定位服务 · 判定全部在本机离线完成） ---------- */
let regionOpts = [];   // app().RegionOptions()
let regionCode = "";   // 当前生效地区码（"" = 不限地区）

async function loadRegion() {
  try {
    regionOpts = (await app().RegionOptions()) || [];
    const st = await app().CurrentRegion();
    regionCode = st.code || "";
    browseFilter.region = regionCode; // 本机地区即当前地区筛选：一处概念，不得「显示已设地区而列表未筛」
    fillRegionSelects();
    renderRegion(st);
  } catch (e) {
    regionOpts = [];
    $("#region-text").textContent = "地区信息读取失败";
    $("#region-note").textContent = String(e.message || e);
  }
}
function fillRegionSelects() {
  const prov = $("#region-prov");
  prov.innerHTML = '<option value="">选择省份…</option>' + regionOpts.filter((o) => o.level === 1)
    .map((o) => `<option value="${escapeHTML(o.code)}">${escapeHTML(o.name)}</option>`).join("");
  // 回显当前地区：地区码是地级时，先定位到其所属省。
  const cur = regionOpts.find((o) => o.code === regionCode);
  if (cur) prov.value = cur.level === 1 ? cur.code : cur.parent;
  fillRegionCities(cur && cur.level === 2 ? cur.code : "");
}
function fillRegionCities(pick) {
  const prov = $("#region-prov"), city = $("#region-city");
  const kids = regionOpts.filter((o) => o.parent === prov.value);
  city.innerHTML = '<option value="">全省</option>' + kids
    .map((o) => `<option value="${escapeHTML(o.code)}">${escapeHTML(o.name)}</option>`).join("");
  city.disabled = !prov.value;
  if (pick) city.value = pick;
}
function renderRegion(st) {
  $("#region-text").textContent = st.path ? "当前地区：" + st.path : "未设置地区（按全部地区浏览）";
  const src = { auto: "按位置就近判定", manual: "手动选择", trial: "试判定 · 非真实定位" }[st.source] || "";
  $("#region-note").textContent = [st.at ? String(st.at).slice(0, 10) : "", src, st.note || ""].filter(Boolean).join(" · ");
  // 地区状态只在顶栏报一行结果，控件在「按条件筛选」的分面里——
  // 地区是一个筛选维度，不是全局开关，侧栏也不再挂整套地区控件。
  const chip = $("#t-region");
  if (chip) {
    chip.classList.toggle("hidden", !st.path);
    const txt = $("#t-region-txt");
    if (txt) txt.textContent = st.path || "";
  }
}
async function applyRegion(code) {
  const msg = await app().SetRegion(code || "");
  if (msg) { toast("设置地区失败：" + msg); return; }
  const label = ($("#region-city").value && $("#region-city").selectedOptions[0].text) ||
    ($("#region-prov").value && $("#region-prov").selectedOptions[0].text) || "";
  await loadRegion();
  browseFilter.region = code || ""; // 应用地区即套用该筛选，界面与数据当次联动
  toast(code ? "当前地区：" + label + "（按名称判定地域，未标注的条目不在此列）" : "已取消地区限制");
  reloadViewing();
}
$("#region-prov").addEventListener("change", () => fillRegionCities(""));
$("#btn-region-apply").addEventListener("click", () => applyRegion($("#region-city").value || $("#region-prov").value));
$("#btn-region-clear").addEventListener("click", () => applyRegion(""));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// 取坐标：优先走系统定位（macOS 原生 CoreLocation）。Wails 的 WKWebView 未实现定位
// 授权回调，JS 的 navigator.geolocation 即使系统已授权也会被 webview 拒绝，因此只作后备。
async function fetchFix() {
  let start = null;
  try { start = await app().StartLocationFix(); } catch (e) { return { state: "unsupported", msg: "本机未提供系统定位接口。" }; }
  // 只有确定的终态才提前返回；working/not_started 一律继续轮询（发起与状态落地之间
  // 存在异步间隙，快照可能仍是「未开始」）。
  if (start.state === "ok" || start.state === "failed"
      || start.state === "unsupported" || start.state === "unauthorized") return start;
  for (let i = 0; i < 40; i++) {
    await sleep(500);
    let f = null;
    try { f = await app().LocationFixStatus(); } catch (e) { f = null; }
    if (f && (f.state === "ok" || f.state === "failed")) return f;
  }
  return { state: "timeout", acc: -1, msg: "系统定位服务 20 秒内未给出坐标（可能定位服务已关闭或信号不佳）。可用左侧下拉选择地区，或点「按选定地点试判定」。" };
}

// 拿到坐标后交 Go 侧离线判定地区（不调用任何在线地理编码服务）。
async function locateFrom(f) {
  const r = await app().LocateFromCoords(f.lat, f.lon);
  if (!r.ok) { toast("未能判定地区：" + r.msg); return; }
  await loadRegion();
  browseFilter.region = (r.region && r.region.code) || ""; // 定位成功后直接按本地区筛选，所见即所得
  const extra = f.acc >= 0 ? "，坐标精度约 " + Math.round(f.acc) + " 米" : "";
  toast("已判定：" + r.region.path + "（" + r.msg + extra + "）");
  reloadViewing();
}

// 后备路径：webview 自己的位置接口（macOS 上通常被 WKWebView 拒绝，其他平台可能可用）。
function webviewLocate() {
  if (!navigator.geolocation) { toast("本机未提供位置服务。请用左侧下拉选择地区，或点「按选定地点试判定」。"); return; }
  toast("正在尝试由界面取位置（后备路径）…");
  navigator.geolocation.getCurrentPosition(locateFrom, (err) => {
    const why = { 1: "界面层未放行定位请求", 2: "设备位置不可用", 3: "等待超时" }[err.code] || (err.message || "未知原因");
    toast("未获取到坐标：" + why + "。可用左侧下拉选择地区，或点「按选定地点试判定」。");
  }, { enableHighAccuracy: false, timeout: 8000, maximumAge: 600000 });
}

$("#btn-locate").addEventListener("click", async () => {
  // 先向系统申请授权（macOS 需要 bundle 身份 + plist 用途声明；Wails 自身不做这件事）。
  // 申请与定位都必须立即返回：绑定调用会占住主线程，而 CLLocationManager 只能在主队列
  // 工作，在 Go 侧阻塞等待会让弹窗和定位回调都排不到（实测踩过）。故一律「发起 → 前端轮询」。
  let perm = null;
  try { perm = await app().RequestLocationPermission(); } catch (e) { perm = null; }
  if (!perm) { toast("无法确认定位授权。请用左侧下拉选择地区，或点「按选定地点试判定」。"); return; }
  if (perm.status !== "authorized" && perm.status !== "not_determined") { toast(perm.msg); return; }
  if (perm.status === "not_determined") {
    toast("请在系统弹窗中选「允许」（最多等待 60 秒）…");
    for (let i = 0; i < 60; i++) {
      await sleep(1000);
      let st = null;
      try { st = await app().LocationPermissionStatus(); } catch (e) { st = null; }
      if (!st) continue;
      if (st.status === "authorized") { perm = st; break; }
      if (st.status !== "not_determined") { toast(st.msg); return; }
    }
    if (perm.status !== "authorized") {
      toast("未在弹窗中应答，仍未取得定位授权。可再点一次；或用左侧下拉选择地区、「按选定地点试判定」。");
      return;
    }
  }
  toast("正在读取坐标…");
  const f = await fetchFix();
  if (f.state === "ok") { await locateFrom(f); return; }
  if (f.state === "unsupported") { webviewLocate(); return; }
  toast(f.msg);
});
// 试判定：把选定地点的中心点当作位置，跑同一条离线判定链路（明确非真实定位）。
$("#btn-region-trial").addEventListener("click", async () => {
  const code = $("#region-city").value || $("#region-prov").value;
  if (!code) { toast("请先在上方选择省份（或市），再点「按选定地点试判定」"); return; }
  try {
    const r = await app().LocateFromRegion(code);
    if (!r.ok) { toast(r.msg); return; }
    await loadRegion();
    toast(r.msg);
    reloadViewing();
  } catch (e) { toast("试判定失败：" + (e.message || e)); }
});
/* ---------- 按条件筛选：多维筛选（筛选 ↔ 条目与计数实时联动） ---------- */
// browseFilter 是浏览页唯一的筛选状态。公众模式常用「效力等级 + 地区」两个维度，
// 专业模式可再叠加复核状态、来源、公布年代——同一套条件、同一条 SQL 口径
// （Go 侧 BrowseLawList 与 BrowseFacets 共用 browseWhere，计数与列表不会两说）。
const browseFilter = { level: "", confirm: "", source: "", year: "", region: "" };

function browseArgs(limit, offset) {
  return {
    level: browseFilter.level, confirm: browseFilter.confirm, source: browseFilter.source,
    year: browseFilter.year, region: browseFilter.region,
    kw: $("#b-kw").value.trim(), limit: limit || 300, offset: offset || 0,
  };
}

// 分面组：可折叠。组头给出「本维度已选什么 / 共有多少可选值」，折叠状态记住——
// 年代、来源这类长维度默认收起，公众常用的类别与地区默认展开（筛选栏不必长成一条竖河）。
const GROUP_DEFAULT = { level: true, confirm: true, year: false, source: false, region: true };
let groupOpen = {};
try { groupOpen = JSON.parse(localStorage.getItem("ol-fgrp") || "{}") || {}; } catch (e) { groupOpen = {}; }
const groupExpanded = {}; // 「显示全部」只在本次会话内展开，下次进来仍按默认收起
let lastFacets = {};      // 最近一次分面结果：展开/折叠时就地重绘，不再查库
function isGroupOpen(dim) { return dim in groupOpen ? !!groupOpen[dim] : GROUP_DEFAULT[dim] !== false; }
function setGroupOpen(dim, open) { groupOpen[dim] = open; localStorage.setItem("ol-fgrp", JSON.stringify(groupOpen)); }
function facetGroup(dim) {
  const g = $("#g-" + dim);
  if (!g) return;
  const open = isGroupOpen(dim);
  g.dataset.open = open ? "true" : "false";
  const hd = g.querySelector(".fhd");
  if (!hd) return;
  hd.setAttribute("aria-expanded", open ? "true" : "false");
  if (!hd.dataset.wired) {
    hd.dataset.wired = "1";
    hd.addEventListener("click", () => { setGroupOpen(dim, !isGroupOpen(dim)); facetGroup(dim); });
  }
}

// facetChips 渲染一个维度的可选值与计数。计数按「其余条件已生效、本维度不限」给出
// （Go 侧 skip 自身维度），所以选中某项后仍可改选；计数为 0 的选项禁点，
// 免得用户点了换来一个空列表。地区维度只列条目最多的前几个省份，
// 其余省市用下方下拉（下拉能选到地级，比芯片更细）。
function facetChips(box, dim, opts, max, isOn) {
  const el = $(box);
  if (!el) return;
  const usable = (opts || []).filter((o) => o.value !== "");
  const openAll = groupExpanded[dim] || !max;
  const shown = openAll ? usable : usable.slice(0, max);
  let html = shown.map((o) => {
    const on = isOn ? isOn(o.value) : browseFilter[dim] === o.value;
    const off = o.count === 0 && !on;
    const cls = on ? "on" : ""; // 选中态单独成类，class 属性里保留 fchip 字面量供样式门禁核对
    return `<button class="fchip ${cls}" data-dim="${dim}" data-val="${escapeHTML(o.value)}"` +
      (off ? " disabled" : "") + ` title="${escapeHTML(o.label)}：${num(o.count)} 部">${escapeHTML(o.label)}` +
      `<span class="fcount num">${num(o.count)}</span></button>`;
  }).join("");
  if (!shown.length) html = '<span class="fempty">该维度暂无可选值</span>';
  if (!openAll && usable.length > shown.length) {
    html += `<button class="fchip more" data-more="${dim}" title="其余 ${num(usable.length - shown.length)} 项同样可筛">显示全部 ${num(usable.length)} 项</button>`;
  }
  el.innerHTML = html;
  el.querySelectorAll(".fchip").forEach((b) => b.addEventListener("click", () => {
    const more = b.dataset.more;
    if (more) { groupExpanded[more] = true; renderFacets(lastFacets); return; }
    const d = b.dataset.dim, val = b.dataset.val;
    if (d === "region") {
      // 芯片按已标注的区划码前两位分组，选省即按整省筛选（含下辖地级）。
      applyRegion(isOn(val) ? "" : val + "0000");
      return;
    }
    browseFilter[d] = browseFilter[d] === val ? "" : val;
    loadLaws();
  }));
  facetAdorn(dim, usable, isOn);
}

// facetAdorn：组头右侧给出「已选什么」或「共几项可选」——折叠状态下也能看出
// 这个维度现在有没有参与筛选、筛的是哪一项。
function facetAdorn(dim, usable, isOn) {
  const ad = $("#ad-" + dim);
  if (!ad) return;
  const sel = browseFilter[dim];
  if (dim === "region" && browseFilter.region) {
    const chip = (usable || []).find((o) => (browseFilter.region || "").slice(0, 2) === o.value);
    ad.textContent = "已选 " + ((chip && chip.label) || regionPathOf(browseFilter.region) || browseFilter.region);
    ad.classList.add("sel");
    return;
  }
  if (!sel) { ad.textContent = (usable || []).length + " 项"; ad.classList.remove("sel"); return; }
  const hit = (usable || []).find((o) => o.value === sel);
  ad.textContent = "已选 " + ((hit && hit.label) || sel);
  ad.classList.add("sel");
}

function renderFacets(v) {
  lastFacets = v || {};
  facetChips("#f-levels", "level", v.levels);
  facetChips("#f-confirms", "confirm", v.confirms);
  facetChips("#f-years", "year", v.years, 6);
  facetChips("#f-sources", "source", v.sources, 5);
  facetChips("#f-regions", "region", v.regions, 6, (val) => (browseFilter.region || "").slice(0, 2) === val);
  ["level", "confirm", "year", "source", "region"].forEach(facetGroup);
  const note = $("#f-region-note");
  if (note) {
    note.textContent = v.unlabeled
      ? `另有 ${num(v.unlabeled)} 部未标注地区码，按法规名称中的地名匹配（上方下拉可选到具体地级）`
      : "当前条件下的条目均已标注地区";
  }
  $("#f-total").textContent = num(v.total);
  $("#b-stats").textContent = activeFilterText();
  setCrumb("browse");
}

// activeFilterText 把当前生效的筛选回显成一句白话——用户必须能看到
// 「现在看到的是被筛过的」，而不是以为在看全库。
function activeFilterText() {
  const parts = [];
  if (browseFilter.level) parts.push("类别 " + browseFilter.level);
  if (browseFilter.confirm) parts.push("复核状态 " + { confirmed: "已复核确认", pending: "待复核", rejected: "已驳回" }[browseFilter.confirm]);
  if (browseFilter.year) parts.push("公布年代 " + (browseFilter.year === "none" ? "缺失" : browseFilter.year + " 年代"));
  if (browseFilter.source) parts.push("来源 " + browseFilter.source);
  if (browseFilter.region) parts.push("地区 " + (($("#region-city").selectedOptions[0] || {}).text || regionPathOf(browseFilter.region) || browseFilter.region));
  const kw = $("#b-kw").value.trim();
  if (kw) parts.push("名称含「" + kw + "」");
  return parts.length ? "当前筛选：" + parts.join(" · ") : "未设筛选条件（显示全库）";
}
function regionPathOf(code) {
  const hit = (regionOpts || []).find((o) => o.code === code);
  return hit ? hit.name : "";
}

async function loadLaws() {
  const args = browseArgs(300, 0);
  try {
    const [rows, v] = await Promise.all([app().BrowseLawList(args), app().BrowseFacets(args)]);
    renderFacets(v || {});
    const list = rows || [];
    const pc = $("#b-pane-cnt");
    if (pc) {
      pc.innerHTML = `显示 <b>${num(list.length)}</b> / 共 <b>${num((v || {}).total)}</b> 部`;
      pc.title = list.length >= 300 ? "本次列出前 300 部，其余请用条件收窄" : "已列出当前条件下的全部条目";
    }
    $("#law-list").innerHTML = list.length ? list.map((r) => `
      <div class="item${r.confirm_status === "confirmed" ? "" : " pend"}" data-law="${escapeHTML(r.law_id)}" data-name="${escapeHTML(r.name)}">
        <div class="t">${escapeHTML(r.name)} ${statusBadge(r.confirm_status, r.confirm_source)} ${validBadge(r.effective_date, r.expire_date)}</div>
        <div class="m">${rowMeta(r)}</div>
      </div>`).join("") :
      '<p class="muted">' + (browseFilter.region
        ? "当前条件下没有法规。已标注地区的按区划码匹配、未标注的按名称中的地名匹配；两者都判不出地区的条目不会出现在按地区筛选的结果中——可点「清空筛选」，或改用上方下拉。"
        : "当前条件下没有法规。可点「清空筛选」，或放宽名称关键词。") + "</p>";
    document.querySelectorAll("#law-list .item").forEach((el) =>
      el.addEventListener("click", () => openLaw(el.dataset.law, el.dataset.name)));
  } catch (e) {
    $("#law-list").innerHTML = '<p class="bad">读取失败：' + escapeHTML(e.message || e) + "</p>";
  }
}

/* ---------- 公众 · 查法规（搜索为中心） ---------- */
async function loadHome() {
  try {
    const v = await app().Status();
    const levels = ["行政法规", "部门规章", "地方政府规章"];
    let html = levels.map((lv) => {
      const r = (v.by_level || []).find((x) => x.level === lv) || { pending: 0, confirmed: 0, rejected: 0 };
      return `<button class="card big" data-level="${lv}" title="按该类别浏览"><b>${num(r.pending + r.confirmed + r.rejected)}</b><span>${lv}</span><span class="muted">已复核 ${num(r.confirmed)}</span></button>`;
    }).join("");
    const lf = Object.values(v.last_fetch || {}).sort().reverse()[0];
    if (lf) html += `<div class="card big"><b>${escapeHTML(lf.slice(0, 10))}</b><span>数据最近更新</span></div>`;
    $("#h-cards").innerHTML = html;
    // 首页卡片即入口：点类别直接进浏览页并带上该筛选，数字与筛选当场对上。
    document.querySelectorAll("#h-cards .card[data-level]").forEach((el) => el.addEventListener("click", () => {
      browseFilter.level = el.dataset.level;
      browseFilter.confirm = browseFilter.source = browseFilter.year = "";
      showPanel("browse");
    }));
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
      el.addEventListener("click", () => openPreview(el.dataset.doc)));
  } catch (e) {
    $("#h-results").innerHTML = '<p class="bad">检索失败：' + escapeHTML(e.message || e) + "</p>";
  }
}
$("#btn-hsearch").addEventListener("click", homeSearch);
$("#hq").addEventListener("keydown", (e) => { if (e.key === "Enter") homeSearch(); });

/* ---------- 按条件筛选 · 条目行的元信息 ---------- */
// LEVEL_UNLABELED：Go 侧对空效力等级给出诚实默认值（desktop/browse.go 的 SQL 默认值），
// 因此前端拿到的是这个字面量而不是空串。判定「缺不缺」必须按它来，否则缺失会被
// 当成已知值排进元信息行（旧版每行三遍「未标注」就是这么来的）。
const LEVEL_UNLABELED = "效力等级未标注";
function levelOf(r) {
  const v = r.level || "";
  return v === LEVEL_UNLABELED ? "" : v;
}
// rowRegion 地域显示：库里已标注（auto/manual/pack）与未标注（名称推定）必须区别显示——
// 机器建议不得看起来像已复核，名称推定不得看起来像已标注（诚实展示纪律）。
// 行内只用最短标注（已复核 / 待复核 / 名称推定），完整说明在条目详情里。
function rowRegion(r) {
  if (r.region_source === "unknown") {
    return '<span class="unlabeled">区划码 ' + escapeHTML(r.region_code) + "（不在内置区划表，无法显示地名）</span>";
  }
  if (!r.region_source) {
    return r.region ? '<span class="unlabeled">' + escapeHTML(r.region) + "（名称推定）</span>" : "";
  }
  const tag = r.region_source === "manual" ? "已复核"
    : r.region_source === "pack" ? "随更新包携带 · 待复核" : "机器建议 · 待复核";
  return '<span class="region">' + escapeHTML(r.region || "区划码 " + r.region_code) + "（" + tag + "）</span>";
}
// rowMeta 一行只列已知值；缺失项合并成一个安静标记（「未标注 N 项」），具体缺什么
// 挂在 title 里并给出补救去处。原先每行重复「效力等级未标注 · 地域未标注 ·
// 发布机关未标注」三遍，既吵又让人误以为信息很多——缺失仍如实显示，只是不刷屏。
function rowMeta(r) {
  const parts = [];
  const lv = levelOf(r);
  if (lv) parts.push(escapeHTML(lv));
  const reg = rowRegion(r);
  if (reg) parts.push(reg);
  if (r.issuer) parts.push(escapeHTML(r.issuer));
  if (r.effective_date) parts.push(escapeHTML(r.effective_date) + " 起施行");
  const miss = [];
  if (!lv) miss.push("效力等级");
  if (!r.issuer) miss.push("发布机关");
  if (!r.region) miss.push("地域");
  if (miss.length) {
    parts.push('<span class="miss" title="未标注：' + miss.join("、") +
      "——可在「智能审核」核对效力等级，在「地域标引」补地区\">未标注 " + miss.length + " 项</span>");
  }
  return parts.join(" · ");
}
// openLaw 右列详情：修订沿革 + 条款列表（点条款看留证原文，可返回条款列表）。
async function openLaw(lawID, name) {
  document.querySelectorAll("#law-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.law === lawID));
  const vs = (await app().LawVersions(lawID)) || [];
  const arts = (await app().BrowseArticles(lawID)) || [];
  const tl = vs.map((x) => `<div class="tl-row"><b>${escapeHTML(x.version_no)}</b> ${escapeHTML(x.effective)} 起施行${x.expire ? "，" + escapeHTML(x.expire) + " 失效" : "，现行有效"} ${statusBadge(x.status)}</div>`).join("");
  const list = arts.map((x) => `<div class="item art" data-doc="${escapeHTML(x.doc_id)}" data-no="${escapeHTML(x.article_no)}"><div class="t">${escapeHTML(x.article_no)}</div><div class="m">${escapeHTML(x.snippet)}…</div></div>`).join("");
  const box = showDetail(name || "法规详情");
  box.innerHTML = `<h3>${escapeHTML(name)}</h3>
    <h4>修订沿革</h4><div class="tl">${tl || '<p class="muted">暂无版本记录。</p>'}</div>
    <h4>条款（${arts.length} 条）</h4>${list || '<p class="muted">该法规暂未收录分条文本（可在「查法规」中全文检索）。</p>'}`;
  document.querySelectorAll("#detail-body .art").forEach((el) =>
    el.addEventListener("click", () => openPreview(el.dataset.doc, el.dataset.no, () => openLaw(lawID, name))));
}
$("#b-refresh").addEventListener("click", loadLaws);
// 清空筛选：维度一律复位；地区是全局偏好，一并取消，避免下一次浏览又被悄悄筛掉。
$("#btn-filter-clear").addEventListener("click", () => {
  browseFilter.level = browseFilter.confirm = browseFilter.source = browseFilter.year = "";
  if (browseFilter.region) { applyRegion(""); return; }
  loadLaws();
});
// 名称关键词即时联动：停手 400ms 再查库，既不逐字打库，也不必按回车。
let kwTimer = null;
$("#b-kw").addEventListener("input", () => { clearTimeout(kwTimer); kwTimer = setTimeout(loadLaws, 400); });
$("#b-kw").addEventListener("keydown", (e) => { if (e.key === "Enter") { clearTimeout(kwTimer); loadLaws(); } });

// renderBars：法规构成给出「形状」而不只是数字——每级一条堆叠条，段长按条目数比例，
// 条长之间按最大等级归一（哪一级占多少一眼可辨）。精确值仍在「各等级的精确数字」折叠里。
function renderBars(rows) {
  const el = $("#ov-bars");
  if (!el) return;
  const data = (rows || []).filter((r) => r && ((r.pending || 0) + (r.confirmed || 0) + (r.rejected || 0) > 0));
  if (!data.length) {
    el.innerHTML = '<p class="muted">法规库中还没有条文。可在「获取最新法规」中从官方渠道获取。</p>';
    return;
  }
  const totalOf = (r) => (r.pending || 0) + (r.confirmed || 0) + (r.rejected || 0);
  const max = data.reduce((m, r) => Math.max(m, totalOf(r)), 0) || 1;
  const seg = (cls, n) => (n ? `<i class="${cls}" style="width:${((n / max) * 100).toFixed(2)}%"></i>` : "");
  el.innerHTML = data.map((r) => `<div class="bar-row" title="已复核确认 ${num(r.confirmed)} · 待复核 ${num(r.pending)} · 已驳回 ${num(r.rejected)}">` +
    `<span class="nm">${escapeHTML(r.level || "效力等级未标注")}</span>` +
    `<span class="bar">${seg("ok", r.confirmed)}${seg("pend", r.pending)}${seg("no", r.rejected)}</span>` +
    `<span class="num">${num(totalOf(r))} 条</span></div>`).join("");
}

/* ---------- 专业 · 库总览（数字 → 处境 → 下一步；技术信息折叠在页尾） ---------- */
async function loadOverview() {
  try {
    const v = await app().Status();
    $("#status-err").textContent = v.open_err || "";
    const lf = Object.values(v.last_fetch || {}).sort().reverse()[0];
    $("#ov-datadir").textContent = "本机法规库：" + num(v.laws) + " 部法规、" + num(v.articles) + " 条条文" +
      (lf ? "；最近一次获取 " + fmtWhen(lf) + "（本机时间）" : "；本机尚未记录任何获取时间");
    setSideFoot(v);
    setNavCount("#cnt-review", v.pending, true);
    $("#ov-hero").innerHTML = `
      <div class="card big"><b>${num(v.laws)}</b><span>收录法规</span></div>
      <div class="card big"><b>${num(v.confirmed)}</b><span>已复核可用</span></div>
      <div class="card big warn"><b>${num(v.pending)}</b><span>待复核</span></div>
      <div class="card big"><b>${escapeHTML(lf ? fmtWhen(lf).slice(0, 10) : "—")}</b><span>数据最近更新</span></div>`;
    // 处境一句话 + 下一步动作（最多两个）。按钮按用户目标命名，不用内部流程名
    // （原「去复核」「采集与更新」是内部流程说法）。
    let acts = "";
    if (v.pending > 0) {
      acts += `<span>法规库中有 <b>${num(v.pending)}</b> 条内容尚未经审核，引用前请以官方原文为准。</span><button data-go="review">去智能审核</button>`;
    } else {
      acts += "<span>已收录的条目均已审核。</span>";
    }
    acts += `<button data-go="collect">获取最新法规</button>`;
    $("#ov-actions").innerHTML = acts;
    document.querySelectorAll("#ov-actions [data-go]").forEach((b) => b.addEventListener("click", () => showPanel(b.dataset.go)));
    $("#level-table tbody").innerHTML = (v.by_level || []).map((r) =>
      `<tr><td>${escapeHTML(r.level)}</td><td>${num(r.pending)}</td><td>${num(r.confirmed)}</td><td>${num(r.rejected)}</td></tr>`).join("");
    renderBars(v.by_level);
    const reps = (await app().RecentReports(5)) || [];
    // 失败如实说：ok=0 不得渲染成中性数字（诚实展示纪律）。
    $("#reports").innerHTML = reps.map((r) => `<div class="rep"><b>${escapeHTML(SRC_TEXT[r.source] || r.source)}</b> ${fmtWhen(r.modified_at)}<span class="muted">（本机时间）</span><br><span class="muted"${utcTitle(r.modified_at)}>${r.ok > 0 ? "获取到 " + num(r.ok) + " 条" : "本轮没有新增内容"} · 失败 ${num(r.failed)} 条${r.pages ? " · 翻页 " + num(r.pages) + " 页" : ""}</span></div>`).join("") ||
      '<p class="muted">暂无获取记录。可在「获取最新法规」中从官方渠道获取原文。</p>';
    const revs = (await app().RecentReviews(5)) || [];
    $("#reviews").innerHTML = revs.map((r) => `<div class="rep"${utcTitle(r.created_at)}>${fmtWhen(r.created_at)}（本机时间）· ${escapeHTML(r.reviewer)} <span class="muted">${escapeHTML(r.action === "confirm" ? "确认" : r.action === "reject" ? "驳回" : "重新复核")} 条目 ${escapeHTML(r.doc_id)}</span></div>`).join("") ||
      '<p class="muted">暂无复核记录。智能审核会自动核对条目，没把握的会进入「智能审核」的例外队列请您纠正。</p>';
    // 技术信息：路径、启动参数、数据格式版本一律收在折叠里，不占主视图。
    $("#ov-tech").innerHTML = `<span>数据存放位置：${escapeHTML(v.data_dir || "未知")}${v.data_src ? "（由启动参数指定：" + escapeHTML(v.data_src) + "）" : ""}</span>
      <span>数据格式版本 ${escapeHTML(v.schema_version || "?")}（本程序支持 ${escapeHTML(v.schema_min)}–${escapeHTML(v.schema_max)}）</span>
      <span>版本记录 ${num(v.versions)} 条 · 文档 ${num(v.documents)} 份 · 条文 ${num(v.articles)} 条</span>`;
  } catch (e) {
    $("#status-err").textContent = "读取失败：" + (e.message || e);
  }
}

// setSideFoot 顶栏一行白话状态（侧栏不放技术版本号，那属折叠里的技术信息）。
// 顶栏同时给出本机法规库总数与待复核提醒：状态在「看数据的地方」常驻，
// 而不是只在某个面板里出现一次。
function setSideFoot(v) {
  setNavCount("#cnt-review", v.pending, true);
  setTopStatus(v);
}
// setTopStatus 顶栏状态徽标：读不到就不显示（未知不等于 0），面板内另有如实说明。
function setTopStatus(v) {
  const lib = $("#t-lib-n");
  if (lib) {
    lib.textContent = v.laws == null ? "—" : num(v.laws);
    $("#t-lib").title = v.laws == null ? "本机法规库条目数读取失败" : "本机法规库共 " + num(v.laws) + " 部";
  }
  const pend = $("#t-pending");
  if (pend) {
    const show = v.pending != null && Number(v.pending) > 0;
    pend.classList.toggle("hidden", !show);
    if (show) {
      $("#t-pending-n").textContent = num(v.pending);
      pend.title = num(v.pending) + " 条尚未经人工复核，引用前请以官方原文为准";
    }
  }
}
async function loadSideFoot() {
  try { setSideFoot(await app().Status()); } catch (e) { /* 绑定未就绪时略过，进面板会再取 */ }
}


/* ---------- 专业 · 法规检索 ---------- */
// 检索命中后就地按维度收窄：计数按本次命中统计（全文检索不重排序、不额外查库），
// 点选即筛、再点取消——所见计数与所见列表始终同源。
let lastHits = [];
const hitFilter = { level: "", confirm: "" };
const CONFIRM_LABEL = { confirmed: "已复核确认", pending: "待复核", rejected: "已驳回", "": "复核状态未记录" };

function hitChips(box, dim, counts) {
  const el = $(box);
  if (!el) return;
  const entries = Object.entries(counts).sort((a, b) => b[1] - a[1]);
  if (!entries.length) { el.innerHTML = '<span class="fempty">检索后在此收窄</span>'; return; }
  el.innerHTML = entries.map(([val, n]) => {
    const label = dim === "confirm" ? (CONFIRM_LABEL[val] || val) : (val || "效力等级未标注");
    const cls = hitFilter[dim] === val ? "on" : "";
    return `<button class="fchip ${cls}" data-dim="${dim}" data-val="${escapeHTML(val)}"` +
      ` title="${escapeHTML(label)}：${num(n)} 条命中">${escapeHTML(label)}<span class="fcount num">${num(n)}</span></button>`;
  }).join("");
  el.querySelectorAll(".fchip").forEach((b) => b.addEventListener("click", () => {
    const d = b.dataset.dim, val = b.dataset.val;
    hitFilter[d] = hitFilter[d] === val ? "" : val;
    renderHits();
  }));
  // 组头 adornment：折叠/未折叠都能看出这一维现在筛的是哪一项。
  const ad = $("#sad-" + dim);
  if (ad) {
    const sel = hitFilter[dim];
    if (!sel) { ad.textContent = entries.length + " 项"; ad.classList.remove("sel"); }
    else {
      const hit = entries.find(([v]) => v === sel);
      ad.textContent = "已选 " + ((hit && (dim === "confirm" ? (CONFIRM_LABEL[sel] || sel) : (sel || "效力等级未标注"))) || sel);
      ad.classList.add("sel");
    }
  }
}

function renderHitFacets() {
  const lv = {}, cf = {};
  for (const r of lastHits) {
    lv[r.level || ""] = (lv[r.level || ""] || 0) + 1;
    const c = r.confirm_status || "";
    cf[c] = (cf[c] || 0) + 1;
  }
  hitChips("#sf-level", "level", lv);
  hitChips("#sf-confirm", "confirm", cf);
}

function renderHits() {
  const rows = lastHits.filter((r) =>
    (!hitFilter.level || (r.level || "") === hitFilter.level) &&
    (!hitFilter.confirm || (r.confirm_status || "") === hitFilter.confirm));
  $("#search-info").textContent = `命中 ${num(lastHits.length)} 条（全文检索，未重排序）` +
    (rows.length === lastHits.length ? "" : ` · 按当前筛选显示 ${num(rows.length)} 条`);
  const pc = $("#s-pane-cnt");
  if (pc) {
    pc.innerHTML = `显示 <b>${num(rows.length)}</b> / 命中 <b>${num(lastHits.length)}</b> 条`;
    pc.title = "全文检索按语料原样给出，不重排序；就地收窄只筛本次命中，不重新查库";
  }
  $("#search-list").innerHTML = rows.map((r) => `
    <div class="item${r.confirm_status === "confirmed" ? "" : " pend"}" data-doc="${escapeHTML(r.doc_id)}" data-no="${escapeHTML(r.article_no)}" data-law="${escapeHTML(r.law_name)}">
      <div class="t">${escapeHTML(r.law_name)} <span class="pill">${escapeHTML(r.level || "等级未标注")}</span> ${statusBadge(r.confirm_status)} ${validBadge(r.effective_date, r.expire_date)}</div>
      <div class="m">${escapeHTML(r.article_no)}${r.effective_date ? " · " + r.effective_date + " 起施行" : ""}</div>
      <div class="m">${escapeHTML(r.snippet)}</div>
    </div>`).join("") || '<p class="muted">当前筛选下没有命中，再点一次已选中的条件即可取消。</p>';
  document.querySelectorAll("#search-list .item").forEach((el) =>
    el.addEventListener("click", () => showSearchPreview(el.dataset.doc, el.dataset.no, el.dataset.law)));
}

async function doSearch() {
  const q = $("#q").value.trim();
  if (!q) return;
  $("#search-info").textContent = "检索中…";
  hitFilter.level = hitFilter.confirm = ""; // 换关键词即换样本，旧筛选不得跨样本沿用
  try {
    lastHits = (await app().SearchArticles(q, 50)) || [];
    renderHitFacets();
    renderHits();
  } catch (e) {
    lastHits = [];
    renderHitFacets();
    $("#search-info").textContent = "检索失败：" + (e.message || e);
    $("#search-list").innerHTML = '<p class="bad">检索失败：' + escapeHTML(e.message || e) + "</p>";
  }
}
async function showSearchPreview(docID, articleNo, lawName) {
  document.querySelectorAll("#search-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.doc === docID));
  const p = await app().Preview(docID);
  const slot = showDetail("条文与核对");
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


/* ---------- 专业 · 智能审核（双智能体自审 → 人工只处理例外） ---------- */
// 纪律（docs/design-agent-review.md）：两项置信度都达门槛才自动确认；
// 拿不准、留痕不合格式一律进例外队列。智能体确认与人工确认分开计数、分开徽章；
// 历史确认（来源未记录）不得冒充其中任何一种（诚实展示纪律）。
let exQueue = [];
let exSelected = null;
let rvTimer = null;

async function loadReview() {
  loadAgentStatus();
  loadAgentStats();
  loadExceptions();
  rvTick(); // 有审核任务在跑时接着轮询进度（页面切走再回来也能接上）
}
async function loadAgentStatus() {
  try {
    const st = await app().AgentStatus();
    $("#rv-channel").textContent = st.configured
      ? "智能审核通道已就绪" + (st.model ? "（模型 " + st.model + "）" : "")
      : (st.guidance || "智能审核通道尚未配置，暂时无法开始审核。");
    $("#btn-review-run").disabled = !st.configured;
    $("#rv-tech").innerHTML = st.technical ? `<span>${escapeHTML(st.technical)}</span>` : "";
  } catch (e) { $("#rv-channel").textContent = "通道状态读取失败：" + (e.message || e); }
}
async function loadAgentStats() {
  try {
    const st = await app().AgentStats();
    $("#rv-stats").innerHTML = `
      <div class="card big"><b>${num(st.agent_confirmed)}</b><span>智能体已确认</span><span class="muted">两项把握度都达门槛</span></div>
      <div class="card big"><b>${num(st.human_confirmed)}</b><span>人工已确认</span><span class="muted">例外队列中由您纠正${st.legacy_confirmed ? " · 另有历史确认 " + num(st.legacy_confirmed) + " 条（来源未记录）" : ""}</span></div>
      <div class="card big warn"><b>${num(st.escalated)}</b><span>需要您纠正</span><span class="muted">智能体审过但没把握</span></div>
      <div class="card big"><b>${num(st.unreviewed)}</b><span>尚未审核</span><span class="muted">还没有智能体留痕</span></div>`;
    setNavCount("#cnt-review", st.escalated, true);
    if (st.last_run) $("#rv-task-info").textContent = "最近一轮审核 " + fmtWhen(st.last_run) + "（本机时间）";
  } catch (e) { $("#rv-stats").textContent = "统计读取失败：" + (e.message || e); }
}
async function loadExceptions() {
  try {
    exQueue = (await app().ExceptionList($("#ex-filter").value.trim(), 300, 0)) || [];
    $("#ex-info").textContent = exQueue.length ? `已载入 ${num(exQueue.length)} 条（一次最多显示 300 条）` : "";
    $("#ex-list").innerHTML = exQueue.map((r) => `
      <div class="item${r.doc_id === exSelected ? " sel" : ""}" data-doc="${escapeHTML(r.doc_id)}">
        <div class="t">${escapeHTML(r.law_name || r.doc_id)} <span class="badge pend">需要纠正</span></div>
        <div class="m">条目编号 ${escapeHTML(r.doc_id)} · 来源：${escapeHTML(r.source || "未记录来源")}${r.level ? " · " + escapeHTML(r.level) : ""}</div>
        <div class="m"><span class="pill warn">没把握的原因：${escapeHTML(r.reason)}</span>${r.confidence ? ` <span class="pill">把握度 ${Math.round(r.confidence * 100)}%</span>` : ""}</div>
        ${r.suggest ? `<div class="m">智能体建议：${escapeHTML(r.suggest)}</div>` : ""}
      </div>`).join("") ||
      '<p class="muted">没有需要纠正的条目（或上方条件无匹配）。清空条件再点「载入例外条目」可查看全部。</p>';
    document.querySelectorAll("#ex-list .item").forEach((el) => el.addEventListener("click", () => showException(el.dataset.doc)));
  } catch (e) { $("#ex-list").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
async function showException(docID) {
  exSelected = docID;
  document.querySelectorAll("#ex-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.doc === docID));
  const row = exQueue.find((x) => x.doc_id === docID) || {};
  try {
    const p = await app().Preview(docID);
    const box = showDetail("需要您纠正的条目");
    box.innerHTML = `<div class="warnbox">智能体没把握：${escapeHTML(row.reason || "智能体未给出可读依据——请核对原文与著录信息。")}</div>` +
      (row.suggest ? `<p class="muted small">智能体建议：${escapeHTML(row.suggest)}${row.confidence ? "（把握度 " + Math.round(row.confidence * 100) + "%）" : ""}</p>` : "") +
      legalHTML(p) +
      `<h4>复核记录</h4><ul class="log">${(p.review_log || []).map((x) => "<li>" + escapeHTML(x) + "</li>").join("") || '<li class="muted">暂无复核记录。</li>'}</ul>`;
    bindCopy(box);
    $("#fix-bar").classList.remove("hidden");
  } catch (e) { showDetail("详情").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
async function correct(verdict) {
  if (!exSelected) return toast("请先在例外队列点选要纠正的条目");
  const reason = $("#fix-reason").value.trim();
  if (!reason) return toast("请填写结论依据（一句话即可，进复核记录）");
  try {
    await app().CorrectDoc(exSelected, verdict, reason);
    toast(verdict === "confirm" ? "已确认：该条目标注为人工确认" : "已驳回：该条目标注为已驳回");
    $("#fix-reason").value = "";
    loadAgentStats(); loadExceptions(); loadSideFoot();
  } catch (e) { toast("操作失败：" + (e.message || e)); }
}
async function runReview() {
  try {
    const info = await app().StartReviewTask(Number($("#rv-limit").value) || 50);
    toast(`智能审核 #${info.id} 已开始：逐条审核，随时可取消`);
    rvTick();
  } catch (e) { toast("无法开始：" + (e.message || e)); }
}
// rvTick：轮询最近一轮智能审核任务。进度白话回显；任务结束即刷新统计与例外队列。
function rvTick() {
  if (rvTimer) return;
  rvTimer = setInterval(async () => {
    let tasks = [];
    try { tasks = (await app().TaskList()) || []; } catch (e) { return; }
    const t = tasks.filter((x) => x.kind === "agent-review")[0] || null;
    if (!t) { clearInterval(rvTimer); rvTimer = null; return; }
    const st = { running: "审核中", done: "已完成", failed: "失败", cancelled: "已取消" };
    $("#rv-task-info").textContent = `${st[t.state] || t.state}${t.progress ? " · " + t.progress : ""}`;
    if (t.state !== "running") {
      clearInterval(rvTimer); rvTimer = null;
      loadAgentStats(); loadExceptions(); loadSideFoot();
    }
  }, 1500);
}
$("#btn-review-run").addEventListener("click", runReview);
$("#btn-ex-load").addEventListener("click", loadExceptions);
$("#fix-confirm").addEventListener("click", () => correct("confirm"));
$("#fix-reject").addEventListener("click", () => correct("reject"));


/* ---------- 专业 · 地域标引（机器建议 → 人工落判） ---------- */
const RG_CONF_TEXT = {
  high: "高置信（名称前缀命中）",
  medium: "中置信（仅制定机关命中，机器不写入）",
  conflict: "名称与制定机关指向不同地区（机器不写入）",
  national: "国家级效力，不按名称标地域",
  none: "无法判定",
  marked: "已有标注",
  rejected: "人工已复核并留空",
};
// 标注来源文案：pack = 随更新包携带（本库未复核），不得显示成已复核；
// auto = 本库机器判定；manual = 人工落判；trial = 试判定。
const RG_SRC_TEXT = {
  auto: "本库机器判定",
  manual: "人工确认",
  trial: "试判定",
  pack: "随更新包携带（本库未复核）",
};
function rgStateBadge(s, src) {
  if (s === "suggest") {
    return '<span class="badge pend">' + (src === "pack" ? "随包携带 · 待复核" : "机器建议 · 待复核") + "</span>";
  }
  if (s === "reviewed") return '<span class="badge ok">已人工复核</span>';
  return '<span class="badge na">地域未标注</span>';
}
let rgQueue = [];
let rgSelected = "";
let rgBulkN = -1; // 最近一次干跑计数；执行时带回核对

function fillRegionReviewSelects() {
  const all = regionOpts || [];
  if (!all.length) return;
  $("#rg-region").innerHTML = '<option value="">（沿用机器建议；无建议时请选地区）</option>' + all.map((o) => {
    const label = o.level === 1 ? o.name : "\u3000\u3000" + o.name;
    return `<option value="${escapeHTML(o.code)}">${escapeHTML(label)}</option>`;
  }).join("");
  $("#rg-b-prov").innerHTML = '<option value="">全部省份</option>' + all.filter((o) => o.level === 1)
    .map((o) => `<option value="${escapeHTML(o.code)}">${escapeHTML(o.name)}</option>`).join("");
}

async function loadRegionStats() {
  try {
    const st = await app().RegionReviewStats();
    $("#rg-stats").innerHTML = `<span>法规总数 <b>${num(st.total)}</b></span>` +
      `<span>机器建议 · 待复核 <b>${num(st.suggest_pending)}</b></span>` +
      `<span>已人工确认 <b>${num(st.confirmed)}</b></span>` +
      `<span>已人工驳回/清除 <b>${num(st.left_blank)}</b></span>` +
      `<span>地域未标注 <b>${num(st.unmarked)}</b></span>`;
    setNavCount("#cnt-region", st.suggest_pending, false);
  } catch (e) { $("#rg-stats").textContent = "统计读取失败：" + (e.message || e); }
}

async function loadRegionQueue() {
  fillRegionReviewSelects();
  try {
    rgQueue = (await app().RegionReviewList($("#rg-filter").value.trim(), $("#rg-state").value, 300, 0)) || [];
    $("#rg-info").textContent = `队列 ${rgQueue.length} 条（最多显示 300）`;
    $("#rg-list").innerHTML = rgQueue.map((r) => `
      <div class="item ${r.law_id === rgSelected ? "sel" : ""}" data-law="${escapeHTML(r.law_id)}">
        <div class="t">${escapeHTML(r.name || r.law_id)} ${rgStateBadge(r.state, r.region_source)}</div>
        <div class="m">${escapeHTML(r.law_id)}${r.effect_level ? " · " + escapeHTML(r.effect_level) : ""}${r.issuer ? " · " + escapeHTML(r.issuer) : ""}</div>
        <div class="m">${r.region_path ? "当前标注：" + escapeHTML(r.region_path) : "当前标注：缺失/未知"}${r.region_reviewer ? "（" + escapeHTML(r.region_reviewer) + "）" : ""}</div>
      </div>`).join("") || '<p class="muted">队列为空（或过滤无匹配）。</p>';
    document.querySelectorAll("#rg-list .item").forEach((el) => el.addEventListener("click", () => showRegionReview(el.dataset.law)));
  } catch (e) { $("#rg-list").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}

async function showRegionReview(lawID) {
  rgSelected = lawID;
  document.querySelectorAll("#rg-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.law === lawID));
  try {
    const d = await app().RegionReviewDetail(lawID);
    const row = d.row || {}, s = d.suggest || {};
    $("#rg-preview").innerHTML = `<h3>${escapeHTML(row.name || lawID)}</h3>
      <p class="muted">${escapeHTML(lawID)}${row.effect_level ? " · " + escapeHTML(row.effect_level) : ""}${row.issuer ? " · 制定机关：" + escapeHTML(row.issuer) : ""}</p>
      <p><b>当前标注：</b>${row.region_code ? escapeHTML(row.region_path) + "（" + escapeHTML(row.region_code) + "）" : "缺失/未知"} ${rgStateBadge(row.state, row.region_source)}</p>
      ${row.region_code ? '<p class="muted">标注来源：' + escapeHTML(RG_SRC_TEXT[row.region_source] || ("未知来源 " + (row.region_source || ""))) + "</p>" : ""}
      <p><b>机器判定：</b>${escapeHTML(RG_CONF_TEXT[s.confidence] || s.confidence || "无")}${s.region_path ? " → " + escapeHTML(s.region_path) : ""}</p>
      <p class="muted">判定依据：${escapeHTML(s.note || "无")}${row.region_rule ? " · 现值依据：" + escapeHTML(row.region_rule) : ""}${row.region_reviewer ? " · 复核人：" + escapeHTML(row.region_reviewer) : ""}</p>
      ${row.state === "unmarked" && s.region_code ? '<p class="muted">该建议置信不足，机器未写入；是否采纳由人工决定。</p>' : ""}
      ${row.state === "suggest" ? '<p class="muted">' + (row.region_source === "pack" ? "随更新包携带的地域未经本库人工确认前不算已复核。" : "机器建议未经人工确认前不算已复核。") + "</p>" : ""}
      <h4>复核记录</h4><ul class="log">${(d.log || []).map((x) => "<li>" + escapeHTML(x) + "</li>").join("") || '<li class="muted">暂无</li>'}</ul>`;
    if (row.region_code) $("#rg-region").value = row.region_code;
  } catch (e) { $("#rg-preview").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}

async function judgeRegion(action) {
  if (!rgSelected) return toast("请先在左侧点选条目");
  const reviewer = "本机用户", reason = $("#rg-reason").value.trim();
  if (!reason) return toast("确认或驳回须填写结论依据（复核人自动记为本机用户）");
  const cur = rgQueue.find((x) => x.law_id === rgSelected) || {};
  const code = action === "confirm" ? ($("#rg-region").value || cur.region_code || "") : "";
  if (action === "confirm" && !code) return toast("该条目无机器建议，请在地区下拉中选择地区");
  try {
    const sum = await app().JudgeRegion([rgSelected], code, action, reviewer, reason);
    toast(`已处理：确认 ${num(sum.confirmed)} · 驳回 ${num(sum.rejected)} · 清除标注 ${num(sum.cleared)}`);
    loadRegionStats(); loadRegionQueue(); showRegionReview(rgSelected);
  } catch (e) { toast("处理失败：" + (e.message || e)); }
}
$("#rg-load").addEventListener("click", () => { loadRegionStats(); loadRegionQueue(); });
$("#rg-state").addEventListener("change", loadRegionQueue);
$("#rg-confirm").addEventListener("click", () => judgeRegion("confirm"));
$("#rg-reject").addEventListener("click", () => judgeRegion("reject"));
$("#rg-clear").addEventListener("click", () => judgeRegion("clear"));

async function rgBulkPreview() {
  rgBulkN = -1;
  try {
    const n = await app().RegionBulkPreview($("#rg-b-prov").value, $("#rg-b-verdict").value);
    rgBulkN = n;
    $("#rg-b-info").textContent = n > 0
      ? `会影响 ${num(n)} 条。核对无误后再点「执行批量处理」。`
      : "按所选条件没有可处理的条目，无需执行。";
  } catch (e) { $("#rg-b-info").textContent = "读取失败：" + (e.message || e); }
}
async function rgBulkApply() {
  if (rgBulkN < 0) return toast("请先点「先看看会影响多少条」，确认范围后再执行");
  const reviewer = "本机用户", reason = $("#rg-reason").value.trim();
  if (!reason) return toast("批量处理须填写结论依据（复核人自动记为本机用户）");
  try {
    const sum = await app().RegionBulkApply($("#rg-b-prov").value, $("#rg-b-verdict").value, reviewer, reason, rgBulkN);
    toast(`批量完成：确认 ${num(sum.confirmed)} · 驳回 ${num(sum.rejected)} · 清除标注 ${num(sum.cleared)}`);
    rgBulkN = -1;
    $("#rg-b-info").textContent = "已执行；若要再处理一批，请重新查看范围。";
    loadRegionStats(); loadRegionQueue();
  } catch (e) {
    rgBulkN = -1;
    $("#rg-b-info").textContent = "已中止：条目有变动或条件不符，请重新查看范围。";
    toast("批量失败：" + (e.message || e));
  }
}
$("#rg-b-preview").addEventListener("click", rgBulkPreview);
$("#rg-b-apply").addEventListener("click", rgBulkApply);

/* ---------- 专业 · 获取最新法规（状态 → 常用参数 → 高级参数 → 高级设置） ---------- */
async function loadSources() {
  try {
    const v = await app().Status();
    const lf = v.last_fetch || {};
    const srcs = [["govcn", "国家行政法规库"], ["govgz", "国家规章库"], ["govlocal", "地方规章"]];
    // 缺失如实说「尚未获取」并说明这意味着什么（不渲染占位数字）。
    $("#src-cards").innerHTML = srcs.map((s) => `<div class="card"><b>${escapeHTML(lf[s[0]] ? fmtWhen(lf[s[0]]).slice(0, 10) : "尚未获取")}</b><span>${s[1]}</span><span>${lf[s[0]] ? "最近获取 " + fmtWhen(lf[s[0]]) + "（本机时间）" : "本机还没有这个来源的法规"}</span></div>`).join("");
    const bs = await app().BrowserStatus();
    $("#browser-hint").textContent = bs.enabled
      ? "已连上辅助浏览工具，可以获取官方规章目录。"
      : (bs.guidance || "尚未连上辅助浏览工具，暂时不能获取官方规章目录。");
    $("#browser-tech").innerHTML = bs.technical ? `<span>${escapeHTML(bs.technical)}</span>` : "";
    const lists = (await app().GovcnLists()) || [];
    $("#g-list").innerHTML = lists.map((l) => `<option value="${escapeHTML(l.path)}">${escapeHTML(l.name || l.path)}</option>`).join("") || '<option value="">（本机还没有已导出的目录清单）</option>';
    $("#g-list-hint").textContent = lists.length ? "" :
      "没有目录清单就无法逐条获取：先在右侧「获取官方规章目录」拿到目录，或用随附的浏览器导出脚本把清单放进本机数据目录。";
  } catch (e) { $("#src-cards").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
let taskTimer = null;
function renderTasks(tasks) {
  const st = { running: "进行中", done: "完成", failed: "失败", cancelled: "已取消" };
  // 参数回显串属技术细节：只挂在 title 里，不在正文铺开（时间显示本机时间）。
  $("#task-list").innerHTML = (tasks || []).map((t) => `
    <div class="task ${escapeHTML(t.state)}"${t.params ? ' title="参数：' + escapeHTML(t.params) + '"' : ""}><b>${escapeHTML(KIND_TEXT[t.kind] || t.kind)} #${t.id}</b><span class="state">${st[t.state] || escapeHTML(t.state)}</span>
    <div class="m">${escapeHTML(t.progress || t.detail || "")}</div>
    <div class="m">${fmtWhen(t.started_at)}（本机时间）开始${t.ended_at ? " · " + fmtWhen(t.ended_at) + " 结束" : ""}</div></div>`).join("") || '<p class="muted">暂无任务记录。</p>';
  // 侧栏「获取最新法规」旁的指示灯：有任务在跑才亮，空闲时不占位。
  const dot = $("#dot-collect");
  const running = (tasks || []).some((t) => t.state === "running");
  if (dot) dot.hidden = !running;
  // 顶栏同步给出「正在获取」：获取任务在跑时，无论用户在哪个面板都能看见，
  // 不会误以为界面卡住或数据没变。
  const chip = $("#t-task");
  if (chip) chip.classList.toggle("hidden", !running);
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
      delay_sec: 2, limit: Number($("#g-limit").value) || 0,
      dry_run: $("#g-dry").checked, local: false
    });
    toast(`任务 #${info.id} 已开始`); loadTasks();
  } catch (e) { toast("无法开始：" + (e.message || e)); }
});
$("#btn-govlist").addEventListener("click", async () => {
  try {
    const info = await app().StartGovListTask({
      cat: $("#l-cat").value, max_pages: Number($("#l-pages").value) || 10,
      // 起始页、限速与空页停止属护栏参数：由程序固定，不在界面暴露（去技术化）。
      start_page: 1, delay_ms: 2500, empty_stop: 10
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
         <span>来源版本 ${escapeHTML(m.producer_version || "未知")} · 数据格式版本 ${escapeHTML(m.schema_version || "?")} · 许可 ${escapeHTML(m.license || "—")}</span>`
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
// 离线更新包：文件夹经系统选择器选取（手输路径属技术操作，不进界面）；
// 必须先校验，校验通过且来源受信才导入（后端强制），新条目一律进入待审核。
let packDir = "";
$("#btn-pack-pick").addEventListener("click", async () => {
  try {
    const dir = await app().PickUpdatePack();
    if (!dir) return; // 用户取消选择：保持原状，不谎报
    packDir = dir;
    const nm = dir.split(/[\\/]/).filter(Boolean).pop() || dir;
    $("#p-dir-name").textContent = "已选择更新包文件夹：" + nm + "（请先校验，再导入）";
  } catch (e) { toast("无法打开文件夹选择器：" + (e.message || e)); }
});
$("#btn-pack-verify").addEventListener("click", async () => {
  if (!packDir) return toast("请先点「选择更新包文件夹」");
  const out = await app().PackVerify(packDir);
  $("#p-dir-name").textContent = "校验结果：" + out;
  toast("校验结果：" + out.slice(0, 120));
});
$("#btn-pack-import").addEventListener("click", async () => {
  if (!packDir) return toast("请先选择并校验更新包文件夹");
  try { const t = await app().StartPackImportTask(packDir); toast(`更新包导入 #${t.id} 已开始（新条目进入待审核，由智能审核处理）`); loadTasks(); } catch (e) { toast("无法开始：" + (e.message || e)); }
});
async function loadSettings() {
  try {
    const v = await app().Status();
    $("#settings-box").innerHTML = `<span>数据存放位置：${escapeHTML(v.data_dir)}</span>
      <span>数据格式版本 ${escapeHTML(v.schema_version || "?")}（本程序支持 ${escapeHTML(v.schema_min)}–${escapeHTML(v.schema_max)}）</span>
      <span>数据来源版本：${escapeHTML(v.producer_version || "未知")}</span>`;
  } catch (e) { /* 已在别处报错 */ }
}

/* ---------- 启动 ---------- */
document.addEventListener("DOMContentLoaded", () => {
  applyTheme(); applyDensity(); applyReading(); applyIcons();
  loadManifest(); loadSettings();
  waitBindings(() => { applyMode(); loadRegion(); loadSideFoot(); loadRegionStats(); });
});

