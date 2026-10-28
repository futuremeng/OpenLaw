// api.js：Wails 绑定访问与启动等待。前端不直连数据库、不发网络请求——
// 一切经 window.go.main.App.*（与 CLI 同一 Go 代码路径）。通用小工具在 app.js。
"use strict";

const app = () => window.go && window.go.main && window.go.main.App;

function waitBindings(cb) {
  if (app()) cb();
  else setTimeout(() => waitBindings(cb), 100);
}
