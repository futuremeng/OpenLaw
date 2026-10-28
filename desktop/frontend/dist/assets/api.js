// api.js：Wails 绑定访问与通用小工具。前端不直连数据库、不发网络请求——
// 一切经 window.go.main.App.*（与 CLI 同一 Go 代码路径）。
"use strict";

const $ = (sel) => document.querySelector(sel);
const app = () => window.go && window.go.main && window.go.main.App;

function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.style.display = "block";
  clearTimeout(toast._t);
  toast._t = setTimeout(() => (t.style.display = "none"), 5000);
}

function escapeHTML(s) {
  return (s == null ? "" : String(s)).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}

function num(n) { return (n ?? 0).toLocaleString(); }

function waitBindings(cb) {
  if (app()) cb();
  else setTimeout(() => waitBindings(cb), 100);
}