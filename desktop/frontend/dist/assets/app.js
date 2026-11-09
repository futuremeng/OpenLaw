// app.js 「开源法律法规数据库」前端逻辑：
//   主工作区（默认）：查法规——关键词检索 / 按条件筛选 / 条文检索（修订沿革与引用核对）；
//   维护工作区：经左列底部「维护法规库」独立入口进入——库总览 / 智能审核（双智能体
//   自审，人工只处理例外纠正）/ 地域标引 / 获取最新法规 / 离线更新包；
//   初始化面板：空库启动默认落到「初始化法规库」，如实给出处境与两条获取路线。
// 三栏布局：左列导航与筛选、中列面板列表、右列共享详情（原文/条款/例外纠正）。
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
  box: '<path d="M4 8.5 12 4l8 4.5v7L12 20l-8-4.5z"/><path d="M4 8.5 12 13l8-4.5"/><path d="M12 13v7"/>',
  back: '<path d="M19 12H5"/><path d="M11 6l-6 6 6 6"/>',
  db: '<ellipse cx="12" cy="6.5" rx="7" ry="3"/><path d="M5 6.5v11c0 1.7 3.1 3 7 3s7-1.3 7-3v-11"/>',
  alert: '<path d="M12 4l8.5 16h-17z"/><path d="M12 10v4M12 17h.01"/>',
  refresh: '<path d="M20 12a8 8 0 1 1-2.9-6.2"/><path d="M20 4.5V9h-4.5"/>',
  chevron: '<path d="M6 9.5l6 6 6-6"/>',
  'chevron-right': '<path d="M9.5 6l6 6-6 6"/>',
  close: '<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>',
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
    const t = THEME_TEXT[theme] || THEME_TEXT.system;
    b.title = t;
    b.setAttribute("aria-label", t); // 图标按钮的唯一名称：title 不是可靠的可编程名称
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
  b.setAttribute("aria-label", "紧凑模式"); // 名称静态、开合状态由 aria-pressed 报
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
    b.setAttribute("aria-label", "阅读行宽");
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

/* ---------- 工作区与导航 ----------
   主工作区（查法规）与维护工作区（维护法规库）完整互斥切换：
   进入维护时主导航整套收起、只留维护子导航（首位常驻「返回查法规」出口）；
   回到查询时维护子导航整套收起。左列任何时刻只呈现一套导航
   （docs/design-ui-language.md：主导航只谈「查法规」）。 */
let ws = localStorage.getItem("ol-ws") === "maint" ? "maint" : "main";
const MAIN_NAV = ["nav-group-main", "nav-home", "nav-browse", "nav-search", "nav-verify"];
const MAINTAIN_NAV = ["nav-group-maintain", "nav-exit-maintain", "nav-overview",
  "nav-review", "nav-region", "nav-subject", "nav-collect", "nav-pack"];
// 顶栏面包屑：当前位置 + 当前生效的筛选摘要。用户随时能看到「自己在哪、看的是不是筛过的」。
// 维护工作区内面包屑根改报「维护法规库」——位置如实反映工作区。
const PANEL_TITLE = {
  home: "关键词检索", browse: "按条件筛选", search: "条文检索", verify: "核验引用", setup: "初始化法规库",
  overview: "库总览", review: "智能审核", region: "地域标引", subject: "部门法标引", collect: "获取最新法规",
  pack: "离线更新包",
};
function setCrumb(p) {
  const root = $("#crumb-root");
  if (root) root.textContent = ws === "maint" ? "维护法规库" : "法律法规库";
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
  if (p === "verify") { const t = $("#v-text"); if (t && !t.value) t.focus(); }
  if (p === "setup") loadSetup();
  if (p === "overview") loadOverview();
  if (p === "review") loadReview();
  if (p === "region") { loadRegionStats(); loadRegionQueue(); rgBulkLock(); } // 回到面板，旧预览范围作废
  if (p === "subject") { loadSubjectStats(); loadSubjectQueue(); sjBulkLock(); } // 同上：部门法标引
  if (p === "collect") { loadSources(); loadTasks(); loadTasksTick(); }
  if (p === "pack") loadPackPanel();
}
// 重新加载当前可见面板（地区等全局偏好变更后需要刷新列表）。
function reloadViewing() {
  const cur = document.querySelector(".nav.active");
  if (cur) showPanel(cur.dataset.panel);
}
function applyWorkspace() {
  // 完整切换：两套导航互斥——查询态只见主导航，维护态只见维护子导航；
  // 底部维护入口在维护态收起（出口是子导航首位的「返回查法规」，不两处各一套）。
  MAIN_NAV.forEach((id) => { const el = $("#" + id); if (el) el.hidden = ws === "maint"; });
  MAINTAIN_NAV.forEach((id) => { const el = $("#" + id); if (el) el.hidden = ws !== "maint"; });
  const entry = $("#nav-maintain");
  if (entry) entry.hidden = ws === "maint" || edition === "reader";
  // 只读查询版：不给写路径的入口（复核/标引/采集面板在维护组内，整组收起；
  // 取数与库总览仍可达——只读用户同样要能装包与查看本机数据状态）。
  if (edition === "reader") {
    ["nav-review", "nav-region", "nav-subject", "nav-collect"].forEach((id) => { const el = $("#" + id); if (el) el.hidden = true; });
  }
}
// 发行版档位（-edition reader|full）：reader 隐藏维护入口，full 全量。
// 隐藏的是入口不是能力——后端方法仍在，CLI 与消费方 SDK 不受影响。
let edition = "full";
async function applyEdition() {
  try {
    const e = await app().AppEdition();
    edition = (e && e.code) || "full";
    // 只读查询版没有维护工作区：上次留下的维护态必须回落查询态，
    // 否则两套导航都被收起，用户会卡在没有任何导航的界面上。
    if (edition === "reader" && ws === "maint") {
      ws = "main";
      localStorage.setItem("ol-ws", ws);
    }
    applyWorkspace();
    return e;
  } catch (e) { return null; }
}
function enterMaintain(p) {
  ws = "maint";
  localStorage.setItem("ol-ws", ws);
  applyWorkspace();
  showPanel(p || "overview");
}
function exitMaintain() {
  ws = "main";
  localStorage.setItem("ol-ws", ws);
  applyWorkspace();
  showPanel("home");
}
document.querySelectorAll(".nav").forEach((b) => b.addEventListener("click", () => showPanel(b.dataset.panel)));
$("#nav-maintain").addEventListener("click", () => enterMaintain("overview"));
$("#nav-exit-maintain").addEventListener("click", exitMaintain);
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
const KIND_TEXT = { "govcn-import": "获取官方原文", "govlist-browse": "获取官方规章目录", "eu-text": "获取欧盟官方全文", "pack-build": "生成数据包", "pack-verify": "校验数据包", "pack-import": "导入数据包", "pack-install": "安装数据包", "agent-review": "智能审核" };
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
let detailOpener = null; // 键盘与读屏的来路：关闭详情后焦点必须回到打开它的那一行
function showDetail(title) {
  const hidden = $("#detail").classList.contains("hidden");
  const a = document.activeElement;
  const fromRow = !!(a && a.matches && a.matches("button.item, button.lawcard"));
  if (hidden) detailOpener = fromRow ? a : null;
  $("#detail").classList.remove("hidden");
  $("#detail-title").textContent = title || "详情";
  const b = $("#detail-body");
  b.innerHTML = "";
  applyReading();
  // 条目行是按钮后，键盘激活必须能把焦点带进右列——否则焦点仍留在行上，
  // 详情既读不到也滚不动（preventScroll 避免顺带把页面弹走）。
  if (hidden && fromRow) $("#detail").focus({ preventScroll: true });
  return b;
}
function hideDetail() {
  $("#detail").classList.add("hidden");
  $("#fix-bar").classList.add("hidden");
  if (detailOpener && document.contains(detailOpener)) detailOpener.focus();
  detailOpener = null;
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
// 窄窗口下详情是浮层（≤1150，见 style.css 响应式分档）——Esc 必须能关，
// 浮层不得只有 ✕ 一个出口。
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !$("#detail").classList.contains("hidden")) hideDetail();
});


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
  // 进行中锁定：授权弹窗与坐标获取最长可达一分钟，连点会产生并发轮询
  // （多套计时各自等待，界面状态反而失焦）。出口统一在 finally。
  const btn = $("#btn-locate");
  btn.disabled = true;
  try {
    await locateFlow();
  } finally {
    btn.disabled = false;
  }
});
async function locateFlow() {
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
}
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
// browseFilter 是浏览页唯一的筛选状态。日常浏览常用「效力等级 + 地区」两个维度，
// 需要时可再叠加复核状态、来源、公布年代——同一套条件、同一条 SQL 口径
// （Go 侧 BrowseLawList 与 BrowseFacets 共用 browseWhere，计数与列表不会两说）。
const browseFilter = { level: "", juris: "", confirm: "", source: "", year: "", region: "", subject: "", timeliness: "" };

function browseArgs(limit, offset) {
  return {
    level: browseFilter.level, juris: browseFilter.juris, confirm: browseFilter.confirm, source: browseFilter.source,
    year: browseFilter.year, region: browseFilter.region, subject: browseFilter.subject, timeliness: browseFilter.timeliness,
    kw: $("#b-kw").value.trim(), limit: limit || 300, offset: offset || 0,
  };
}

