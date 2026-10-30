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
    addEventListener() {}, setAttribute() {}, scrollIntoView() {},
    removeAttribute(k) { if (k === 'title') el.title = ''; },
    insertAdjacentHTML(p, h) { el.innerHTML = p === 'afterbegin' ? h + el.innerHTML : el.innerHTML + h; },
    querySelector() { return mk(); }, querySelectorAll() { return []; }, appendChild() {},
  };
  return el;
}
const byId = new Map(); ids.forEach((i) => byId.set(i, mk()));
const byClass = new Map(); classes.forEach((c) => byClass.set(c, [mk(c)]));
const listeners = {};
const documentElement = { dataset: {} };
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
// 浏览用的小语料：列表与分面计数都由同一条件函数产出（与 Go 侧 browseWhere 同构）。
const laws = [
  { law_id: 'L1', name: '工伤保险条例', level: '行政法规', status: 'pending', region_source: 'none', region: '北京市', issuer: '国务院', effective_date: '2004-01-01' },
  { law_id: 'L2', name: '北京市生活垃圾管理条例', level: '地方性法规', status: 'confirmed', region_source: 'auto', region: '北京市', issuer: '北京市人大常委会', effective_date: '1996-06-01' },
];
const matchLaws = (a) => laws.filter((r) => (!a.level || r.level === a.level) && (!a.kw || r.name.includes(a.kw)));
const data = {
  Status: () => ({ laws: 28952, articles: 412345, documents: 28952, versions: 30000, pending: 36109, confirmed: 12, rejected: 1, by_level: [{ level: '行政法规', pending: 1200, confirmed: 8, rejected: 0 }, { level: '部门规章', pending: 30859, confirmed: 4, rejected: 1 }], last_fetch: { govcn: '2026-10-28T02:28:32Z' }, data_dir: 'data', data_src: '命令行 -data', schema_version: '4', schema_min: '3', schema_max: '4', producer_version: 'v0.1.0', open_err: '' }),
  QueueStats: () => ({ pending: 36109, confirmed: 12, rejected: 1, risk: { inferred_date: 5 } }),
  PendingList: () => [{ doc_id: 'D1', law_name: '工伤保险条例', source_type: 'flk 批量下载', flags: ['inferred_date'] }],
  ConfirmSources: () => [{ source: 'flk 批量下载', pending: 30859 }],
  RecentReports: () => [{ source: 'govcn', ok: 0, failed: 1361, pages: 0, modified_at: '2026-10-28T02:28:32Z' }],
  RecentReviews: () => [{ reviewer: '张三', action: 'confirm', doc_id: 'D1', created_at: '2026-10-27T01:00:00Z' }],
  RegionReviewStats: () => ({ total: 28952, suggest_pending: 83, confirmed: 5, left_blank: 2, unmarked: 900 }),
  RegionReviewList: () => [{ law_id: 'L1', name: '北京市某规定', state: 'suggest', region_source: 'auto', region_path: '北京市' }],
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
      levels: group((r) => r.level), confirms: group((r) => r.status),
      sources: group(() => 'gov.cn 国家规章库'), years: group(() => '2000'),
      regions: group((r) => (r.region_source === 'none' ? '' : '11')),
    };
  },
  TaskList: () => [{ id: 7, kind: 'govcn-import', state: 'running', progress: '已获取 5 条', started_at: '2026-10-29T02:00:00Z' }],
  BrowserStatus: () => ({ enabled: false, guidance: '请打开辅助浏览工具后重试。', technical: 'endpoint http://127.0.0.1:9211/mcp' }),
  GovcnLists: () => [],
  ManifestStatus: () => ({ exists: false }),
  SearchArticles: () => [{ doc_id: 'D1', law_name: '工伤保险条例', level: '行政法规', confirm_status: 'pending', snippet: '…工伤…', article_no: '第一条' }],
};
const App = new Proxy({}, { get: (t, k) => async (...args) => (k in data ? data[k](...args) : []) });
const storage = { _m: new Map(), getItem(k) { return this._m.get(k) ?? null; }, setItem(k, v) { this._m.set(k, v); } };
const window = { go: { main: { App } }, localStorage: storage };
const ctx = vm.createContext({ document, window, localStorage: storage, navigator: { clipboard: { writeText: async () => {} }, geolocation: null }, setTimeout, clearTimeout, setInterval, clearInterval, console });
for (const f of ['assets/api.js', 'assets/app.js']) vm.runInContext(fs.readFileSync(dir + '/' + f, 'utf8'), ctx, { filename: f });

