// wiring_smoke.mjs 前端接线冒烟（无浏览器）：按 index.html 声明的元素桩化 DOM，桩化
// window.go.main.App 的返回值，把 api.js + app.js 真实跑一遍——逐面板切换、检索、
// 载入队列、渲染任务与外观切换。目的是在 CI 里暴露「元素缺失 / 渲染路径抛错 /
// 侧栏计数与占比条不产出」这类只有打开界面才发现的问题。门禁：desktop.TestFrontendWiringSmoke。
// 桩不是浏览器：不做布局与样式校验，只核对接线与产出结构。
import fs from 'fs';
import vm from 'vm';
import path from 'path';
import { fileURLToPath } from 'url';

// 相对脚本自身定位，不依赖调用方工作目录（Go 门禁从 desktop/ 下调用）。
const dir = path.join(path.dirname(fileURLToPath(import.meta.url)), 'dist');
const html = fs.readFileSync(dir + '/index.html', 'utf8');
const ids = [...html.matchAll(/id="([A-Za-z0-9_-]+)"/g)].map((m) => m[1]);
const classes = [...new Set([...html.matchAll(/class="([^"]+)"/g)].flatMap((m) => m[1].split(/\s+/)).filter((c) => /^[a-z][a-z0-9_-]*$/.test(c)))];

function mk(cls) {
  const el = {
    dataset: {}, style: {}, hidden: false, textContent: '', innerHTML: '', title: '',
    value: '', checked: false, disabled: false, selectedOptions: [],
    classList: {
      _s: new Set(cls || []),
      add(c) { this._s.add(c); },
      remove(c) { this._s.delete(c); },
      toggle(c, on) { if (on === undefined) { this._s.has(c) ? this._s.delete(c) : this._s.add(c); } else { on ? this._s.add(c) : this._s.delete(c); } },
      contains(c) { return this._s.has(c); },
    },
    addEventListener() {}, setAttribute() {}, scrollIntoView() {}, focus() {}, blur() {},
    removeAttribute(k) { if (k === 'title') el.title = ''; },
    insertAdjacentHTML(p, h) { el.innerHTML = p === 'afterbegin' ? h + el.innerHTML : el.innerHTML + h; },
    querySelector() { return mk(); }, querySelectorAll() { return []; }, appendChild() {},
  };
  return el;
}
const byId = new Map();
ids.forEach((i) => {
  // 面板显隐按 s.id 判定（showPanel 切换 main section 的 hidden）——桩元素必须带 id，
  // 否则「面板可达」类断言在桩里永远失效。
  const el = mk();
  el.id = i;
  byId.set(i, el);
});
const byClass = new Map(); classes.forEach((c) => byClass.set(c, [mk(c)]));
const listeners = {};
// documentElement 带 dataset 与 style.setProperty：外观、密度与原文字号都落在这两处。
const documentElement = { dataset: {}, style: { _p: {}, setProperty(k, v) { this._p[k] = v; } } };
const document = {
  documentElement,
  addEventListener(t, f) { (listeners[t] = listeners[t] || []).push(f); },
  querySelector(s) {
    const first = s.split(/\s+/)[0];
    if (first.startsWith('#')) {
      if (!byId.has(first.slice(1))) throw new Error('缺元素 ' + s);
      return byId.get(first.slice(1));
    }
    if (first.startsWith('.')) return (byClass.get(first.slice(1)) || [])[0] || mk();
    return mk();
  },
  querySelectorAll(s) {
    if (s === 'main section') return [...byId.keys()].filter((k) => k.startsWith('panel-')).map((k) => byId.get(k));
    if (s.startsWith('.nav-group')) return [mk('nav-group')];
    if (s.startsWith('.')) return byClass.get(s.slice(1)) || [];
    return [];
  },
};
// 浏览用的小语料：列表与分面计数都按同一条件计算——冒烟里也要能看出「筛选变了、计数跟着变」。
// 桩字段一律按 Go 侧 LawRow 的真实 JSON 形状给出（confirm_status/confirm_source，
// 不是 status——桩与真实契约不一致会让接线冒烟验证错东西）。
// L3 故意缺等级/机关/地域：条目行的缺失标记要在冒烟里可见。
// 缺失的效力等级按 Go 侧真实形状给出（SQL 默认值「效力等级未标注」，不是空串）。
const laws = [
  { law_id: 'L1', name: '工伤保险条例', level: '行政法规', juris: 'CN', confirm_status: 'pending', confirm_source: '', region_source: 'none', region: '北京市', issuer: '国务院', effective_date: '2004-01-01' },
  { law_id: 'L2', name: '北京市生活垃圾管理条例', level: '地方性法规', juris: 'CN', confirm_status: 'confirmed', confirm_source: 'agent', region_source: 'auto', region: '北京市', issuer: '北京市人大常委会', effective_date: '1996-06-01' },
  { law_id: 'L3', name: '某市养犬管理办法', level: '效力等级未标注', juris: '', confirm_status: 'pending', confirm_source: '', region_source: '', region: '', issuer: '', effective_date: '' },
];
const matchLaws = (a) => laws.filter((r) => (!a.level || r.level === a.level) && (!a.juris || (r.juris || 'none') === a.juris) && (!a.kw || r.name.includes(a.kw)));
// 空库开关：初始化面板与启动落面板的诚实展示要在冒烟里可见（0 部就说 0 部）。
let emptyLib = false;
const data = {
  Status: () => emptyLib
    ? ({ laws: 0, articles: 0, documents: 0, versions: 0, pending: 0, confirmed: 0, rejected: 0, by_level: [], juris: [{ code: 'CN', laws: 0, documents: 0, pending: 0 }, { code: 'EU', laws: 0, documents: 0, pending: 0 }, { code: 'US', laws: 0, documents: 0, pending: 0 }], last_fetch: {}, data_dir: 'data', schema_version: '7', schema_min: '3', schema_max: '7', producer_version: '无 manifest', open_err: '' })
    : ({ laws: 28953, articles: 412345, documents: 28952, versions: 30000, pending: 36109, confirmed: 12, rejected: 1, by_level: [{ level: '行政法规', juris: 'CN', pending: 1200, confirmed: 8, rejected: 0 }, { level: '部门规章', juris: 'CN', pending: 30859, confirmed: 4, rejected: 1 }], juris: [{ code: 'CN', laws: 28952, documents: 28952, pending: 36109 }, { code: 'EU', laws: 1, documents: 0, pending: 0 }, { code: 'US', laws: 0, documents: 0, pending: 0 }], last_fetch: { govcn: '2026-10-28T02:28:32Z' }, data_dir: 'data', data_src: '命令行 -data', schema_version: '10', schema_min: '3', schema_max: '10', producer_version: 'v0.1.0', license: '公有领域文本；转发须署名出处', edition: 'full', edition_note: '本机为数据维护版：除查法规外，还可进行人工复核、地域标引、从官方渠道逐条获取与数据自检。', migration_note: '本机数据已从格式版本 9 升级到 10（2026-11-04T01:00:00Z）；升级前已自动备份：data/db/openlaw.db.bak-v9', open_err: '' }),
  // 智能审核：通道就绪 + 分开计数（智能体/人工/历史来源未记录）+ 例外队列样例。
  // 例外条目必须带白话原因与建议——冒烟里要能看出「为什么需要人工」。
  AgentStatus: () => ({ configured: true, model: 'test-model', guidance: '', technical: '环境变量 OPENLAW_LLM_*' }),
  AgentStats: () => ({ agent_confirmed: 42, human_confirmed: 3, legacy_confirmed: 7, escalated: 5, unreviewed: 9, last_run: '2026-10-29T02:00:00Z' }),
  ExceptionList: () => [{ doc_id: 'D1', law_name: '工伤保险条例', level: '行政法规', source: 'gov.cn 国家规章库', confidence: 0.55, reason: '公布日期与原文不一致', suggest: '建议公布日期 2004-01-01', model: 'test-model', reviewed_at: '2026-10-29T02:00:00Z' }],
  CorrectDoc: () => ({}),
  StartReviewTask: () => ({ id: 9, kind: 'agent-review', state: 'running', progress: '' }),
  Preview: () => ({ doc_id: 'D1', law_name: '工伤保险条例', level: '行政法规', confirm_status: 'pending', confirm_source: '', review_log: [], articles: [{ article_no: '第一条', text: '为了保障工伤职工…' }] }),
  RecentReports: () => [{ source: 'govcn', juris: 'CN', ok: 0, failed: 1361, pages: 0, modified_at: '2026-10-28T02:28:32Z' }],
  RecentReviews: () => [{ reviewer: '张三', action: 'confirm', doc_id: 'D1', law_name: '工伤保险条例', juris: 'CN', created_at: '2026-10-27T01:00:00Z' }],
  RegionReviewStats: () => ({ total: 28952, suggest_pending: 83, confirmed: 5, left_blank: 2, unmarked: 900 }),
  RegionReviewList: () => [{ law_id: 'L1', name: '北京市某规定', state: 'suggest', region_source: 'auto', region_path: '北京市' }],
  // 批量干跑：按真实形状返回计数（解锁执行按钮的判定按它来）。
  RegionBulkPreview: () => 7,
  RegionOptions: () => [{ code: '110000', name: '北京市', level: 1, parent: '' }],
  CurrentRegion: () => ({ code: '', path: '', source: '', at: '', note: '' }),
  // 浏览：一份小语料，列表与分面计数都按同一条件计算——冒烟里也要能看出「筛选变了、计数跟着变」。
  BrowseLawList: (a) => matchLaws(a),
  BrowseFacets: (a) => {
    const sub = matchLaws(a);
    const group = (key) => {
      const m = {};
      for (const r of sub) { const v = key(r) || ''; m[v] = (m[v] || 0) + 1; }
      return Object.entries(m).map(([v, c]) => ({ value: v, label: v || '未标注', count: c }));
    };
    return {
      total: sub.length,
      unlabeled: sub.filter((r) => r.region_source === 'none').length,
      // 分面口径与 Go 侧一致：缺失项按 COALESCE(...,'') 归入空值桶（前端不列空值桶，
      // 由脚注说明「未记录的条目不参与该维度筛选」）。
      levels: group((r) => (r.level === '效力等级未标注' ? '' : r.level)),
      juris: group((r) => r.juris || 'none').map((o) => ({ ...o, label: ({ CN: '中国', EU: '欧盟', US: '美国', none: '法域未标注' })[o.value] || o.value })),
      confirms: group((r) => r.confirm_status),
      sources: group(() => 'gov.cn 国家规章库'), years: group(() => '2000'),
      regions: group((r) => (r.region_source === 'none' ? '' : '11')),
    };
  },
  TaskList: () => [{ id: 7, kind: 'govcn-import', state: 'running', progress: '已获取 5 条', started_at: '2026-10-29T02:00:00Z' }],
  BrowserStatus: () => ({ enabled: false, guidance: '请打开辅助浏览工具后重试。', technical: 'endpoint http://127.0.0.1:9211/mcp' }),
  GovcnLists: () => [],
  ManifestStatus: () => ({ exists: false }),
  // 官方数据包目录（远程分发通道）：冒烟里桩一份两包目录（国家级基础包 + 日期增量包），
  // 验证渲染路径与法言法语标签；联网动作本身不在冒烟内发生。
  PackCatalogRefresh: () => ({ generated_at: '2026-10-30T00:00:00Z', packs: [
    { pack_id: 'base-cn-2026', kind: 'base', regions: ['CN'], since: '', until: '', doc_count: 12345, size: 10485760 },
    { pack_id: 'delta-20260101-20260331', kind: 'delta', regions: [], since: '2026-01-01', until: '2026-03-31', doc_count: 88, size: 1048576 },
  ] }),
  // 更新包校验：按 Go 侧真实形状返回「校验通过（来源可信…）」——导入解锁判定按它来。
  PackVerify: () => '校验通过（来源可信：受信发布者 test-key）',
  SearchArticles: () => [{ doc_id: 'D1', law_name: '工伤保险条例', level: '行政法规', confirm_status: 'pending', snippet: '…工伤…', article_no: '第一条' }],
  // 检索的诚实出路（P1-4）：结果 + 说明同一批数据；零命中必须带原因与下一步。
  SearchGuided: (q, k) => ({
    rows: data.SearchArticles().slice(0, k),
    guidance: { query: q, hits: 1, capped: false, zero_result: false, reasons: [], suggestions: [], alias_of: [] },
  }),
  // 发行版档位（P1-1）与复核兑现（P0-1）、本机度量：冒烟里按维护版 + 有缺口如实给出。
  AppEdition: () => ({ code: 'full', note: '本机为数据维护版：除查法规外，还可进行人工复核、地域标引、从官方渠道逐条获取与数据自检。' }),
  ReviewCoverage: () => ({
    priority_total: 99, priority_in_corpus: 12, priority_confirmed: 4, priority_unreviewed: 8, priority_missing: 87,
    missing_names: ['中华人民共和国海商法'], unreviewed_names: ['中华人民共和国劳动法'],
    truncated: true, corpus_laws: 3, corpus_docs: 3, corpus_confirmed: 1, confirmed_pct: 33.3,
    headline: '已复核确认 1 / 3 条（33.3%）；常用法规参考清单 99 部中已收录 12 部、已复核 4 部。',
    list_note: '常用法规参考清单由本项目按实务高频领域整理，用于显示复核兑现进度；它不是官方认定，也不代表清单内法规更重要。',
  }),
  UsageReport: () => ({
    available: true, since: '2026-10-05T00:00:00Z', window_days: 30,
    funnel: [{ kind: 'app_open', label: '打开本应用', count: 3, done: true }, { kind: 'search', label: '完成一次检索', count: 5, done: true }],
    search_total: 5, search_zero: 1, zero_rate: 20, verify_total: 2,
    human_reviews: 4, human_per_day: 0.1, exception_backlog: 2, fetch_runs: 1,
    chain_total: 3, chain_fail: 1, fail_rate: 33.3,
    headline: '最近 30 天：检索 5 次（其中 1 次没有命中）、引用核验 2 次、人工复核 4 次（平均每天 0.1 次）、待您纠正 2 条。',
    note: '以下数字全部来自本机留痕（不上传、不含检索词与案件内容）；「没有记录」与「记录为 0」是两回事，未记录时不会显示比率。',
  }),
  // 粘贴即核验（P0-2）：按 Go 侧 VerifyResult 形状桩「命中且一致」的结论。
  VerifyCitationText: () => ({
    parsed: { raw: '', law_name: '工伤保险条例', law_name_from: 'corpus-name', article_no: '第一条', article_written: '第1条', article_number: 1, at_date: '', quote: '', recognized: ['法规名称《工伤保险条例》（库内法规名命中）', '条款号 第一条'], missing: [] },
    verdict: 'verified',
    citation: { hit: true, law_name: '工伤保险条例', article_no: '第一条', doc_id: 'D1', version_no: '现行', similarity: 0.98, confirm_status: 'confirmed', content: '工伤保险条例第一条 为了保障工伤职工，制定本条例。', chain_issues: [], candidates: [], resolved_from: 'full_name' },
    next_steps: ['可在右侧查看留证原文并复制；引用时请附条目编号与版本，便于他人复验。'],
  }),
};
const App = new Proxy({}, { get: (t, k) => async (...args) => (k in data ? data[k](...args) : []) });
const storage = { _m: new Map(), getItem(k) { return this._m.get(k) ?? null; }, setItem(k, v) { this._m.set(k, v); } };
const window = { go: { main: { App } }, localStorage: storage };
const ctx = vm.createContext({ document, window, localStorage: storage, navigator: { clipboard: { writeText: async () => {} }, geolocation: null }, setTimeout, clearTimeout, setInterval, clearInterval, console });
for (const f of ['assets/api.js', 'assets/app.js']) vm.runInContext(fs.readFileSync(dir + '/' + f, 'utf8'), ctx, { filename: f });

(listeners['DOMContentLoaded'] || []).forEach((f) => f());
const g = (id) => byId.get(id);
(async () => {
  for (const p of ['home', 'setup', 'browse', 'overview', 'search', 'verify', 'review', 'region', 'collect', 'pack']) {
    ctx.showPanel(p);
    await new Promise((r) => setTimeout(r, 30));
  }
  g('q').value = '工伤'; g('hq').value = '工伤';
  ctx.homeSearch(); ctx.doSearch(); ctx.loadLaws(); ctx.loadTasks();
  ctx.cycleTheme(); ctx.cycleTheme();
  await new Promise((r) => setTimeout(r, 80));
  // 浏览页筛选与计数同源联动：默认条件下 3 部，改一个维度后必须跟着变。
  const totalAll = g('f-total').textContent;
  const levelChips = g('f-levels').innerHTML;
  const unlabeledNote = g('f-region-note').textContent;
  const rowHTML = g('law-list').innerHTML;
  const paneCnt = g('b-pane-cnt').innerHTML;
  const yearClosed = g('g-year').dataset.open;
  const adornAll = g('ad-level').textContent;
  // 法域维度（v7 多法域）：全库条件下分面给出「中国/法域未标注」，勾选后计数联动、回显成白话。
  const jurisChips = g('f-juris').innerHTML;
  vm.runInContext('browseFilter.level = "行政法规"; loadLaws();', ctx);
  await new Promise((r) => setTimeout(r, 60));
  const totalFiltered = g('f-total').textContent;
  const statsLine = g('b-stats').textContent;
  const adornSel = g('ad-level').textContent;
  const crumb = g('crumb-cur').textContent + ' | ' + g('crumb-ctx').textContent;
  vm.runInContext('browseFilter.level = ""; browseFilter.juris = "CN"; loadLaws();', ctx);
  await new Promise((r) => setTimeout(r, 60));
  const jurisFiltered = g('f-total').textContent;
  const jurisEcho = g('b-stats').textContent;
  vm.runInContext('browseFilter.juris = "";', ctx);
  // 进行状态就地给出：loadLaws 发起的一刻，面板头先如实报「正在取列…」，
  // 完成后才换成列出/共计——改条件后界面不得停在旧列表上让人以为筛选没生效。
  vm.runInContext('loadLaws()', ctx);
  const busyLine = g('b-pane-cnt').innerHTML;
  await new Promise((r) => setTimeout(r, 60));
  // 首页搜索封顶出口：命中 20 条（首页上限）必须给「到条文检索查看全部」，
  // 不得长成死路（与初始化面板「只给去处」同一精神）。
  const homeStub = data.SearchGuided;
  data.SearchGuided = (q, k) => ({
    rows: Array.from({ length: 20 }, (_, i) =>
      ({ doc_id: 'D' + i, law_name: '测试法规' + i, level: '行政法规', confirm_status: 'confirmed', snippet: '…测试…', article_no: '第一条' })),
    guidance: { query: q, hits: 20, capped: true, zero_result: false, reasons: [], suggestions: ['本次只显示前 20 条命中，库里可能还有更多。'], alias_of: [] },
  });
  ctx.showPanel('home');
  g('hq').value = '测试';
  await ctx.homeSearch();
  const homeMore = g('h-results').innerHTML.includes('到条文检索查看全部');
  data.SearchGuided = homeStub;
  // 粘贴即核验（P0-2）：结论、认出的要素、下一步必须同时给出，且留证原文可查。
  ctx.showPanel('verify');
  g('v-text').value = '《工伤保险条例》第一条';
  await ctx.doVerify();
  const verifyOut = g('v-out').innerHTML;
  // 零命中必须给出「为什么 + 可以怎么做」，不得只剩一句「没有找到」。
  const guidedStub = data.SearchGuided;
  data.SearchGuided = (q) => ({
    rows: [],
    guidance: { query: q, hits: 0, capped: false, zero_result: true, reasons: ['条文原文里没有这段文字。'], suggestions: ['换成更短的词后再试。'], alias_of: [] },
  });
  ctx.showPanel('search');
  g('q').value = '一个库里没有的词';
  await ctx.doSearch();
  const zeroGuide = g('search-guide').innerHTML;
  data.SearchGuided = guidedStub;
  // 恢复常规命中样本后再跑一次：后面的断言看的是正常命中下的就地收窄控件。
  await ctx.doSearch();
  const cntConfirm = g('cnt-review').textContent, cntConfirmTitle = g('cnt-review').title;
  // 待复核确实为 0 时不得留「0」徽标（无需提醒），面板内另有如实说明。
  ctx.setSideFoot({ laws: 1, pending: 0 });
  const topLib = g('t-lib-n').textContent;
  const topPendHidden = g('t-pending').classList.contains('hidden');
  const topTaskHidden = g('t-task').classList.contains('hidden');
  // 长维度默认只列前若干项，其余用「显示全部」展开（筛选栏不必长成一条竖河）。
  vm.runInContext(`facetChips('#f-years','year',` +
    `[{value:'2020',label:'2020 年代',count:9},{value:'2010',label:'2010 年代',count:8},` +
    `{value:'2000',label:'2000 年代',count:7},{value:'1990',label:'1990 年代',count:6},` +
    `{value:'1980',label:'1980 年代',count:5},{value:'1970',label:'1970 年代',count:4},` +
    `{value:'1960',label:'1960 年代',count:3},{value:'none',label:'公布日期缺失',count:2}], 6)`, ctx);
  const moreChip = g('f-years').innerHTML.includes('data-more="year"');
  vm.runInContext('setGroupOpen("year", true); facetGroup("year");', ctx);
  const yearOpen = g('g-year').dataset.open;
  // 密度与字号属显示层偏好：落到 html 属性与 CSS 变量，不得改动取列条件。
  const argsBefore = JSON.stringify(vm.runInContext('browseArgs(300,0)', ctx));
  vm.runInContext('density = "compact"; applyDensity(); stepFontSize(1);', ctx);
  const densityOn = documentElement.dataset.density === 'compact';
  const readSize = documentElement.style._p['--read-size'];
  const argsAfter = JSON.stringify(vm.runInContext('browseArgs(300,0)', ctx));
  // 维护工作区：完整切换——主导航整套收起、维护子导航整套出现，底部入口收起；
  // 「离线更新包」在维护区内可达；退出后主导航整套回来。
  ctx.enterMaintain('overview');
  await new Promise((r) => setTimeout(r, 30));
  const maintGroupOn = g('nav-group-maintain').hidden === false;
  const maintCrumb = g('crumb-root').textContent + ' | ' + g('crumb-cur').textContent;
  const mainNavHidden = g('nav-group-main').hidden === true && g('nav-home').hidden === true &&
    g('nav-browse').hidden === true && g('nav-search').hidden === true &&
    g('nav-verify').hidden === true && g('nav-maintain').hidden === true;
  ctx.enterMaintain('region');
  // 批量处理：执行按钮如实反映「先看范围再执行」——进面板即锁定，预览有可处理条目才解锁。
  const bulkLocked = g('rg-b-apply').disabled === true;
  await ctx.rgBulkPreview();
  const bulkOpen = g('rg-b-apply').disabled === false;
  ctx.enterMaintain('pack');
  const packOn = !g('panel-pack').classList.contains('hidden');
  // 离线更新包：导入按钮如实反映「必须先校验」——loadPackPanel 后仍锁定，
  // 校验通过且来源受信才解锁（不靠后端拒绝来教用户）。
  vm.runInContext('packDir = "/tmp/pk"', ctx);
  ctx.loadPackPanel();
  const importLocked = g('btn-pack-import').disabled === true;
  await ctx.packVerify();
  const importOpen = g('btn-pack-import').disabled === false;
  const verifyEcho = g('p-dir-name').textContent;
  vm.runInContext('packDir = ""; packVerified = false;', ctx);
  ctx.exitMaintain();
  const maintGroupOff = g('nav-group-maintain').hidden === true;
  // 只读查询版（P1-1）：维护入口与写路径面板入口收起；上次留下的维护态必须回落查询态，
  // 否则两套导航都被收起，用户会卡在没有任何导航的界面上。
  const edStub = data.AppEdition;
  data.AppEdition = () => ({ code: 'reader', note: '本机为只读查询版：不提供人工复核、地域标引与逐条采集。' });
  ctx.enterMaintain('overview');
  await ctx.applyEdition();
  const readerHidden = g('nav-maintain').hidden === true && g('nav-review').hidden === true &&
    g('nav-region').hidden === true && g('nav-collect').hidden === true;
  const readerBackToMain = g('nav-home').hidden === false && g('nav-verify').hidden === false;
  ctx.showPanel('overview');
  await new Promise((r) => setTimeout(r, 40));
  const readerOverviewActs = g('ov-actions').innerHTML;
  data.AppEdition = edStub;
  await ctx.applyEdition();
  ctx.exitMaintain();
  const mainNavBack = g('nav-group-main').hidden === false && g('nav-home').hidden === false &&
    g('nav-maintain').hidden === false;
  const mainCrumbRoot = g('crumb-root').textContent;
  // 初始化面板：空库如实报空并禁用「开始查法规」；非空如实报完成并解禁。
  emptyLib = true;
  await ctx.loadSetup();
  const setupEmptyLine = g('su-status').textContent;
  const goDisabled = g('su-go-home').disabled === true;
  emptyLib = false;
  await ctx.loadSetup();
  const setupDoneLine = g('su-status').textContent;
  const goEnabled = g('su-go-home').disabled === false;
  // 官方数据包目录：桩目录渲染为基础包/增量包两类标签（法言法语，不露 kind 黑话）。
  await ctx.loadPackCatalog();
  const catalogHTML = g('su-catalog').innerHTML;
  const catalogInfo = g('su-catalog-info').textContent;
  // 复位顶栏/徽标态：工作区场景里总览面板重设过待复核徽标，此处恢复「为 0 即收起」的核对基线。
  ctx.setSideFoot({ laws: 1, pending: 0 });
  const report = {
    '侧栏待复核计数': cntConfirm + ' (title: ' + cntConfirmTitle + ')',
    '待复核为 0 时清空徽标': g('cnt-review').textContent === '' && !g('cnt-review').title,
    '侧栏地域计数': g('cnt-region').textContent,
    '获取任务指示灯随任务显示': g('dot-collect').hidden === false,
    '顶栏法规库总数常驻': topLib === '1',
    '顶栏待复核提醒随计数收起': topPendHidden === true,
    '顶栏获取任务徽标随任务显示': topTaskHidden === false,
    '总览占比条含 bar-row': g('ov-juris').innerHTML.includes('bar-row'),
    '占比条段长按比例归一': /style="width:[\d.]+%"/.test(g('ov-juris').innerHTML),
    '等级精确表行数': (g('ov-juris').innerHTML.match(/<table class="lv">/g) || []).length,
    '获取记录如实（本轮没有新增内容）': g('ov-juris').innerHTML.includes('本轮没有新增内容'),
    '复核记录本机时间': /\d{4}-\d{2}-\d{2} \d{2}:\d{2}/.test(g('ov-juris').innerHTML),
    '获取/复核记录归入法域列': g('ov-juris').innerHTML.includes('最近获取') && g('ov-juris').innerHTML.includes('最近复核') && g('ov-juris').innerHTML.includes('《工伤保险条例》'),
    '空法域列如实报暂无动态': (g('ov-juris').innerHTML.match(/暂无获取记录/g) || []).length >= 2,
    '智能审核统计分开计数': g('rv-stats').innerHTML.includes('智能体已确认') && g('rv-stats').innerHTML.includes('人工已确认') && g('rv-stats').innerHTML.includes('历史确认'),
    '例外队列给出没把握原因': g('ex-list').innerHTML.includes('没把握的原因') && g('ex-list').innerHTML.includes('公布日期与原文不一致'),
    '智能审核通道回显': g('rv-channel').textContent.includes('已就绪'),
    '外观切换（顶栏图标按钮）': g('theme-toggle').title,
    'html data-theme': JSON.stringify(documentElement.dataset.theme || ''),
    '任务卡 class 含 running': g('task-list').innerHTML.includes('task running'),
    '端点等技术细节仍可得（供折叠展示）': g('browser-tech').innerHTML.includes('127.0.0.1'),
    '清单缺失给出下一步': g('g-list-hint').textContent.includes('没有目录清单'),
    '分面法域给出中国/未标注': jurisChips.includes('中国') && jurisChips.includes('法域未标注'),
    '法域筛选联动计数': jurisFiltered === '2',
    '法域筛选回显为白话': jurisEcho.includes('法域 中国'),
    '总览分法域如实报缺失': g('ov-juris').innerHTML.includes('尚未收录') && g('ov-juris').innerHTML.includes('美国'),
    '总览法域行可下钻（带待复核）': g('ov-juris').innerHTML.includes('data-juris="CN"') && g('ov-juris').innerHTML.includes('待复核 36,109 条'),
    '分法域与构成一个法域一个块': (g('ov-juris').innerHTML.match(/class="jgroup"/g) || []).length === 3,
    '列宽随数据状态（仅中国有条→solo-1）': String(g('ov-juris').className).includes('solo-1'),
    '空法域如实说明（0条文不得说均已复核）': g('ov-juris').innerHTML.includes('尚未收录') && g('ov-juris').innerHTML.includes('尚无留证条文'),
    '精确数字折叠归入法域列': (g('ov-juris').innerHTML.match(/各等级的精确数字/g) || []).length === 1 && g('ov-juris').innerHTML.includes('行政法规'),
    '浏览分面给出计数': levelChips.includes('fcount'),
    '筛选前后总数联动': totalAll === '3' && totalFiltered === '1',
    '筛选回显为白话': statsLine.includes('当前筛选：类别 行政法规'),
    '面包屑给出当前位置与筛选摘要': crumb.includes('按条件筛选') && crumb.includes('类别 行政法规'),
    '分面组头报已选项': adornAll === '2 项' && adornSel === '已选 行政法规',
    '长维度默认收起': yearClosed === 'false' && yearOpen === 'true',
    '长维度给出显示全部': moreChip,
    '面板头如实报列出/共计': paneCnt.includes('显示') && paneCnt.includes('共'),
    '取列进行时就地提示': busyLine.includes('正在取列'),
    '首页搜索封顶给出路': homeMore,
    '粘贴即核验给出结论与下一步': verifyOut.includes('引用正确') && verifyOut.includes('认出：') && verifyOut.includes('留证'),
    '零命中给出为什么与出路': zeroGuide.includes('为什么') && zeroGuide.includes('可以怎么做'),
    '复核兑现如实报进度': g('ov-coverage').textContent.includes('已复核确认 1 / 3') && g('ov-coverage-bar').innerHTML.includes('bar-row'),
    '复核缺口点名并声明清单性质': g('ov-coverage-list').innerHTML.includes('中华人民共和国海商法') && g('ov-coverage-list').innerHTML.includes('不是官方认定'),
    '数据来源与可信度常驻': g('ov-cred').innerHTML.includes('官方渠道') && g('ov-cred').innerHTML.includes('不构成法律意见'),
    '发行版与迁移留痕可见': g('ov-edition').innerHTML.includes('数据维护版') && g('ov-edition').innerHTML.includes('迁移留痕'),
    '本机使用情况如实（含不上传声明）': g('ov-usage').textContent.includes('最近 30 天') && g('ov-usage-funnel').innerHTML.includes('不上传'),
    '实验通道如实标注': g('ov-juris').innerHTML.includes('实验通道'),
    '只读查询版收起写路径入口': readerHidden && readerBackToMain,
    '只读查询版下一步不给写路径去处': !readerOverviewActs.includes('data-go="collect"') && !readerOverviewActs.includes('data-go="review"') && readerOverviewActs.includes('data-go="pack"'),
    '缺失合并成一个标记': rowHTML.includes('未标注 3 项') && !rowHTML.includes('效力等级未标注 · 地域未标注'),
    '缺失不被当作已知值回显': !rowHTML.includes('效力等级未标注'),
    '待复核条目行有强调类': rowHTML.includes('item pend'),
    '条目徽章如实报智能体确认': rowHTML.includes('智能体确认'),
    '未标注地区如实说明': unlabeledNote.includes('未标注'),
    '检索命中可就地收窄': g('sf-level').innerHTML.includes('fcount'),
    '检索面板头报显示/命中': g('s-pane-cnt').innerHTML.includes('命中'),
    '首页类别卡带筛选入口': g('h-cards').innerHTML.includes('data-level'),
    '紧凑模式落到 html 属性': densityOn,
    '原文字号可调': readSize === '17px',
    '显示偏好不改取列条件': argsBefore === argsAfter,
    '维护入口进入独立工作区': maintGroupOn && maintCrumb === '维护法规库 | 库总览',
    '两套导航完整互斥切换': mainNavHidden && mainNavBack,
    '离线更新包在维护区可达': packOn,
    '导入须先校验（校验前锁定）': importLocked,
    '校验通过且来源受信才解锁导入': importOpen && verifyEcho.includes('校验通过（来源可信'),
    '批量执行须先预览（进面板锁定）': bulkLocked,
    '预览有可处理条目才解锁执行': bulkOpen,
    '退出维护回主导航': maintGroupOff && mainCrumbRoot === '法律法规库',
    '初始化面板如实报空库': setupEmptyLine.includes('还是空的（0 部法规）'),
    '空库时不得装作可查询': goDisabled,
    '库非空初始化如实报完成': setupDoneLine.includes('初始化已完成') && goEnabled,
    '数据包目录渲染基础包/增量包': catalogHTML.includes('基础包 · 全国') && catalogHTML.includes('增量包 · 2026-01-01 至 2026-03-31 公布'),
    '数据包目录如实报更新时间': catalogInfo.includes('目录已更新') && catalogInfo.includes('2 个包'),
  };
  const failed = Object.entries(report).filter(([, v]) => v === false).map(([k]) => k);
  console.log(JSON.stringify(report, null, 1));
  if (failed.length) { console.error('SMOKE_FAIL 未通过项：' + failed.join('、')); process.exit(1); }
  console.log('SMOKE_OK');
  // 面板轮询（任务刷新的 setInterval）会让进程常驻，核对完成即退出。
  process.exit(0);
})().catch((e) => { console.error('SMOKE_FAIL', e); process.exit(1); });