// 分面组：可折叠。组头给出「本维度已选什么 / 共有多少可选值」，折叠状态记住——
// 年代、来源这类长维度默认收起，常用的类别与地区默认展开（筛选栏不必长成一条竖河）。
const GROUP_DEFAULT = { level: true, juris: true, confirm: true, year: false, source: false, region: true, subject: true, timeliness: true };
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
  facetChips("#f-juris", "juris", v.juris);
  facetChips("#f-levels", "level", v.levels);
  facetChips("#f-subjects", "subject", v.subjects, 10);
  facetChips("#f-regions", "region", v.regions, 6, (val) => (browseFilter.region || "").slice(0, 2) === val);
  facetChips("#f-years", "year", v.years, 6);
  // 时效分面（v11）：语料版本不含 law_status 视图（只读消费 v10 语料）时如实缺席——
  // 收起可选值、脚注说明原因，不得渲染一列全「未知」的假分面。
  const tg = $("#g-timeliness");
  if (tg) tg.hidden = !v.has_timeliness;
  const tnote = $("#f-timeliness-note");
  if (tnote) {
    tnote.textContent = v.has_timeliness ? "" : "法规库数据版本不含时效信息，该维度暂不可筛。";
    tnote.classList.toggle("hidden", !!v.has_timeliness);
  }
  if (v.has_timeliness) facetChips("#f-timeliness", "timeliness", v.timeliness);
  facetChips("#f-confirms", "confirm", v.confirms);
  facetChips("#f-sources", "source", v.sources, 5);
  ["juris", "level", "subject", "region", "year", "timeliness", "confirm", "source"].forEach(facetGroup);
  const note = $("#f-region-note");
  if (note) {
    note.textContent = v.unlabeled
      ? `另有 ${num(v.unlabeled)} 部未标注地区码，按法规名称中的地名匹配（上方下拉可选到具体地级）`
      : "当前条件下的条目均已标注地区";
  }
  $("#f-total").textContent = num(v.total);
  // 计数到手才把「多少部符合」这句话交出去：进行中与失败都不许留一个像数值的
  // 占位符在句子里（诚实展示纪律——缺失要写成话，不是写成破折号）。
  const fsumOk = $("#f-sum-ok"), fsumState = $("#f-sum-state");
  if (fsumOk) fsumOk.classList.remove("hidden");
  if (fsumState) fsumState.classList.add("hidden");
  $("#b-stats").textContent = activeFilterText();
  setCrumb("browse");
}

// activeFilterText 把当前生效的筛选回显成一句白话——用户必须能看到
// 「现在看到的是被筛过的」，而不是以为在看全库。
function activeFilterText() {
  const parts = [];
  if (browseFilter.level) parts.push("类别 " + browseFilter.level);
  if (browseFilter.juris) parts.push("法域 " + { CN: "中国", EU: "欧盟", US: "美国", none: "未标注" }[browseFilter.juris]);
  if (browseFilter.confirm) parts.push("复核状态 " + { confirmed: "已复核确认", pending: "待复核", rejected: "已驳回" }[browseFilter.confirm]);
  if (browseFilter.year) parts.push("公布年代 " + (browseFilter.year === "none" ? "缺失" : browseFilter.year + " 年代"));
  if (browseFilter.source) parts.push("来源 " + browseFilter.source);
  if (browseFilter.subject) {
    const hit = ((lastFacets || {}).subjects || []).find((o) => o.value === browseFilter.subject);
    parts.push("部门法 " + ((hit && hit.label) || browseFilter.subject));
  }
  if (browseFilter.timeliness) parts.push("时效状态 " + browseFilter.timeliness);
  if (browseFilter.region) parts.push("地区 " + (($("#region-city").selectedOptions[0] || {}).text || regionPathOf(browseFilter.region) || browseFilter.region));
  const kw = $("#b-kw").value.trim();
  if (kw) parts.push("名称含「" + kw + "」");
  return parts.length ? "当前筛选：" + parts.join(" · ") : "未设筛选条件（显示全库）";
}
function regionPathOf(code) {
  const hit = (regionOpts || []).find((o) => o.code === code);
  return hit ? hit.name : "";
}

// lawsSeq：取列请求的序号。关键词防抖、分面连点、面板切换都可能让多份请求在途，
// 先发后到的旧响应不得覆盖新条件的结果——终笔按序号判定（旧版会随机覆盖，
// 用户看到「筛的是 A、列里是 B」）。
let lawsSeq = 0;
async function loadLaws() {
  const seq = ++lawsSeq;
  // 进行状态就地给出：改完条件不得让界面停在旧列表上，让人以为「筛选没生效」。
  // 计数区是「显示 N / 共 M」的家——忙态与结果在同一处交接。
  const pc0 = $("#b-pane-cnt");
  if (pc0) pc0.innerHTML = "正在取列…";
  const args = browseArgs(300, 0);
  try {
    const [rows, v] = await Promise.all([app().BrowseLawList(args), app().BrowseFacets(args)]);
    if (seq !== lawsSeq) return; // 过期响应迟到的那一份：丢弃，不覆盖新结果
    renderFacets(v || {});
    const list = rows || [];
    const pc = $("#b-pane-cnt");
    if (pc) {
      // 上限之外的说明必须可见：title 对键盘与触屏完全不可达（WCAG 1.4.13），
      // 而「屏上不是全部」正是会让人误判的那一句。
      const capped = list.length >= 300;
      pc.innerHTML = `显示 <b>${num(list.length)}</b> / 共 <b>${num((v || {}).total)}</b> 部` +
        (capped ? '<span class="capnote">已列出前 300 部，用左侧条件收窄可看到更多</span>' : "");
      pc.title = capped ? "本次列出前 300 部，其余请用条件收窄" : "已列出当前条件下的全部条目";
    }
    $("#law-list").innerHTML = list.length ? list.map((r) => `
      <button type="button" class="item${r.confirm_status === "confirmed" ? "" : " pend"}" data-law="${escapeHTML(r.law_id)}" data-name="${escapeHTML(r.name)}">
        <span class="t">${escapeHTML(r.name)} ${statusBadge(r.confirm_status, r.confirm_source)} ${validBadge(r.effective_date, r.expire_date)}</span>
        <span class="m">${rowMeta(r)}</span>
      </button>`).join("") :
      '<p class="muted">' + (browseFilter.region
        ? "当前条件下没有法规。已标注地区的按区划码匹配、未标注的按名称中的地名匹配；两者都判不出地区的条目不会出现在按地区筛选的结果中——可点「清空筛选」，或改用上方下拉。"
        : "当前条件下没有法规。可点「清空筛选」，或放宽名称关键词。") + "</p>";
    document.querySelectorAll("#law-list .item").forEach((el) =>
      el.addEventListener("click", () => openLaw(el.dataset.law, el.dataset.name)));
  } catch (e) {
    if (seq !== lawsSeq) return; // 过期请求的失败同样不得覆盖新结果
    $("#law-list").innerHTML = '<p class="bad">读取失败：' + escapeHTML(e.message || e) + "</p>";
    // 进行态是承诺：失败就必须改口，不得让面板头永远停在「正在取列…」。
    const pcF = $("#b-pane-cnt");
    if (pcF) pcF.innerHTML = "取列未完成，原因见下方列表";
    // 计数句同样不得停在「正在统计」或留个占位符：失败要说失败，去处就在下方。
    const fsumState = $("#f-sum-state"), fsumOk = $("#f-sum-ok");
    if (fsumState) {
      fsumState.textContent = "计数未能读取，失败原因见下方列表";
      fsumState.classList.remove("hidden");
    }
    if (fsumOk) fsumOk.classList.add("hidden");
  }
}

/* ---------- 主工作区 · 关键词检索（搜索为中心） ---------- */
async function loadHome() {
  try {
    const v = await app().Status();
    // 空库如实说明并给出补救去处（诚实展示纪律：不渲染空搜索界面装作一切正常）。
    const empty = !v.open_err && Number(v.laws || 0) === 0;
    const he = $("#h-empty");
    if (he) he.classList.toggle("hidden", !empty);
    const levels = ["行政法规", "部门规章", "地方政府规章"];
    let html = levels.map((lv) => {
      const r = (v.by_level || []).reduce((acc, x) => (x && x.level === lv)
        ? { pending: acc.pending + (x.pending || 0), confirmed: acc.confirmed + (x.confirmed || 0), rejected: acc.rejected + (x.rejected || 0) }
        : acc, { pending: 0, confirmed: 0, rejected: 0 });
      return `<button class="card big" data-level="${lv}"><b>${num(r.pending + r.confirmed + r.rejected)}</b><span>${lv}</span><span class="muted">已复核 ${num(r.confirmed)}</span></button>`;
    }).join("");
    const lf = Object.values(v.last_fetch || {}).sort().reverse()[0];
    if (lf) html += `<div class="card big"><b>${escapeHTML(lf.slice(0, 10))}</b><span>数据最近更新</span></div>`;
    $("#h-cards").innerHTML = html;
    // 首页卡片即入口：点类别直接进浏览页并带上该筛选，数字与筛选当场对上。
    document.querySelectorAll("#h-cards .card[data-level]").forEach((el) => el.addEventListener("click", () => {
      browseFilter.level = el.dataset.level;
      browseFilter.juris = browseFilter.confirm = browseFilter.source = browseFilter.year = "";
      browseFilter.subject = browseFilter.timeliness = "";
      showPanel("browse");
    }));
  } catch (e) { /* 首页卡片失败不阻塞搜索 */ }
}
async function homeSearch() {
  const q = $("#hq").value.trim();
  if (!q) return;
  $("#h-results").innerHTML = '<p class="muted">正在检索…</p>';
  try {
    const v = await app().SearchGuided(q, 20);
    const rows = v.rows || [];
    // 封顶如实显示：首页只列前 20 条——到顶必须给出出口，不得长成死路
    // （与初始化面板「只给去处」同一精神；条文检索面板列 50 条并带收窄筛选）。
    const more = rows.length >= 20
      ? '<p class="muted">仅列出前 20 条命中。<button id="h-more" class="ok">到条文检索查看全部 →</button></p>'
      : "";
    $("#h-results").innerHTML = rows.length ? rows.map((r) => `
      <button type="button" class="lawcard" data-doc="${escapeHTML(r.doc_id)}">
        <span class="t">${escapeHTML(r.law_name)} <span class="pill">${escapeHTML(r.level)}</span> ${statusBadge(r.confirm_status)} ${validBadge(r.effective_date, r.expire_date)}</span>
        <span class="snip">${escapeHTML(r.snippet)}</span>
        <span class="m">${escapeHTML(r.article_no)}${r.effective_date ? " · " + r.effective_date + " 起施行" : ""} · <b class="view">查看原文 →</b></span>
      </button>`).join("") + more : guideBlock(v.guidance, "没有找到相关条文。");
    document.querySelectorAll("#h-results .lawcard").forEach((el) =>
      el.addEventListener("click", () => openPreview(el.dataset.doc)));
    const hm = $("#h-results").querySelector("#h-more");
    if (hm) hm.addEventListener("click", () => { $("#q").value = q; showPanel("search"); doSearch(); });
    bindGuideJump("#h-results");
  } catch (e) {
    $("#h-results").innerHTML = '<p class="bad">检索失败：' + escapeHTML(e.message || e) + "</p>";
  }
}
// guideBlock 检索的诚实出路（P1-4）：为什么没找到 + 现在能做什么。
// 边界不变：不重排序、不做同义改写；给出的每一条都必须是可照做的动作或库内事实。
function guideBlock(g, lead) {
  const reasons = (g && g.reasons) || [];
  const sugg = (g && g.suggestions) || [];
  if (!reasons.length && !sugg.length) return '<p class="muted">' + escapeHTML(lead || "") + "</p>";
  return '<div class="guide"><p class="t">' + escapeHTML(lead || "本次检索的说明") + "</p>" +
    (reasons.length ? '<div class="k">为什么</div><ul>' + reasons.map((x) => "<li>" + escapeHTML(x) + "</li>").join("") + "</ul>" : "") +
    (sugg.length ? '<div class="k">可以怎么做</div><ul>' + sugg.map((x) => "<li>" + escapeHTML(x) + "</li>").join("") + "</ul>" : "") +
    "</div>";
}
// 说明里的「按该法规浏览」是库内事实（别名指向），给出可点的跳转。
function bindGuideJump(sel) {
  document.querySelectorAll(sel + " .guide [data-jump-law]").forEach((el) =>
    el.addEventListener("click", () => { browseFilter.juris = ""; showPanel("browse"); }));
}
$("#btn-hsearch").addEventListener("click", homeSearch);
$("#hq").addEventListener("keydown", (e) => { if (e.key === "Enter") homeSearch(); });
$("#h-setup").addEventListener("click", () => showPanel("setup"));