(listeners['DOMContentLoaded'] || []).forEach((f) => f());
const g = (id) => byId.get(id);
(async () => {
  for (const p of ['home', 'browse', 'overview', 'search', 'confirm', 'region', 'collect']) {
    ctx.showPanel(p);
    await new Promise((r) => setTimeout(r, 30));
  }
  g('q').value = '工伤'; g('hq').value = '工伤';
  ctx.homeSearch(); ctx.doSearch(); ctx.loadLaws(); ctx.loadTasks();
  ctx.cycleTheme(); ctx.cycleTheme();
  await new Promise((r) => setTimeout(r, 80));
  // 浏览页筛选与计数同源联动：默认条件下 2 部，改一个维度后必须跟着变。
  const totalAll = g('f-total').textContent;
  const levelChips = g('f-levels').innerHTML;
  const unlabeledNote = g('f-region-note').textContent;
  vm.runInContext('browseFilter.level = "行政法规"; loadLaws();', ctx);
  await new Promise((r) => setTimeout(r, 60));
  const totalFiltered = g('f-total').textContent;
  const statsLine = g('b-stats').textContent;
  const cntConfirm = g('cnt-confirm').textContent, cntConfirmTitle = g('cnt-confirm').title;
  // 待复核确实为 0 时不得留「0」徽标（无需提醒），面板内另有如实说明。
  ctx.setSideFoot({ laws: 1, pending: 0 });
  const report = {
    '侧栏待复核计数': cntConfirm + ' (title: ' + cntConfirmTitle + ')',
    '待复核为 0 时清空徽标': g('cnt-confirm').textContent === '' && !g('cnt-confirm').title,
    '侧栏地域计数': g('cnt-region').textContent,
    '获取任务指示灯 hidden': g('dot-collect').hidden,
    '总览占比条含 bar-row': g('ov-bars').innerHTML.includes('bar-row'),
    '占比条段长按比例归一': /style="width:[\d.]+%"/.test(g('ov-bars').innerHTML),
    '等级精确表行数': (g('level-table').innerHTML.match(/<tr>/g) || []).length,
    '获取记录如实（本轮没有新增内容）': g('reports').innerHTML.includes('本轮没有新增内容'),
    '复核记录本机时间': /\d{4}-\d{2}-\d{2} \d{2}:\d{2}/.test(g('reviews').innerHTML),
    '批量来源为真实来源名': g('b-source').innerHTML.includes('flk 批量下载'),
    '外观切换文案': g('btn-theme').textContent,
    'html data-theme': JSON.stringify(documentElement.dataset),
    '任务卡 class 含 running': g('task-list').innerHTML.includes('task running'),
    '端点等技术细节仍可得（供折叠展示）': g('browser-tech').innerHTML.includes('127.0.0.1'),
    '清单缺失给出下一步': g('g-list-hint').textContent.includes('没有目录清单'),
    '浏览分面给出计数': levelChips.includes('fcount'),
    '筛选前后总数联动': totalAll === '2' && totalFiltered === '1',
    '筛选回显为白话': statsLine.includes('当前筛选：类别 行政法规'),
    '未标注地区如实说明': unlabeledNote.includes('未标注'),
    '检索命中可就地收窄': g('sf-level').innerHTML.includes('fcount'),
    '首页类别卡带筛选入口': g('h-cards').innerHTML.includes('data-level'),
  };
  console.log(JSON.stringify(report, null, 1));
  console.log('SMOKE_OK');
  // 面板轮询（任务刷新的 setInterval）会让进程常驻，核对完成即退出。
  process.exit(0);
})().catch((e) => { console.error('SMOKE_FAIL', e); process.exit(1); });
