// 页面侧公共部分：颜色归一、设计变量读取、元素描述、弹层识别、页面错误、布局跳动与掉帧记录。
// 由 lib.mjs 拼接后注入页面，全部定义挂在 window.__weirScan 上。
const S = (window.__weirScan = { errors: [] });

window.addEventListener("error", (e) => S.errors.push({ kind: "error", message: String(e.message || e.error), at: location.pathname + location.search }));
window.addEventListener("unhandledrejection", (e) => S.errors.push({ kind: "rejection", message: String(e.reason?.message ?? e.reason), at: location.pathname + location.search }));

// ---------- 颜色 ----------
const canvas = document.createElement("canvas");
canvas.width = canvas.height = 1;
const ctx2d = canvas.getContext("2d", { willReadFrequently: true });
const colorCache = new Map();
// 任意 CSS 颜色 → [r,g,b,a(0-1)]；用画布取像素，oklch / color-mix 等也能比较
S.rgba = function (str) {
  if (!str) return null;
  if (colorCache.has(str)) return colorCache.get(str);
  let out = null;
  if (str === "transparent") out = [0, 0, 0, 0];
  else if (CSS.supports("color", str)) {
    ctx2d.clearRect(0, 0, 1, 1);
    ctx2d.fillStyle = "#000";
    ctx2d.fillStyle = str;
    ctx2d.fillRect(0, 0, 1, 1);
    const d = ctx2d.getImageData(0, 0, 1, 1).data;
    out = [d[0], d[1], d[2], d[3] / 255];
  }
  colorCache.set(str, out);
  return out;
};
S.colorText = (c) => (c ? (c[3] >= 0.999 ? `rgb(${c[0]} ${c[1]} ${c[2]})` : `rgb(${c[0]} ${c[1]} ${c[2]} / ${c[3].toFixed(2)})`) : "?");
const sameColor = (a, b) => {
  if (a[3] < 0.004 && b[3] < 0.004) return true;
  const tol = Math.min(a[3], b[3]) >= 0.5 ? 2 : 8;
  return Math.abs(a[0] - b[0]) <= tol && Math.abs(a[1] - b[1]) <= tol && Math.abs(a[2] - b[2]) <= tol && Math.abs(a[3] - b[3]) <= 0.015;
};