/* ---------- 初始化法规库（新装/空库引导 · 空库启动默认落这里） ----------
   纪律：状态如实读、如实报——库空就说空，通道未配置就展示指引；
   「开始查法规」只在确有条目后才成为主行动。获取控件不在这里重复，
   一律跳到唯一落点（离线更新包 / 获取最新法规，均在维护工作区）。 */
async function loadSetup() {
  try {
    const v = await app().Status();
    const n = Number(v.laws || 0);
    const go = $("#su-go-home");
    if (v.open_err) {
      $("#su-status").textContent = "本机状态读取失败：" + v.open_err + "——请先修复数据目录访问，再完成初始化。";
      if (go) go.disabled = true;
    } else if (n > 0) {
      $("#su-status").textContent = "初始化已完成：本机法规库已有 " + num(n) + " 部法规、" +
        num(v.articles) + " 条条文。如需补充数据，请到「维护法规库」。";
      if (go) go.disabled = false;
    } else {
      $("#su-status").textContent = "本机法规库还是空的（0 部法规）。选择下面任一方式获取数据。";
      if (go) go.disabled = true; // 空库不得装作可查询（诚实展示：主行动不能指向空结果）
    }
  } catch (e) { $("#su-status").textContent = "本机状态读取失败：" + (e.message || e); }
  loadSetupAgent();
}
async function loadSetupAgent() {
  try {
    const st = await app().AgentStatus();
    $("#su-agent").textContent = st.configured
      ? "智能审核通道已就绪" + (st.model ? "（模型 " + st.model + "）" : "") + "：获取数据后可自动核对新条目。"
      : (st.guidance || "智能审核尚未配置：不配置也能查询与人工纠正，只是新条目会保持待复核。");
  } catch (e) { $("#su-agent").textContent = "通道状态读取失败：" + (e.message || e); }
}
// 获取路线的选择要落进本机台账（首启漏斗的一步；只记路线名，不记任何内容）。
$("#su-go-pack").addEventListener("click", () => { app().SetupRouteChosen("pack").catch(() => {}); enterMaintain("pack"); });
$("#su-go-collect").addEventListener("click", () => { app().SetupRouteChosen("collect").catch(() => {}); enterMaintain("collect"); });
$("#su-go-home").addEventListener("click", () => showPanel("home"));

/* ---------- 初始化 · 官方数据包目录（GitHub Releases 分发通道） ----------
   纪律：只有用户按下「刷新官方目录」才联网；目录验签 + 归档哈希复核 + 包级验签
   三道闸全部在 Go 侧执行，失败原样展示——不渲染空目录冒充可用，不假装已就绪。 */
