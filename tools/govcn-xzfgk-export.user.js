// ==UserScript==
// @name         OpenCourt/OpenLaw 行政法规库清单导出
// @namespace    openlaw
// @version      0.1.0
// @description  在国家行政法规库（www.gov.cn/zhengce/xzfgk/ 及其检索结果页）浏览时，把【当前页面已显示】的法规条目导出为 list.json，供 openlaw-govcn 按清单抓取详情页。合规边界：本脚本只整理用户已浏览页面的 DOM，绝不代为调用检索接口（sousuo.www.gov.cn robots.txt 为 Disallow: /，自动化访问禁止）；翻页须人工点击，脚本跨页累积。
// @match        https://www.gov.cn/zhengce/xzfgk/*
// @match        https://sousuo.www.gov.cn/sousuo/search.shtml*
// @grant        none
// @run-at       document-idle
// ==/UserScript==
(function () {
  'use strict';

  // 跨页累积（同一标签页内，人工翻页后点击导出会带上此前所有已浏览页）
  var ACCUM = {}; // url -> {title, pcode, pubtime, url}

  function absURL(a) {
    try { return new URL(a.href, location.href).href; } catch (e) { return null; }
  }

  function contextText(el) {
    // 条目所在行/卡片的纯文本（用于提取文号与日期），限长防止抓进整页噪声
    var p = el;
    for (var i = 0; i < 5 && p.parentElement; i++) {
      p = p.parentElement;
      if ((p.innerText || '').length > 40) break;
    }
    return (p.innerText || '').slice(0, 500);
  }

  function harvest() {
    var found = 0;
    document.querySelectorAll('a[href*="/zhengce/"][href*="content_"]').forEach(function (a) {
      var u = absURL(a);
      if (!u || !/^https:\/\/www\.gov\.cn\/zhengce\/.+\/content_\d+\.html?$/.test(u)) return;
      var title = (a.textContent || '').replace(/\s+/g, ' ').trim();
      if (!title) return;
      var ctx = contextText(a);
      var pm = ctx.match(/(国令|国发|国函|国办发|国办函)\s*第?\s*([0-9一二三四五六七八九十百千]+)\s*号/);
      var dm = ctx.match(/(\d{4})\.(\d{1,2})\.(\d{1,2})/);
      if (!ACCUM[u]) { found++; }
      ACCUM[u] = {
        title: title,
        pcode: pm ? pm[1] + '第' + pm[2] + '号' : '',
        pubtime: dm ? dm[0] : '',
        url: u
      };
    });
    return found;
  }

  function exportJSON() {
    var n = harvest();
    var list = Object.keys(ACCUM).map(function (k) { return ACCUM[k]; });
    if (!list.length) { alert('当前页面未发现法规条目（确认已在法规库检索结果页，且结果已显示）'); return; }
    var blob = new Blob([JSON.stringify(list, null, 2)], { type: 'application/json' });
    var a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = 'govcn-list.json';
    a.click();
    URL.revokeObjectURL(a.href);
    console.log('[openlaw] 导出 ' + list.length + ' 条（本页新增 ' + n + '）→ 保存后运行: openlaw-govcn -data data -list <此文件>');
  }

  var btn = document.createElement('button');
  btn.textContent = '导出法规清单 JSON';
  btn.style.cssText = 'position:fixed;right:16px;bottom:16px;z-index:99999;padding:10px 14px;' +
    'background:#b02a30;color:#fff;border:0;border-radius:6px;cursor:pointer;font-size:14px;';
  btn.title = '仅整理本页已显示内容（人工浏览），跨页累积；翻页请人工点击';
  btn.addEventListener('click', exportJSON);
  document.body.appendChild(btn);
})();
