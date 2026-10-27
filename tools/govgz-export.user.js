// ==UserScript==
// @name         OpenCourt/OpenLaw 国家规章库清单导出
// @namespace    openlaw
// @version      0.1.0
// @description  在国家规章库（www.gov.cn/zhengce/xxgk/gjgzk/）浏览时，把【当前页面已显示】的规章条目导出为 govgz-list.json，供 openlaw-govcn -source gzk 按清单抓取详情页。合规边界：本脚本只整理用户已浏览页面的 DOM，绝不代为调用检索接口（sousuo 检索域 robots 为 Disallow: /，规章库 Athena 接口另带应用层鉴权——均不得自动化）；翻页/切页签须人工点击，脚本跨页累积。
// @match        https://www.gov.cn/zhengce/xxgk/gjgzk/*
// @grant        none
// @run-at       document-idle
// ==/UserScript==
(function () {
  'use strict';

  // 跨页累积（同一标签页内，人工翻页/切页签后点击导出会带上此前所有已浏览页）
  var ACCUM = {}; // url -> {title, url, cat, puborg, pcode}

  function currentCat() {
    // 当前激活的分类页签（站点自述分类，人工浏览时可见）：部门规章 / 地方政府规章
    var sel = document.querySelector('.top-b div.active_bac, .top-b1 div.active_bac');
    if (!sel) return '';
    var t = (sel.innerText || '').replace(/\s+/g, '');
    if (t.indexOf('地方') >= 0) return '地方政府规章';
    if (t.indexOf('部门') >= 0 || t.indexOf('规章') >= 0) return '部门规章';
    return '';
  }

  function harvest() {
    var cat = currentCat();
    var found = 0;
    document.querySelectorAll('div.depart_m').forEach(function (item) {
      var a = item.querySelector('a[docurl]');
      var u = a ? a.getAttribute('docurl') : '';
      try { u = new URL(u, location.href).href; } catch (e) { return; }
      if (!/^https:\/\/www\.gov\.cn\/.+\/content_\d+\.html?$/.test(u)) return;
      var h5 = item.querySelector('h5');
      var title = h5 ? (h5.textContent || '').replace(/\s+/g, ' ').trim() : '';
      if (!title) return;
      var dp = item.querySelector('p.de_part span');
      var puborg = dp ? (dp.textContent || '').replace(/\s+/g, ' ').trim() : '';
      var ctx = (item.innerText || '').slice(0, 600);
      var pm = ctx.match(/[\u4e00-\u9fa5（）]{2,24}令\s*第?\s*[0-9一二三四五六七八九十百]+\s*号/);
      if (!ACCUM[u]) { found++; }
      ACCUM[u] = {
        title: title,
        url: u,
        cat: cat || (ACCUM[u] && ACCUM[u].cat) || '',
        puborg: puborg || (ACCUM[u] && ACCUM[u].puborg) || '',
        pcode: pm ? pm[0].replace(/\s+/g, '') : ''
      };
    });
    return found;
  }

  function exportJSON() {
    var n = harvest();
    var list = Object.keys(ACCUM).map(function (k) { return ACCUM[k]; });
    if (!list.length) { alert('当前页面未发现规章条目（确认已在国家规章库检索结果页，且结果已显示）'); return; }
    var blob = new Blob([JSON.stringify(list, null, 2)], { type: 'application/json' });
    var a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = 'govgz-list.json';
    a.click();
    URL.revokeObjectURL(a.href);
    console.log('[openlaw] 导出 ' + list.length + ' 条（本页新增 ' + n + '）→ 保存后运行: openlaw-govcn -source gzk -data data -list <此文件>');
  }

  var btn = document.createElement('button');
  btn.textContent = '导出规章清单 JSON';
  btn.style.cssText = 'position:fixed;right:16px;bottom:16px;z-index:99999;padding:10px 14px;' +
    'background:#b02a30;color:#fff;border:0;border-radius:6px;cursor:pointer;font-size:14px;';
  btn.title = '仅整理本页已显示内容（人工浏览），跨页累积；翻页/切页签请人工点击';
  btn.addEventListener('click', exportJSON);
  document.body.appendChild(btn);
})();