let suCatalog = null;
async function loadPackCatalog() {
  const info = $("#su-catalog-info");
  info.textContent = "正在获取官方目录…";
  try {
    const c = await app().PackCatalogRefresh();
    const packs = (c && c.packs) || [];
    if (!packs.length) {
      suCatalog = null;
      $("#su-catalog").innerHTML = '<p class="muted">目录中暂无可安装的数据包。</p>';
      info.textContent = "目录已获取：暂无数据包。";
      $("#su-install").disabled = true;
      return;
    }
    suCatalog = c;
    // 主视图禁止原始 ISO UTC（docs/design-ui-language.md）：显示本机时间，UTC 原串留 title。
    const when = c.generated_at ? fmtWhen(c.generated_at) : "时间未知";
    info.textContent = "目录已更新（" + when + " · " + num(packs.length) + " 个包）。勾选需要的包再安装。";
    info.title = c.generated_at ? "原始时间（UTC）：" + c.generated_at : "";
    renderSuCatalog();
  } catch (e) {
    suCatalog = null;
    $("#su-catalog").innerHTML = '<p class="muted">未获取到目录。</p>';
    info.textContent = "目录获取失败：" + (e.message || e) + "——可稍后重试，或用下方其他方式。";
    $("#su-install").disabled = true;
  }
}
function suPackLabel(p) {
  if (p.kind === "delta") return "增量包 · " + (p.since || "?") + " 至 " + (p.until || "?") + " 公布";
  const rs = (p.regions || []).map((r) => (r === "CN" ? "全国" : "区划 " + r));
  return "基础包 · " + (rs.length ? rs.join("、") : "全部地域");
}
function suFmtMB(n) { return ((Number(n) || 0) / 1048576).toFixed(1) + " MB"; }
function renderSuCatalog() {
  $("#su-catalog").innerHTML = (suCatalog.packs || []).map((p) =>
    `<label class="pack-row"><input type="checkbox" data-pack="${escapeHTML(p.pack_id)}"/><b>${escapeHTML(suPackLabel(p))}</b><span class="muted">${num(p.doc_count)} 条 · ${suFmtMB(p.size)}</span></label>`).join("");
  updateSuSel();
}
function suSelected() {
  return [...document.querySelectorAll("#su-catalog input[data-pack]")]
    .filter((b) => b.checked).map((b) => b.getAttribute("data-pack"));
}
function updateSuSel() {
  const ids = suSelected();
  const btn = $("#su-install");
  btn.disabled = !ids.length;
  if (!ids.length) { $("#su-sel").textContent = "未勾选数据包。"; return; }
  let docs = 0, size = 0;
  for (const p of ((suCatalog && suCatalog.packs) || [])) {
    if (ids.includes(p.pack_id)) { docs += Number(p.doc_count) || 0; size += Number(p.size) || 0; }
  }
  $("#su-sel").textContent = `已勾选 ${ids.length} 个包（约 ${num(docs)} 条 · ${suFmtMB(size)}）；` +
    "逐包安装，每包都会校验完整性与来源。";
}
$("#su-catalog-refresh").addEventListener("click", loadPackCatalog);
$("#su-catalog").addEventListener("change", updateSuSel);
$("#su-install").addEventListener("click", async () => {
  const ids = suSelected();
  if (!ids.length) { toast("请先勾选要安装的数据包"); return; }
  app().SetupRouteChosen("pack").catch(() => {}); // 首启漏斗：选了哪条获取路线（只记路线名）
  try {
    const t = await app().StartRemoteInstallTask(ids);
    toast(`安装任务 #${t.id} 已开始（逐包获取与校验，进度见任务列表）`);
    loadTasks(); loadTasksTick();
  } catch (e) { toast("无法开始：" + (e.message || e)); }
});

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
    const where = [];
    if (!lv) where.push("「智能审核」核对效力等级");
    if (!r.region) where.push("「地域标引」补地区");
    parts.push('<span class="miss">未标注 ' + miss.length + " 项（" + miss.join("、") + "）" +
      (where.length ? "——可在" + where.join("、") : "") + "</span>");
  }
  return parts.join(" · ");
}
// openLaw 右列详情：法规信息卡（§6 字段序）+ 修订沿革 + 条款列表（点条款看留证原文，可返回条款列表）。
async function openLaw(lawID, name) {
  document.querySelectorAll("#law-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.law === lawID));
  const [vs, arts, meta] = await Promise.all([app().LawVersions(lawID), app().BrowseArticles(lawID), app().LawMeta(lawID)]);
  const tl = vs.map((x) => `<div class="tl-row"><b>${escapeHTML(x.version_no)}</b> ${escapeHTML(x.effective)} 起施行${x.expire ? "，" + escapeHTML(x.expire) + " 失效" : "，现行有效"} ${statusBadge(x.status)}</div>`).join("");
  const list = arts.map((x) => `<button type="button" class="item art" data-doc="${escapeHTML(x.doc_id)}" data-no="${escapeHTML(x.article_no)}"><span class="t">${escapeHTML(x.article_no)}</span><span class="m">${escapeHTML(x.snippet)}…</span></button>`).join("");
  const box = showDetail(name || "法规详情");
  box.innerHTML = `<h3>${escapeHTML(name)}</h3>${lawMetaCard(meta || {})}
    <h4>修订沿革</h4><div class="tl">${tl || '<p class="muted">暂无版本记录。</p>'}</div>
    <h4>条款（${arts.length} 条）</h4>${list || '<p class="muted">该法规暂未收录分条文本（可在「查法规」中全文检索）。</p>'}`;
  document.querySelectorAll("#detail-body .art").forEach((el) =>
    el.addEventListener("click", () => openPreview(el.dataset.doc, el.dataset.no, () => openLaw(lawID, name))));
  // 信息卡「到部门法标引处理 →」必须可点（给出的每条出路都要能照做）。
  const sj = box.querySelector('[data-jump="subject"]');
  if (sj) sj.addEventListener("click", () => showPanel("subject"));
}

// 时效状态徽章——五态枚举集中一处（lawtext statusClassMap 的教训：别在多处映射）。
// 语料版本不含时效视图时如实显示原因，不得当作「现行有效」或渲染假数据。
const TIMELINESS_BADGE = {
  现行有效: ["ok", "现行有效"],
  已修改: ["na", "已修改（以新版本为准）"],
  已废止: ["no", "已废止"],
  尚未生效: ["pend", "尚未生效"],
  未知: ["na", "时效未知"],
};
function timelinessBadge(t, has) {
  if (!has) return '<span class="badge na" title="law_status 视图缺失，无法派生时效">未知（法规库数据版本不含时效信息）</span>';
  const b = TIMELINESS_BADGE[t] || TIMELINESS_BADGE.未知;
  return `<span class="badge ${b[0]}">${b[1]}</span>`;
}

// lawMetaCard 条目页法规信息卡（docs/design-taxonomy.md §6 字段序）：制定机关｜公布日期｜
// 施行日期｜时效状态徽章｜效力层级｜部门法（含机器建议/已复核标记）｜文档形态｜来源链接。
// 缺失一律如实显示「未标注/缺失」并给补救去处，不渲染占位内容。
function lawMetaCard(m) {
  const kv = (k, v) => `<span><b>${k}</b>${v}</span>`;
  const miss = '<span class="muted">缺失</span>';
  const subj = !m.subject_name
    ? '未标注 <a class="view" data-jump="subject">到部门法标引处理 →</a>'
    : escapeHTML(m.subject_name) + (m.subject_state === "suggest" ? ' <span class="badge pend">机器建议 · 待复核</span>'
      : m.subject_state === "reviewed" ? ' <span class="badge ok">已人工复核</span>' : "");
  return '<div class="kv">' +
    kv("制定机关", m.issuer ? escapeHTML(m.issuer) : miss) +
    kv("公布日期", m.publish_date ? escapeHTML(m.publish_date) : miss) +
    kv("施行日期", m.effective_date ? escapeHTML(m.effective_date) : miss) +
    kv("时效状态", timelinessBadge(m.timeliness, m.has_timeliness)) +
    kv("效力层级", m.level ? escapeHTML(m.level) : miss) +
    kv("部门法", subj) +
    kv("文档形态", m.doc_form_name ? escapeHTML(m.doc_form_name) : "形态未判定") +
    kv("来源链接", m.source_url ? `<a href="${escapeHTML(m.source_url)}" rel="noreferrer">官方原文</a>` : "来源链接缺失") +
    "</div>";
}
$("#b-refresh").addEventListener("click", loadLaws);
// 清空筛选：维度一律复位；地区是全局偏好，一并取消，避免下一次浏览又被悄悄筛掉。
$("#btn-filter-clear").addEventListener("click", () => {
  browseFilter.level = browseFilter.juris = browseFilter.confirm = browseFilter.source = browseFilter.year = "";
  browseFilter.subject = browseFilter.timeliness = "";
  if (browseFilter.region) { applyRegion(""); return; }
  loadLaws();
});
// 名称关键词即时联动：停手 400ms 再查库，既不逐字打库，也不必按回车。
let kwTimer = null;
$("#b-kw").addEventListener("input", () => { clearTimeout(kwTimer); kwTimer = setTimeout(loadLaws, 400); });
$("#b-kw").addEventListener("keydown", (e) => { if (e.key === "Enter") { clearTimeout(kwTimer); loadLaws(); } });

// 法规构成给出「形状」而不只是数字——每级一条堆叠条，段长按条目数比例，
// 条长之间按最大等级归一（哪一级占多少一眼可辨，跨法域可比）。
// 精确值仍在「各等级的精确数字」折叠里。
// 法域文案映射（CN/EU/US 是技术标识，界面一律法言法语；空=法域未标注）。
const JURIS_TEXT = { CN: "中国", EU: "欧盟", US: "美国", "": "法域未标注" };
// renderJuris：分法域与构成合并为一个区块——一个法域一个块（jgroup）：
// 块头 = 法域名 + 部数 + 该法域待复核 + 下钻入口；块体 = 该法域按效力等级的构成条。
// 计数与构成同一口径联动（诚实展示纪律）：0 部如实「尚未收录」+ 补救指引；
// 有部数但无留证条文如实说明，不得凭空出条，也不得说「均已复核」（0 条文恒真是空话）。
const JURIS_HINT = {
  CN: "尚未收录。可在「初始化法规库」安装官方数据包，或在「获取最新法规」从官方渠道获取。",
  EU: "尚未收录。可在「获取最新法规 → 获取欧盟官方全文」按法规编号获取（条目须先经欧盟官方出版通道建立）。",
  US: "尚未收录。美国联邦法规可经命令行从官方批量数据包导入（openlaw-us），界面通道尚未开通。",
  "": "请核对条目来源后补标法域。",
};
function renderJuris(rows, byLevel, reps, revs) {
  const el = $("#ov-juris");
  if (!el) return;
  const m = {};
  (rows || []).forEach((r) => { if (r) m[r.code || ""] = r; });
  const totalOf = (r) => (r.pending || 0) + (r.confirmed || 0) + (r.rejected || 0);
  const groups = {};
  (byLevel || []).forEach((r) => {
    if (r && totalOf(r) > 0) { const k = r.juris || ""; (groups[k] = groups[k] || []).push(r); }
  });
  const max = (byLevel || []).reduce((acc, r) => (r ? Math.max(acc, totalOf(r)) : acc), 0) || 1; // 全局归一：跨法域可比
  const seg = (cls, n) => (n ? `<i class="${cls}" style="width:${((n / max) * 100).toFixed(2)}%"></i>` : "");
  const R = (reps || []).filter((r) => r), V = (revs || []).filter((r) => r);
  const codes = ["CN", "EU", "US"];
  const hasRec = (c) => R.some((r) => (r.juris || "") === c) || V.some((r) => (r.juris || "") === c);
  if (groups[""] || (m[""] && m[""].laws > 0) || hasRec("")) codes.push("");
  // 列宽随数据状态走：恰好一个法域有构成条 → 该位置宽列（空列不值得占宽）；
  // 两个及以上有 → 等宽并列（将来欧盟/美国条文进来，宽度自动跟上）。
  const barred = codes.map((c) => (groups[c] || []).length);
  const solo = barred.filter((n) => n > 0).length === 1 ? barred.findIndex((n) => n > 0) : -1;
  el.className = "juris" + (codes.length === 3 && barred.length === 3 && solo >= 0 ? " solo-" + (solo + 1) : "");
  let html = "";
  for (const c of codes) {
    const r = m[c] || { laws: 0, documents: 0, pending: 0 };
    const laws = r.laws || 0, docs = r.documents || 0;
    const bars = (groups[c] || []).slice().sort((a, b) => totalOf(b) - totalOf(a));
    let head;
    // 实验通道如实标注（P1-5）：欧盟/美国是并行验证的通道，不与主战场并列叙事；
    // 空桶仍然如实显示 0，但收在折叠里，不占主视图注意力。
    const exp = c === "EU" || c === "US";
    const expTag = exp ? ' <span class="pill">实验通道</span>' : "";
    if (laws > 0) {
      const stat = docs > 0
        ? ((r.pending || 0) > 0 ? `待复核 ${num(r.pending)} 条` : "均已复核")
        : "尚无留证条文";
      head = `<button class="jhead" data-juris="${c}">` +
        `<b>${JURIS_TEXT[c]}</b>${expTag}<span class="num">${num(laws)} 部</span>` +
        `<span class="muted">${stat} · 点击查看全部 ${icon("chevron-right")}</span></button>`;
    } else {
      head = `<div class="jhead off"><b>${JURIS_TEXT[c]}</b>${expTag}<span class="num">0 部</span>` +
        `<span class="muted">尚未收录</span></div>`;
    }
    let body;
    if (bars.length) {
      body = bars.map((b) => `<div class="bar-row" title="已复核确认 ${num(b.confirmed)} · 待复核 ${num(b.pending)} · 已驳回 ${num(b.rejected)}">` +
        `<span class="nm">${escapeHTML(b.level || "效力等级未标注")}</span>` +
        `<span class="bar">${seg("ok", b.confirmed)}${seg("pend", b.pending)}${seg("no", b.rejected)}</span>` +
        `<span class="num">${num(totalOf(b))} 条</span></div>`).join("");
      // 精确数字明细随构成走：折叠在本法域列内（与占比条同一批数据，可对账）。
      body += `<details class="disclose sm"><summary>${icon("chevron")}各等级的精确数字</summary>` +
        `<table class="lv"><thead><tr><th>效力等级</th><th>待复核</th><th>已复核确认</th><th>已驳回</th></tr></thead><tbody>` +
        bars.map((b) => `<tr><td>${escapeHTML(b.level || "效力等级未标注")}</td><td>${num(b.pending)}</td><td>${num(b.confirmed)}</td><td>${num(b.rejected)}</td></tr>`).join("") +
        `</tbody></table></details>`;
    } else if (laws > 0) {
      body = `<p class="muted">尚无留证原文入库，构成待原文获取后显示。</p>`;
    } else {
      const hint = `<p class="muted">${escapeHTML(JURIS_HINT[c])}</p>`;
      body = exp ? `<details class="disclose sm"><summary>${icon("chevron")}该通道现状与下一步</summary>${hint}</details>` : hint;
    }
    // 该法域的最近动态：获取记录（按渠道映射法域）与复核记录（经条目真实联查法域）
    // 归入所属法域列——每个法域一个完整档案（计数→构成→动态），缺失如实说明。
    const jr = R.filter((x) => (x.juris || "") === c).slice(0, 3);
    const jv = V.filter((x) => (x.juris || "") === c).slice(0, 3);
    const recs = `<div class="jsub">最近获取<span class="muted">本机时间</span></div>` +
      (jr.length ? jr.map((x) => `<div class="rep sm"><b>${escapeHTML(SRC_TEXT[x.source] || x.source)}</b> ${fmtWhen(x.modified_at)}<br>` +
        `<span class="muted"${utcTitle(x.modified_at)}>${x.ok > 0 ? "获取到 " + num(x.ok) + " 条" : "本轮没有新增内容"} · 失败 ${num(x.failed)} 条${x.pages ? " · 翻页 " + num(x.pages) + " 页" : ""}</span></div>`).join("") :
        '<p class="muted small">暂无获取记录。</p>') +
      `<div class="jsub">最近复核<span class="muted">谁确认或驳回了哪一条</span></div>` +
      (jv.length ? jv.map((x) => `<div class="rep sm"${utcTitle(x.created_at)}>${fmtWhen(x.created_at)}（本机时间）· ${escapeHTML(x.reviewer)} ` +
        `<span class="muted">${escapeHTML(x.action === "confirm" ? "确认" : x.action === "reject" ? "驳回" : "重新复核")} ${x.law_name ? "《" + escapeHTML(x.law_name) + "》" : "条目 " + escapeHTML(x.doc_id)}</span></div>`).join("") :
        '<p class="muted small">暂无复核记录。</p>');
    html += `<div class="jgroup">${head}${body}${recs}</div>`;
  }
  el.innerHTML = html;
  el.querySelectorAll("[data-juris]").forEach((b) => b.addEventListener("click", () => {
    browseFilter.juris = b.dataset.juris || "none"; // 未标注桶在浏览页以 none 表达
    browseFilter.level = browseFilter.confirm = browseFilter.source = browseFilter.year = "";
    showPanel("browse");
  }));
}
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
    if (edition === "reader") {
      // 只读查询版：不给写路径的去处，但必须说清这批数据现在处于什么状态、缺了怎么办。
      acts += v.pending > 0
        ? `<span>法规库中有 <b>${num(v.pending)}</b> 条内容尚未经人工复核，引用前请以官方原文为准；复核在数据维护版进行。</span>`
        : "<span>已收录的条目均已审核。</span>";
      acts += `<button data-go="pack">导入更新包补充数据</button>`;
    } else if (v.pending > 0) {
      acts += `<span>法规库中有 <b>${num(v.pending)}</b> 条内容尚未经审核，引用前请以官方原文为准。</span><button data-go="review">去智能审核</button>`;
    } else {
      acts += "<span>已收录的条目均已审核。</span>";
    }
    if (edition !== "reader") acts += `<button data-go="collect">获取最新法规</button>`;
    $("#ov-actions").innerHTML = acts;
    document.querySelectorAll("#ov-actions [data-go]").forEach((b) => b.addEventListener("click", () => showPanel(b.dataset.go)));
    // 获取/复核记录归入所属法域列（计数→构成→动态，一个法域一个完整档案）。
    // 失败如实说：ok=0 不得渲染成中性数字（诚实展示纪律）。
    const reps = (await app().RecentReports(9)) || [];
    const revs = (await app().RecentReviews(9)) || [];
    renderJuris(v.juris, v.by_level, reps, revs);
    // 复核兑现（P0-1）：把「可信」从机制变成可核对的事实——常用清单进度 + 全库比例，
    // 缺失与未复核必须点名（截断时如实说明还有多少没列）。
    let cov = null;
    try { cov = await app().ReviewCoverage(12); } catch (e) { cov = null; }
    $("#ov-coverage").textContent = cov ? cov.headline : "本机暂时读不到复核进度。";
    if (cov) {
      const total = Number(cov.priority_total || 0);
      const done = Number(cov.priority_confirmed || 0);
      const inCorp = Number(cov.priority_in_corpus || 0);
      const pct = total ? Math.round((done / total) * 100) : 0;
      const segPct = (cls, n) => n > 0 ? `<i class="${cls}" style="width:${((n / (total || 1)) * 100).toFixed(2)}%"></i>` : "";
      $("#ov-coverage-bar").innerHTML =
        `<div class="bar-row"><span class="nm">常用法规已复核</span>` +
        `<span class="bar">${segPct("ok", done)}${segPct("pend", Math.max(inCorp - done, 0))}${segPct("no", Math.max(total - inCorp, 0))}</span>` +
        `<span class="num">${num(done)} / ${num(total)}（${pct}%）</span></div>`;
      const notes = [];
      if (Number(cov.priority_missing || 0) > 0) {
        notes.push(`<span>清单中本机尚未收录 ${num(cov.priority_missing)} 部${(cov.missing_names || []).length ? "：" + escapeHTML(cov.missing_names.join("、")) + ((cov.missing_names || []).length < Number(cov.priority_missing) ? " 等" : "") : ""}——可在「获取最新法规」补齐。</span>`);
      }
      if (Number(cov.priority_unreviewed || 0) > 0) {
        notes.push(`<span>已收录但尚未复核 ${num(cov.priority_unreviewed)} 部${(cov.unreviewed_names || []).length ? "：" + escapeHTML(cov.unreviewed_names.join("、")) + ((cov.unreviewed_names || []).length < Number(cov.priority_unreviewed) ? " 等" : "") : ""}。</span>`);
      }
      if (cov.truncated) notes.push('<span class="muted">清单较长，此处只列前 12 条，其余以计数给出。</span>');
      notes.push(`<span class="muted">${escapeHTML(cov.list_note || "")}</span>`);
      $("#ov-coverage-list").innerHTML = notes.join("");
      // 数据来源与可信度（P2-7）：能做什么、数据从哪来、凭什么信——一屏给齐。
      $("#ov-cred").innerHTML =
        `<span>数据来自国务院「国家行政法规库」「国家规章库」等官方渠道，每条都保留原文链接与获取时间供逐字核对。</span>` +
        `<span>本工具只做<b>核对与留证</b>：不重排序、不改写原文、不替人裁决——同名多法一律交人工。</span>` +
        `<span>标注「尚未复核」的条目未经比对确认，引用前请以官网文本为准；本工具不构成法律意见。</span>` +
        (v.license ? `<span class="muted">数据许可：${escapeHTML(v.license)}</span>` : "") +
        (v.producer_version ? `<span class="muted">数据来源版本：${escapeHTML(v.producer_version)}</span>` : "");
    } else {
      $("#ov-coverage-bar").innerHTML = "";
      $("#ov-coverage-list").innerHTML = "";
      $("#ov-cred").innerHTML = "";
    }
    // 发行版档位与迁移留痕（P1-1 / P1-6）：本机是哪一档、数据是否被升级过，必须可见。
    const ed = [];
    if (v.edition_note) ed.push(escapeHTML(v.edition_note));
    if (v.migration_note) ed.push("迁移留痕：" + escapeHTML(v.migration_note));
    $("#ov-edition").innerHTML = ed.join(" ");
    // 本机使用情况（度量体系）：只在本机累计；未记录必须直说，不得渲染成 0。
    try {
      const u = await app().UsageReport();
      $("#ov-usage").textContent = u.headline || "";
      $("#ov-usage-funnel").innerHTML =
        `<span>${escapeHTML(u.note || "")}</span>` +
        (u.funnel || []).map((s) => `<span>${s.done ? "已" : "尚未"}${escapeHTML(s.label)}${s.count ? "（本机记录 " + num(s.count) + " 次）" : ""}</span>`).join("") +
        `<span>例外队列积压 ${num(u.exception_backlog)} 条 · 人工复核 ${num(u.human_reviews)} 次（平均每天 ${(u.human_per_day || 0).toFixed(1)} 次）</span>` +
        (u.chain_total ? `<span>溯源校验 ${num(u.chain_total)} 次，其中 ${num(u.chain_fail)} 次发现断链（${(u.fail_rate || 0).toFixed(1)}%）</span>` : "");
    } catch (e) {
      $("#ov-usage").textContent = "本机暂时读不到使用情况。";
      $("#ov-usage-funnel").innerHTML = "";
    }
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


/* ---------- 主工作区 · 条文检索 ---------- */
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
    pc.title = "全文检索按法规库数据原样给出，不重排序；就地收窄只筛本次命中，不重新查库";
  }
  $("#search-list").innerHTML = rows.map((r) => `
    <button type="button" class="item${r.confirm_status === "confirmed" ? "" : " pend"}" data-doc="${escapeHTML(r.doc_id)}" data-no="${escapeHTML(r.article_no)}" data-law="${escapeHTML(r.law_name)}">
      <span class="t">${escapeHTML(r.law_name)} <span class="pill">${escapeHTML(r.level || "等级未标注")}</span> ${statusBadge(r.confirm_status)} ${validBadge(r.effective_date, r.expire_date)}</span>
      <span class="m">${escapeHTML(r.article_no)}${r.effective_date ? " · " + r.effective_date + " 起施行" : ""}</span>
      <span class="m">${escapeHTML(r.snippet)}</span>
    </button>`).join("") || '<p class="muted">当前筛选下没有命中，再点一次已选中的条件即可取消。</p>';
  document.querySelectorAll("#search-list .item").forEach((el) =>
    el.addEventListener("click", () => showSearchPreview(el.dataset.doc, el.dataset.no, el.dataset.law)));
}

