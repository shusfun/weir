// 首屏前应用主题，避免闪烁。独立文件以满足 CSP script-src 'self'（内联脚本会被拦截）。
(function () {
  var theme = "system";
  try {
    var stored = localStorage.getItem("migate-theme");
    if (stored === "light" || stored === "dark" || stored === "system") theme = stored;
  } catch (e) {
    // 浏览器禁止存储时按跟随系统处理
  }
  var dark = theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  var root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.dataset.theme = theme;
  root.style.colorScheme = dark ? "dark" : "light";
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta && dark) meta.setAttribute("content", "#1c1c1e");
})();