// ---------- 设计变量 ----------
// 名称从样式表里收集（:root、.dark 以及组件里定义的 --weir-*），值按当前主题在 <html> 上计算
function tokenNames() {
  const names = new Set();
  const walk = (rules) => {
    for (const r of rules) {
      if (r.style) for (let i = 0; i < r.style.length; i++) if (r.style[i].startsWith("--weir-")) names.add(r.style[i]);
      if (r.cssRules) walk(r.cssRules);
    }
  };
  for (const sh of document.styleSheets) {
    try {
      walk(sh.cssRules);
    } catch {}
  }
  const cs = getComputedStyle(document.documentElement);
  for (let i = 0; i < cs.length; i++) if (cs[i].startsWith("--weir-")) names.add(cs[i]);
  return [...names];
}
S.tokens = function () {
  const root = document.documentElement;
  const cs = getComputedStyle(root);
  const probe = document.createElement("div");
  probe.style.cssText = "position:absolute;visibility:hidden;pointer-events:none;width:0;height:0";
  root.appendChild(probe);
  const t = { colors: [], radii: new Set(["0px"]), durations: new Set([0]), fonts: new Set(), sizes: [], names: [], dark: root.classList.contains("dark") };
  for (const name of tokenNames()) {
    const raw = cs.getPropertyValue(name).trim();
    if (!raw) continue;
    t.names.push(name);
    if (CSS.supports("color", raw)) {
      probe.style.color = `var(${name})`;
      const c = S.rgba(getComputedStyle(probe).color);
      if (c) t.colors.push({ name, c });
      continue;
    }
    if (/^-?[\d.]+m?s$/.test(raw)) {
      t.durations.add(raw.endsWith("ms") ? parseFloat(raw) : parseFloat(raw) * 1000);
      continue;
    }
    if (/^[\d.]+(px|rem|em)$/.test(raw)) {
      probe.style.borderTopLeftRadius = `var(${name})`;
      const v = getComputedStyle(probe).borderTopLeftRadius;
      if (/radius/.test(name)) t.radii.add(v);
      if (/(font-size|text-|leading|line-height)/.test(name)) {
        probe.style.fontSize = `var(${name})`;
        t.sizes.push({ name, px: parseFloat(getComputedStyle(probe).fontSize) });
      }
      continue;
    }
    if (/,/.test(raw) && /(sans|serif|mono|system-ui)/.test(raw)) t.fonts.add(S.normFont(raw));
  }
  probe.remove();
  return t;
};
S.normFont = (f) => f.toLowerCase().replace(/["']/g, "").replace(/\s*,\s*/g, ",").trim();
S.colorToken = (tok, c) => {
  if (!c || c[3] < 0.004) return "transparent";
  const hit = tok.colors.find((x) => sameColor(x.c, c));
  return hit ? hit.name : null;
};

// ---------- 元素 ----------
S.visible = (el) => {
  const r = el.getBoundingClientRect();
  if (r.width < 1 || r.height < 1) return false;
  // 横向完全移出视口（窄屏收起的侧栏抽屉等）视为不可见；纵向在折叠线以下的照常检查
  if (r.right <= 0 || r.left >= innerWidth) return false;
  const cs = getComputedStyle(el);
  return cs.visibility !== "hidden" && cs.display !== "none" && !el.closest("[inert],[aria-hidden=true]");
};
// 组件名：自身或最近 4 层祖先里第一个 data-slot 或带连字符的类名（weir-key、rail-item 等），都没有再取最近的 data-slot
S.component = (el) => {
  for (let n = el, d = 0; n && n !== document.body && d < 5; n = n.parentElement, d++) {
    const slot = n.getAttribute("data-slot");
    if (slot) return slot;
    const c = [...n.classList].find((x) => x.startsWith("weir-") || /^[a-z]+(-[a-z]+)+$/.test(x));
    if (c) return c;
  }
  return el.closest("[data-slot]")?.getAttribute("data-slot") || el.tagName.toLowerCase();
};
S.selector = (el) => {
  const parts = [];
  for (let n = el, d = 0; n && n !== document.body && d < 4; n = n.parentElement, d++) {
    let p = n.tagName.toLowerCase();
    if (n.id && !/^(:|base-ui|radix)/.test(n.id)) {
      parts.unshift(p + "#" + CSS.escape(n.id));
      break;
    }
    const slot = n.getAttribute("data-slot");
    if (slot) p += `[data-slot=${slot}]`;
    else {
      const cls = [...n.classList].filter((c) => !/[:[\]/]/.test(c)).slice(0, 2);
      if (cls.length) p += "." + cls.join(".");
    }
    parts.unshift(p);
  }
  return parts.join(" > ");
};
S.describe = (el) => ({
  tag: el.tagName.toLowerCase(),
  role: el.getAttribute("role") || "",
  text: (el.getAttribute("aria-label") || el.innerText || el.value || el.getAttribute("placeholder") || el.getAttribute("title") || "").replace(/\s+/g, " ").trim().slice(0, 40),
  selector: S.selector(el),
  component: S.component(el),
});
S.interactiveSel = 'button, a[href], input:not([type=hidden]), select, textarea, summary, [role=button], [role=link], [role=tab], [role=menuitem], [role=menuitemcheckbox], [role=menuitemradio], [role=option], [role=switch], [role=checkbox], [role=radio], [role=combobox], [role=slider], [tabindex]:not([tabindex="-1"])';

// ---------- 弹层 ----------
S.popupSel = '[role=dialog], [role=alertdialog], [role=menu], [role=listbox], [data-slot=dialog-content], [data-slot=drawer-content], [data-slot=sheet-content], [data-slot=menu-content], [data-slot=popover-content], [data-slot=select-content]';
S.popups = () => [...document.querySelectorAll(S.popupSel)].filter((el) => S.visible(el) && !el.parentElement?.closest(S.popupSel));
S.scopeRoot = (popupOnly) => (popupOnly ? S.popups().at(-1) || null : document.body);

// 页面是否稳定：没有加载占位、200ms 内没有 DOM 变化（最多等 timeout）
S.settle = (timeout = 6000) =>
  new Promise((resolve) => {
    const start = performance.now();
    let last = performance.now();
    const mo = new MutationObserver(() => (last = performance.now()));
    mo.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
    const tick = () => {
      const busy = document.querySelector('[aria-busy=true], [data-slot=skeleton], .weir-skeleton, [role=status][data-kind=loading]');
      if ((!busy && performance.now() - last > 200) || performance.now() - start > timeout) {
        mo.disconnect();
        resolve({ waited: Math.round(performance.now() - start), busy: !!busy });
      } else setTimeout(tick, 60);
    };
    setTimeout(tick, 120);
  });

// 页面错误：错误边界、Vite 报错层、记录到的未捕获异常
S.pageErrors = () => {
  const out = S.errors.splice(0);
  const overlay = document.querySelector("vite-error-overlay");
  if (overlay) out.push({ kind: "vite", message: (overlay.shadowRoot?.querySelector(".message")?.textContent || "Vite 报错层").slice(0, 300) });
  const boundary = [...document.querySelectorAll("[role=status], [role=alert]")].find((n) => /页面出错|页面加载失败/.test(n.textContent || ""));
  if (boundary) out.push({ kind: "boundary", message: boundary.textContent.replace(/\s+/g, " ").slice(0, 300) });
  return out;
};

// ---------- 布局跳动 ----------
S.clsStart = () => {
  S.cls = { value: 0, sources: [] };
  S.clsObs?.disconnect();
  S.clsObs = new PerformanceObserver((list) => {
    for (const e of list.getEntries()) {
      if (e.hadRecentInput) continue;
      S.cls.value += e.value;
      for (const s of e.sources || []) if (s.node && S.cls.sources.length < 8) S.cls.sources.push({ ...S.describe(s.node.nodeType === 1 ? s.node : s.node.parentElement || document.body), value: +e.value.toFixed(4) });
    }
  });
  S.clsObs.observe({ type: "layout-shift", buffered: false });
};
S.clsStop = () => {
  S.clsObs?.disconnect();
  S.clsObs = null;
  return S.cls || { value: 0, sources: [] };
};

// ---------- 掉帧 ----------
S.framesStart = () => {
  const f = (S.frames = { longtasks: [], slow: [], frames: 0, run: true, hidden: document.hidden });
  S.ltObs?.disconnect();
  S.ltObs = new PerformanceObserver((list) => {
    for (const e of list.getEntries()) if (e.duration > 50) f.longtasks.push(Math.round(e.duration));
  });
  try {
    S.ltObs.observe({ type: "longtask", buffered: false });
  } catch {}
  let prev = performance.now();
  const loop = (t) => {
    if (!f.run) return;
    const d = t - prev;
    prev = t;
    f.frames++;
    if (f.frames > 1) {
      (f.all ||= []).push(d);
      if (d > 34) f.slow.push(Math.round(d));
      const a = [...f.all].sort((x, y) => x - y);
      f.median = Math.round(a[a.length >> 1]);
    }
    requestAnimationFrame(loop);
  };
  requestAnimationFrame(loop);
};
S.framesStop = () => {
  const f = S.frames || { longtasks: [], slow: [], frames: 0 };
  f.run = false;
  S.ltObs?.disconnect();
  f.hidden = f.hidden || document.hidden;
  return { longtasks: f.longtasks, slow: f.slow, frames: f.frames, hidden: f.hidden, median: f.median };
};

// 单页内切换路由：不整页刷新，保留已注入的脚本与观察器
S.navigate = (url) => {
  history.pushState({}, "", url);
  dispatchEvent(new PopStateEvent("popstate", { state: {} }));
};