async function doSearch() {
  const q = $("#q").value.trim();
  if (!q) return;
  $("#search-info").textContent = "检索中…";
  hitFilter.level = hitFilter.confirm = ""; // 换关键词即换样本，旧筛选不得跨样本沿用
  try {
    const v = await app().SearchGuided(q, 50);
    lastHits = v.rows || [];
    // 封顶与零命中的说明与结果同一批数据（同一口径），命中被就地筛选时不重复刷屏。
    const g = $("#search-guide");
    if (g) {
      const cap = v.guidance && v.guidance.capped && !hitFilter.level && !hitFilter.confirm
        ? '<div class="guide"><p class="t">本次显示前 ' + num(lastHits.length) + ' 条命中，库里可能还有更多——用上面的条件收窄或到「按条件筛选」放宽年代。</p></div>' : "";
      g.innerHTML = lastHits.length ? cap : guideBlock(v.guidance, "全库条文中没有这段文字。");
      g.classList.toggle("hidden", lastHits.length > 0 && !v.guidance.capped);
      bindGuideJump("#search-guide");
    }
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
    // OL-1010：法名对应多部同名法规——必须并列候选交人工，不得显示为「未找到」，也不得自动采信
    if (c.error_code === "OL-1010") {
      const cs = c.candidates || [];
      $("#cite-out").textContent = "该法规名称对应多部同名法规，需人工确认所指（系统不自动采信）：\n" +
        cs.map((x) => `《${x.name}》${x.issuer ? "（" + x.issuer + "）" : ""}`).join("\n") +
        "\n请改用完整法规名称（含制定机关或发文字号）后重新核对。";
      return;
    }
    if (!c.hit) {
      $("#cite-out").textContent = "未找到对应条文" + (c.reason ? "：" + c.reason : "") + "。请核对法规名称、条款号与引用文本。";
      return;
    }
    let out = `命中：与留证原文相似度 ${(c.similarity * 100).toFixed(1)}%（条目 ${c.doc_id}，版本 ${c.version_no}，${c.confirm_status === "confirmed" ? "已复核" : "尚未复核"}）。`;
    // 按简称/别名命中时必须把认定依据摆出来（凭什么认定这部法，不能只给结论）
    if (c.resolved_from && c.resolved_from !== "full_name") {
      const how = { short_name: "简称登记", alias: "简称/别名", name_rule: "名称规则" }[c.resolved_from] || c.resolved_from;
      out += ` 本次按${how}认定为《${c.law_name || ""}》` +
        (c.alias_article_id ? `（出处条文 ${c.alias_article_id}）` : "") + "，请确认为所指法规。";
    }
    // 同名多法只有一部在所给日期生效：按日期唯一确定，但必须说明，不得静默认定
    if (c.date_disambiguated) {
      out += " 法规库中有多部同名法规，已按所给日期唯一确定；如非所指请改用完整名称。";
    }
    if (c.expired) out += ` 注意：该版本已失效，现行版本为「${c.current_version}」，请核实现行条文。`;
    if (c.chain_issues && c.chain_issues.length) out += " 来源链提示：" + c.chain_issues.map((i) => i.code).join("、") + "。";
    out += " 以下为留证原文，供逐字比对：\n" + (c.content || "");
    $("#cite-out").textContent = out;
  } catch (e) {
    $("#cite-out").textContent = "核对失败：" + (e.message || e);
  }
}
$("#btn-cite").addEventListener("click", doCite);

/* ---------- 查法规 · 核验引用（P0-2：招牌动作） ----------
   要素识别在库层（validate.VerifyCitationText），判定走 CheckCitation 同一条链路；
   这里只把六态结论翻译成人话并给出下一步——界面不得自判一套。 */
const VERDICT_TEXT = {
  verified: ["引用正确", "ok"],
  quote_mismatch: ["条文找到了，但您手头的文字与留证原文不一致", "warn"],
  not_found: ["未找到对应条文", "warn"],
  ambiguous: ["同名法规有多部，需人工确认所指", "warn"],
  expired: ["所引版本已失效", "warn"],
  broken_chain: ["来源链有断点，本次不给结论", "bad"],
  incomplete: ["这段文字里认不出法规名称", "warn"],
};
async function doVerify() {
  const text = $("#v-text").value.trim();
  if (!text) { $("#v-out").innerHTML = '<p class="muted">请先粘贴一段引用文字。</p>'; return; }
  $("#v-out").innerHTML = '<p class="muted">核验中…</p>';
  try {
    const r = await app().VerifyCitationText(text, $("#v-date").value.trim());
    const p = r.parsed || {};
    const [label, tone] = VERDICT_TEXT[r.verdict] || ["核验未完成", "warn"];
    let html = `<p class="vresult ${tone}">${escapeHTML(label)}</p>`;
    html += "<ul class=\"vlist\">" + (p.recognized || []).map((x) => `<li>认出：${escapeHTML(x)}</li>`).join("") +
      (p.missing || []).map((x) => `<li class="miss">缺：${escapeHTML(x)}</li>`).join("") + "</ul>";
    const c = r.citation || {};
    if (c.hit) {
      html += `<p class="muted small">命中留证：《${escapeHTML(c.law_name || "")}》${escapeHTML(c.article_no || "")} · ` +
        `条目 ${escapeHTML(c.doc_id || "")} · 版本 ${escapeHTML(c.version_no || "")} · ` +
        `与原文相似度 ${num(Math.round((c.similarity || 0) * 1000) / 10)}% · ` +
        `${c.confirm_status === "confirmed" ? "已复核确认" : "尚未复核，请以官方原文为准"}</p>`;
      if (c.content) html += `<div class="preview"><p>${escapeHTML(c.content)}</p></div>`;
    }
    // OL-1010：同名多法必须并列候选交人工（不自动采信、不显示成「未找到」）。
    if (c.error_code === "OL-1010" || r.verdict === "ambiguous") {
      const cs = c.candidates || [];
      html += '<p class="muted">法规库中同名条目（请人工确认所指，系统不自动采信）：' +
        cs.map((x) => `《${escapeHTML(x.name)}》${x.issuer ? "（" + escapeHTML(x.issuer) + "）" : ""}`).join(" ") +
        "</p>";
    }
    if (c.resolved_from && c.resolved_from !== "full_name") {
      const how = { short_name: "简称登记", alias: "简称/别名", name_rule: "名称规则" }[c.resolved_from] || c.resolved_from;
      html += `<p class="muted small">本次按${escapeHTML(how)}认定为《${escapeHTML(c.law_name || "")}》` +
        (c.alias_article_id ? `（出处条文 ${escapeHTML(c.alias_article_id)}）` : "") + "，请确认为所指法规。</p>";
    }
    if (c.date_disambiguated) {
      html += '<p class="muted small">法规库中有多部同名法规，已按所给日期唯一确定；如非所指请改用完整名称。</p>';
    }
    if ((c.chain_issues || []).length) {
      html += '<p class="muted small">来源链提示：' + c.chain_issues.map((i) => escapeHTML(i.code)).join("、") + "</p>";
    }
    html += "<ul class=\"vnext\">" + (r.next_steps || []).map((x) => `<li>${escapeHTML(x)}</li>`).join("") + "</ul>";
    if (c.hit && c.doc_id) {
      html += `<div class="rowbtns"><button id="v-open" class="ok">查看这条原文</button></div>`;
    }
    $("#v-out").innerHTML = html;
    const vo = $("#v-out").querySelector("#v-open");
    if (vo) vo.addEventListener("click", () => openPreview(c.doc_id));
  } catch (e) {
    $("#v-out").innerHTML = '<p class="bad">核验失败：' + escapeHTML(e.message || e) + "</p>";
  }
}
$("#btn-verify").addEventListener("click", doVerify);
$("#h-verify").addEventListener("click", () => showPanel("verify"));


/* ---------- 维护 · 智能审核（双智能体自审 → 人工只处理例外） ---------- */
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
      <button type="button" class="item${r.doc_id === exSelected ? " sel" : ""}" data-doc="${escapeHTML(r.doc_id)}">
        <span class="t">${escapeHTML(r.law_name || r.doc_id)} <span class="badge pend">需要纠正</span></span>
        <span class="m">条目编号 ${escapeHTML(r.doc_id)} · 来源：${escapeHTML(r.source || "未记录来源")}${r.level ? " · " + escapeHTML(r.level) : ""}</span>
        <span class="m"><span class="pill warn">没把握的原因：${escapeHTML(r.reason)}</span>${r.confidence ? ` <span class="pill">把握度 ${Math.round(r.confidence * 100)}%</span>` : ""}</span>
        ${r.suggest ? `<span class="m">智能体建议：${escapeHTML(r.suggest)}</span>` : ""}
      </button>`).join("") ||
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


/* ---------- 维护 · 地域标引（机器建议 → 人工落判） ---------- */
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
      <button type="button" class="item ${r.law_id === rgSelected ? "sel" : ""}" data-law="${escapeHTML(r.law_id)}">
        <span class="t">${escapeHTML(r.name || r.law_id)} ${rgStateBadge(r.state, r.region_source)}</span>
        <span class="m">${escapeHTML(r.law_id)}${r.effect_level ? " · " + escapeHTML(r.effect_level) : ""}${r.issuer ? " · " + escapeHTML(r.issuer) : ""}</span>
        <span class="m">${r.region_path ? "当前标注：" + escapeHTML(r.region_path) : "当前标注：缺失/未知"}${r.region_reviewer ? "（" + escapeHTML(r.region_reviewer) + "）" : ""}</span>
      </button>`).join("") || '<p class="muted">队列为空（或过滤无匹配）。</p>';
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

// 批量流程锁定：执行按钮只在「预览成功且有可处理条目」后解锁——「先干跑再执行」
// 如实反映在按钮状态上，不靠 expected 校验在事后兜底（AGENTS.md 人工复核纪律）。
// 预览条件一改动即失效重锁：范围核对针对的是刚才那个条件，不得张冠李戴。
function rgBulkLock(note) {
  rgBulkN = -1;
  const b = $("#rg-b-apply");
  if (b) b.disabled = true;
  if (note) $("#rg-b-info").textContent = note;
}
async function rgBulkPreview() {
  rgBulkLock("");
  try {
    const n = await app().RegionBulkPreview($("#rg-b-prov").value, $("#rg-b-verdict").value);
    rgBulkN = n;
    const btn = $("#rg-b-apply");
    if (btn) btn.disabled = !(n > 0); // 0 条可处理时执行没有意义，保持锁定并如实说明
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
    rgBulkLock("已执行；若要再处理一批，请重新查看范围。");
    loadRegionStats(); loadRegionQueue();
  } catch (e) {
    rgBulkLock("已中止：条目有变动或条件不符，请重新查看范围。");
    toast("批量失败：" + (e.message || e));
  }
}
$("#rg-b-preview").addEventListener("click", rgBulkPreview);
$("#rg-b-apply").addEventListener("click", rgBulkApply);
["rg-b-prov", "rg-b-verdict"].forEach((id) =>
  $("#" + id).addEventListener("change", () => rgBulkLock("条件已改动，请重新查看范围后再执行。")));

/* ---------- 维护 · 部门法标引（机器建议 → 人工落判；文档形态全自动只读） ---------- */
const SJ_CONF_TEXT = {
  high: "高置信（名称规则命中）",
  none: "无法判定（转智能体/人工队列）",
  marked: "已有标注",
  rejected: "人工已复核并留空",
};
// 标注来源文案：pack = 随更新包携带（本库未复核），不得显示成已复核；
// auto = 本库机器判定；manual = 人工落判；trial = 试判定。
const SJ_SRC_TEXT = {
  auto: "本库机器判定",
  manual: "人工确认",
  trial: "试判定",
  pack: "随更新包携带（本库未复核）",
};
const SJ_JUR_TEXT = { CN: "中国", EU: "欧盟", US: "美国" };
function sjStateBadge(s, src) {
  if (s === "suggest") {
    return '<span class="badge pend">' + (src === "pack" ? "随包携带 · 待复核" : "机器建议 · 待复核") + "</span>";
  }
  if (s === "reviewed") return '<span class="badge ok">已人工复核</span>';
  return '<span class="badge na">部门法未标注</span>';
}
let sjQueue = [];
let sjSelected = "";
let sjBulkN = -1; // 最近一次干跑计数；执行时带回核对
let subjectOpts = []; // app().SubjectOptions()

function fillSubjectSelects() {
  const all = subjectOpts || [];
  const sel = $("#sj-subject");
  if (!all.length) { // 词表缺失如实说明，不得渲染空下拉当已覆盖（诚实展示纪律）
    sel.innerHTML = '<option value="">（词表缺失：法规库数据不含部门法词表，请更新数据）</option>';
    return;
  }
  const groups = ["CN", "EU", "US"].map((j) => {
    const items = all.filter((o) => o.jurisdiction === j);
    if (!items.length) return "";
    return `<optgroup label="${escapeHTML(SJ_JUR_TEXT[j] || j)}">` + items.map((o) =>
      `<option value="${escapeHTML(o.code)}">${escapeHTML(o.name)}</option>`).join("") + "</optgroup>";
  }).join("");
  sel.innerHTML = '<option value="">（沿用机器建议；无建议时请选部门法）</option>' + groups;
}

async function loadSubjectOptions() {
  try { subjectOpts = (await app().SubjectOptions()) || []; } catch (e) { subjectOpts = []; }
  fillSubjectSelects();
}

async function loadSubjectStats() {
  try {
    const st = await app().SubjectReviewStats();
    $("#sj-stats").innerHTML = `<span>法规总数 <b>${num(st.total)}</b></span>` +
      `<span>机器建议 · 待复核 <b>${num(st.suggest_pending)}</b></span>` +
      `<span>已人工确认 <b>${num(st.confirmed)}</b></span>` +
      `<span>已人工驳回/清除 <b>${num(st.left_blank)}</b></span>` +
      `<span>部门法未标注 <b>${num(st.unmarked)}</b></span>`;
    setNavCount("#cnt-subject", st.suggest_pending, false);
  } catch (e) { $("#sj-stats").textContent = "统计读取失败：" + (e.message || e); }
}

async function loadSubjectQueue() {
  if (!subjectOpts.length) await loadSubjectOptions(); else fillSubjectSelects();
  try {
    sjQueue = (await app().SubjectReviewList($("#sj-filter").value.trim(), $("#sj-state").value, 300, 0)) || [];
    $("#sj-info").textContent = `队列 ${sjQueue.length} 条（最多显示 300）`;
    $("#sj-list").innerHTML = sjQueue.map((r) => `
      <button type="button" class="item ${r.law_id === sjSelected ? "sel" : ""}" data-law="${escapeHTML(r.law_id)}">
        <span class="t">${escapeHTML(r.name || r.law_id)} ${sjStateBadge(r.state, r.subject_source)}</span>
        <span class="m">${escapeHTML(r.law_id)}${r.effect_level ? " · " + escapeHTML(r.effect_level) : ""}${r.issuer ? " · " + escapeHTML(r.issuer) : ""}</span>
        <span class="m">${r.subject_name ? "部门法：" + escapeHTML(r.subject_name) : "部门法未标注"}${r.doc_form_name ? " · 形态：" + escapeHTML(r.doc_form_name) : ""}</span>
      </button>`).join("") || '<p class="muted">没有符合条件的条目。</p>';
    document.querySelectorAll("#sj-list .item").forEach((el) => el.addEventListener("click", () => showSubjectReview(el.dataset.law)));
  } catch (e) { $("#sj-list").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
async function showSubjectReview(lawID) {
  sjSelected = lawID;
  document.querySelectorAll("#sj-list .item").forEach((x) => x.classList.toggle("sel", x.dataset.law === lawID));
  try {
    const d = await app().SubjectReviewDetail(lawID);
    const row = d.row || {}, s = d.suggest || {};
    $("#sj-preview").innerHTML = `<h3>${escapeHTML(row.name || lawID)}</h3>
      <p class="muted">${escapeHTML(lawID)}${row.effect_level ? " · " + escapeHTML(row.effect_level) : ""}${row.issuer ? " · 制定机关：" + escapeHTML(row.issuer) : ""}</p>
      <p><b>当前部门法：</b>${row.subject_name ? escapeHTML(row.subject_name) + "（" + escapeHTML(row.subject_code) + "）" : "缺失/未知——可用「部门法标引」补标"} ${sjStateBadge(row.state, row.subject_source)}</p>
      ${row.subject_code ? '<p class="muted">标注来源：' + escapeHTML(SJ_SRC_TEXT[row.subject_source] || ("未知来源 " + (row.subject_source || ""))) + "</p>" : ""}
      <p><b>文档形态：</b>${row.doc_form_name ? escapeHTML(row.doc_form_name) : "形态未判定（机器按文种自动判定，无需人工确认）"}</p>
      <p><b>机器判定：</b>${escapeHTML(SJ_CONF_TEXT[s.confidence] || s.confidence || "无")}${s.subject_name ? " → " + escapeHTML(s.subject_name) : ""}${s.doc_form_name && !row.doc_form ? " · 形态建议：" + escapeHTML(s.doc_form_name) : ""}</p>
      <p class="muted">判定依据：${escapeHTML(s.note || "无")}${row.subject_rule ? " · 现值依据：" + escapeHTML(row.subject_rule) : ""}${row.subject_reviewer ? " · 复核人：" + escapeHTML(row.subject_reviewer) : ""}</p>
      ${row.state === "suggest" ? '<p class="muted">' + (row.subject_source === "pack" ? "随更新包携带的部门法未经本库人工确认前不算已复核。" : "机器建议未经人工确认前不算已复核。") + "</p>" : ""}
      <h4>复核记录</h4><ul class="log">${(d.log || []).map((x) => "<li>" + escapeHTML(x) + "</li>").join("") || '<li class="muted">暂无</li>'}</ul>`;
    if (row.subject_code) $("#sj-subject").value = row.subject_code;
  } catch (e) { $("#sj-preview").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}

async function judgeSubject(action) {
  if (!sjSelected) return toast("请先在左侧点选条目");
  const reviewer = "本机用户", reason = $("#sj-reason").value.trim();
  if (!reason) return toast("确认或驳回须填写结论依据（复核人自动记为本机用户）");
  const cur = sjQueue.find((x) => x.law_id === sjSelected) || {};
  const code = action === "confirm" ? ($("#sj-subject").value || cur.subject_code || "") : "";
  if (action === "confirm" && !code) return toast("该条目无机器建议，请在部门法下拉中选择部门法");
  try {
    const sum = await app().JudgeSubject([sjSelected], code, action, reviewer, reason);
    toast(`已处理：确认 ${num(sum.confirmed)} · 驳回 ${num(sum.rejected)} · 清除标注 ${num(sum.cleared)}`);
    loadSubjectStats(); loadSubjectQueue(); showSubjectReview(sjSelected);
  } catch (e) { toast("处理失败：" + (e.message || e)); }
}
$("#sj-load").addEventListener("click", () => { loadSubjectStats(); loadSubjectQueue(); });
$("#sj-state").addEventListener("change", loadSubjectQueue);
$("#sj-confirm").addEventListener("click", () => judgeSubject("confirm"));
$("#sj-reject").addEventListener("click", () => judgeSubject("reject"));
$("#sj-clear").addEventListener("click", () => judgeSubject("clear"));

// 批量流程锁定：与地域面板同一纪律——「先干跑再执行」如实反映在按钮状态上，
// 预览条件一改动即失效重锁（AGENTS.md 人工复核纪律）。
function sjBulkLock(note) {
  sjBulkN = -1;
  const b = $("#sj-b-apply");
  if (b) b.disabled = true;
  if (note) $("#sj-b-info").textContent = note;
}
async function sjBulkPreview() {
  sjBulkLock("");
  try {
    const n = await app().SubjectBulkPreview($("#sj-b-jur").value, $("#sj-b-verdict").value);
    sjBulkN = n;
    const btn = $("#sj-b-apply");
    if (btn) btn.disabled = !(n > 0); // 0 条可处理时执行没有意义，保持锁定并如实说明
    $("#sj-b-info").textContent = n > 0
      ? `会影响 ${num(n)} 条。核对无误后再点「执行批量处理」。`
      : "按所选条件没有可处理的条目，无需执行。";
  } catch (e) { $("#sj-b-info").textContent = "读取失败：" + (e.message || e); }
}
async function sjBulkApply() {
  if (sjBulkN < 0) return toast("请先点「先看看会影响多少条」，确认范围后再执行");
  const reviewer = "本机用户", reason = $("#sj-reason").value.trim();
  if (!reason) return toast("批量处理须填写结论依据（复核人自动记为本机用户）");
  try {
    const sum = await app().SubjectBulkApply($("#sj-b-jur").value, $("#sj-b-verdict").value, reviewer, reason, sjBulkN);
    toast(`批量完成：确认 ${num(sum.confirmed)} · 驳回 ${num(sum.rejected)} · 清除标注 ${num(sum.cleared)}`);
    sjBulkLock("已执行；若要再处理一批，请重新查看范围。");
    loadSubjectStats(); loadSubjectQueue();
  } catch (e) {
    sjBulkLock("已中止：条目有变动或条件不符，请重新查看范围。");
    toast("批量失败：" + (e.message || e));
  }
}
$("#sj-b-preview").addEventListener("click", sjBulkPreview);
$("#sj-b-apply").addEventListener("click", sjBulkApply);
["sj-b-jur", "sj-b-verdict"].forEach((id) =>
  $("#" + id).addEventListener("change", () => sjBulkLock("条件已改动，请重新查看范围后再执行。")));


/* ---------- 维护 · 获取最新法规（状态 → 常用参数 → 高级参数 → 高级设置） ---------- */
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
  // 更新包导入终态：在这一处统一渲染入口就地回显并刷新本机计数（初始化面板
  // 在显示时一并刷新）——packWatch 不再自开计时器，结果只有一处产出。
  if (packWatchID != null) {
    const t = (tasks || []).find((x) => x.id === packWatchID);
    if (t && t.state !== "running") {
      packWatchID = null;
      const label = { done: "完成", failed: "失败", cancelled: "已取消" }[t.state] || t.state;
      toast("更新包导入" + label + "：" + (t.progress || t.detail || "") + "（新条目一律进入待复核）");
      loadSideFoot();
      const sp = $("#panel-setup");
      if (sp && !sp.classList.contains("hidden")) loadSetup();
    }
  }
}
// packWatchID：正在跟踪的更新包导入任务号（null = 无）。
let packWatchID = null;
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
$("#btn-eu-text").addEventListener("click", async () => {
  try {
    const info = await app().StartEUTextTask({
      celex: $("#e-celex").value.trim(), lang: $("#e-lang").value,
      // 限速属护栏参数：由程序固定（不低于 2 秒），不在界面暴露。
      delay_sec: 2, dry_run: $("#e-dry").checked
    });
    toast(`任务 #${info.id} 已开始`); loadTasks();
  } catch (e) { toast("无法开始：" + (e.message || e)); }
});


/* ---------- 维护 · 数据自检 / 完整性档案 / 离线更新包 ---------- */
async function loadManifest() {
  try {
    const m = await app().ManifestStatus();
    $("#manifest-box").innerHTML = m.exists
      ? `<span>完整性档案：${m.verify_err ? "校验未通过 — " + escapeHTML(m.verify_err) : "校验通过 ✓"}</span>
         <span${utcTitle(m.generated_at)}>生成时间 ${escapeHTML(fmtWhen(m.generated_at) || "未知")} · 文件 ${num(m.files)} 个</span>
         <span>来源版本 ${escapeHTML(m.producer_version || "未知")} · 许可 ${escapeHTML(m.license || "未记录")}</span>`
      : '<span class="bad">完整性档案缺失</span><span>尚未生成离线数据包（CLI：openlaw-pack build），数据自检仅做留证完整性扫描。</span>';
  } catch (e) { $("#manifest-box").innerHTML = '<p class="bad">' + escapeHTML(e.message || e) + "</p>"; }
}
$("#btn-check").addEventListener("click", async () => {
  $("#check-info").textContent = "自检中（只读，可能需要十几秒）…";
  try {
    const c = await app().CorpusCheck();
    loadManifest();
    // 留证文件路径属数据目录路径——可见文字只留条目编号与原因，路径收进 title 供核对。
    const rows = (c.broken || []).map((b) => `<div class="rep bad"${b.file_path ? ' title="留证文件：' + escapeHTML(b.file_path) + '"' : ""}>${escapeHTML(b.doc_id || "")} ${escapeHTML(b.reason || "")}</div>`).join("");
    $("#check-out").innerHTML = `<div class="rep">${c.broken_count ? "留证完整性问题 " + num(c.broken_count) + " 条" : "留证文件全部完整 ✓"}${c.truncated ? "（明细仅显示前 100 条）" : ""}</div>` + rows;
    $("#check-info").textContent = "自检完成";
  } catch (e) { $("#check-info").textContent = "自检失败：" + (e.message || e); }
});
// 离线更新包：文件夹经系统选择器选取（手输路径属技术操作，不进界面）；
// 必须先校验，校验通过且来源受信才导入（后端强制），新条目一律进入待审核。
let packDir = "";
// packVerified：导入只在「校验通过且来源受信」后解锁——「必须先校验」如实反映在
// 按钮状态上，不靠后端拒绝来教用户（AGENTS.md：离线包一处写路径，先校验后导入）。
// Go 侧 PackVerify 以「校验通过（来源可信…）」开头表示可导入；
// 「校验通过，但签名公钥不在受信发布者名单——导入将被拒绝」必须保持锁定。
let packVerified = false;
function setPackVerified(ok, note) {
  packVerified = ok;
  const btn = $("#btn-pack-import");
  if (btn) btn.disabled = !ok;
  if (note) $("#p-dir-name").textContent = note;
}
// loadPackPanel：回到面板时回显已选文件夹与校验态（不让用户重选、也不装作仍未校验），
// 未选保持「尚未选择」。
function loadPackPanel() {
  const btn = $("#btn-pack-import");
  if (btn) btn.disabled = !packVerified;
  if (packDir) {
    const nm = packDir.split(/[\\/]/).filter(Boolean).pop() || packDir;
    $("#p-dir-name").textContent = "已选择更新包文件夹：" + nm +
      (packVerified ? "（校验已通过，可导入）" : "（请先校验，再导入）");
  }
  loadTasks();
}
$("#btn-pack-pick").addEventListener("click", async () => {
  try {
    const dir = await app().PickUpdatePack();
    if (!dir) return; // 用户取消选择：保持原状，不谎报
    packDir = dir;
    const nm = dir.split(/[\\/]/).filter(Boolean).pop() || dir;
    setPackVerified(false, "已选择更新包文件夹：" + nm + "（请先校验，再导入）");
  } catch (e) { toast("无法打开文件夹选择器：" + (e.message || e)); }
});
async function packVerify() {
  if (!packDir) return toast("请先点「选择更新包文件夹」");
  let out = "";
  try {
    out = await app().PackVerify(packDir);
  } catch (e) {
    // 绑定抛错同样要如实回显并锁住导入——静默失败会让人以为校验做过。
    setPackVerified(false, "校验失败：" + (e.message || e));
    return;
  }
  const ok = !out || out.indexOf("校验通过（") === 0;
  setPackVerified(ok, (out || "校验通过（来源可信），可以导入。") + (ok ? "" : "——导入保持锁定。"));
  toast((out || "校验通过（来源可信），可以导入。").slice(0, 160));
}
$("#btn-pack-verify").addEventListener("click", packVerify);
$("#btn-pack-import").addEventListener("click", async () => {
  if (!packDir) return toast("请先点「选择更新包文件夹」");
  if (!packVerified) return toast("请先校验：校验通过且来源受信才能导入");
  try { const t = await app().StartPackImportTask(packDir); toast(`更新包导入 #${t.id} 已开始（新条目进入待审核，由智能审核处理）`); loadTasks(); packWatch(t.id); } catch (e) { toast("无法开始：" + (e.message || e)); }
});
// packWatch：把一轮导入任务挂到统一任务轮询（loadTasksTick → renderTasks）跟踪到终态——
// 不再每轮导入自开计时器（旧版并发导入会叠加轮询，离开面板后仍在跑）。
// 终态回显在 renderTasks 内统一产出。
function packWatch(id) {
  packWatchID = id;
  loadTasks(); loadTasksTick();
}
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
  waitBindings(async () => {
    await applyEdition();
    applyWorkspace();
    loadRegion(); loadSideFoot(); loadRegionStats(); loadSubjectStats();
    // 空库启动（新装用户）：如实落到「初始化法规库」面板，不装作浏览态正常。
    // 读不到状态时不判定为空库（未知 ≠ 0），按当前工作区进入常规面板。
    let v = null;
    try { v = await app().Status(); } catch (e) { /* 面板内会再取并如实报错 */ }
    if (v && !v.open_err && Number(v.laws || 0) === 0) {
      ws = "main";
      localStorage.setItem("ol-ws", ws);
      applyWorkspace();
      showPanel("setup");
    } else {
      showPanel(ws === "maint" ? "overview" : "home");
    }
  });
});

