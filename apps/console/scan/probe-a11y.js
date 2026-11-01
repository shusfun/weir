// 可达性：点击区域（窄屏 44×44）与文字对比度（4.5，大字 3）。
// 组合控件：组内用方向键移动（roving tabindex），按组判定是否可达；分段拨钮 Segmented 也按组算
const compositeSel = "[role=tablist], [role=radiogroup], [role=menu], [role=menubar], [role=listbox], [role=grid], [role=tree], [role=toolbar], [data-slot=segmented]";
S.compositeSel = compositeSel;

// 视觉上被隐藏的原生勾选 / 单选：命中区域按它的 label 或包裹元素算。
// 包在 <label> 里的勾选、单选、开关（CheckboxField / SwitchField / OptionRow 整行可点）：点到 label 任何位置都会切换，
// 命中区域按整个 label 算，不只看控件本身
const realTarget = (el) => {
  const r = el.getBoundingClientRect();
  const cs = getComputedStyle(el);
  if (el.matches("input[type=checkbox], input[type=radio]") && (r.width < 2 || r.height < 2 || parseFloat(cs.opacity) < 0.05)) {
    return (el.id && document.querySelector(`label[for="${CSS.escape(el.id)}"]`)) || el.closest("label") || el.parentElement;
  }
  if (el.matches("[role=checkbox], [role=radio], [role=switch]")) {
    const label = el.closest("label");
    if (label && !label.querySelector("button, a[href], input:not([type=checkbox]):not([type=radio]):not([type=hidden])")) return label;
  }
  return el;
};
// 正文里的行内链接按 WCAG 2.5.8 例外处理，只计数不判违规
const inlineLink = (el) => el.tagName === "A" && getComputedStyle(el).display === "inline" && (el.parentElement?.innerText || "").trim().length > (el.innerText || "").trim().length + 8;

// 命中区域：几何尺寸不足 44 时，用 elementFromPoint 从中心向外探测，伪元素扩大的命中区也算进去
const hitExtent = (el, r) => {
  const cx = r.left + r.width / 2, cy = r.top + r.height / 2;
  const hits = (x, y) => {
    if (x < 0 || y < 0 || x >= innerWidth || y >= innerHeight) return false;
    const t = document.elementFromPoint(x, y);
    return !!t && (t === el || el.contains(t));
  };
  if (!hits(cx, cy)) return null; // 被遮挡
  const span = (dx, dy) => {
    let n = 0;
    for (let s = 1; s <= 30; s++) if (hits(cx + dx * s, cy + dy * s)) n = s; else break;
    return n;
  };
  return { w: span(-1, 0) + span(1, 0) + 1, h: span(0, -1) + span(0, 1) + 1 };
};

// 视口外的元素无法用 elementFromPoint 探测，只看几何尺寸会把靠伪元素扩到 44 的控件（开关、勾选、ObjectLink）误报；
// 先滚到视口中央再探测，全部判完后恢复各滚动容器原来的位置
const inViewport = (r) => r.top >= 0 && r.left >= 0 && r.bottom <= innerHeight && r.right <= innerWidth;
const scrollState = () => [...document.querySelectorAll("*")].filter((n) => n.scrollTop || n.scrollLeft).map((n) => [n, n.scrollTop, n.scrollLeft]);

S.checkTap = (scope, col, stats) => {
  const saved = { x: scrollX, y: scrollY, inner: scrollState() };
  let scrolled = false;
  for (const el0 of scope.querySelectorAll(S.interactiveSel)) {
    if (el0.closest("[data-weir-scan]")) continue;
    const el = realTarget(el0);
    if (!el || !S.visible(el)) continue;
    if (inlineLink(el)) {
      stats.inlineLinks++;
      continue;
    }
    stats.tapTargets++;
    let r = el.getBoundingClientRect();
    // 尺寸取布局尺寸（offset*，不受 transform 影响）与外框的较大者：弹窗打开的 0.96→1 缩放在浏览器窗口
    // 被遮挡或最小化时会停在半途（过渡被限流），只看外框会把 44 量成 42
    const lw = el.offsetWidth ?? 0, lh = el.offsetHeight ?? 0;
    if (Math.max(r.width, lw) >= 44 && Math.max(r.height, lh) >= 44) continue;
    if (!inViewport(r)) {
      el.scrollIntoView({ block: "center", inline: "nearest", behavior: "instant" });
      scrolled = true;
      r = el.getBoundingClientRect();
    }
    let w = r.width, h = r.height, how = "几何尺寸";
    // 滚动后仍在视口外（被裁剪的滚动区、固定定位层后面）才退回只看几何尺寸
    if (inViewport(r)) {
      const ext = hitExtent(el, r);
      if (ext === null) {
        stats.occluded++;
        continue;
      }
      if (ext.w > w + 1 || ext.h > h + 1) how = "含伪元素扩展";
      w = Math.max(w, ext.w);
      h = Math.max(h, ext.h);
    }
    if (w < 44 || h < 44) col.add("tap", el, "命中区域", `${Math.round(w)}×${Math.round(h)}（${how}）`, "≥ 44×44");
  }
  if (scrolled) {
    for (const n of document.querySelectorAll("*")) if (n.scrollTop || n.scrollLeft) n.scrollTop = n.scrollLeft = 0;
    for (const [n, t, l] of saved.inner) {
      n.scrollTop = t;
      n.scrollLeft = l;
    }
    scrollTo({ left: saved.x, top: saved.y, behavior: "instant" });
  }
};

// ---------- 对比度 ----------
const lum = ([r, g, b]) => {
  const f = (v) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
};
const over = (top, under) => {
  const a = top[3];
  return [0, 1, 2].map((i) => top[i] * a + under[i] * (1 - a)).concat(1);
};
// 向上找背景：叠加半透明层直到第一个不透明背景，返回可能的背景色列表（取最差的一个算对比度）。
// 渐变背景取各个色标；图片背景无法判定，返回 null
const stops = (img) => [...img.matchAll(/(rgba?\([^)]*\)|oklch\([^)]*\)|oklab\([^)]*\)|color\([^)]*\)|#[0-9a-f]{3,8}|transparent)/gi)].map((m) => S.rgba(m[1])).filter(Boolean);
const backdrop = (el) => {
  const layers = []; // 自上而下，每层是候选色数组
  let closed = false;
  for (let n = el; n; n = n.parentElement) {
    const cs = getComputedStyle(n);
    if (cs.backgroundImage !== "none") {
      if (!/gradient/.test(cs.backgroundImage)) return null;
      const st = stops(cs.backgroundImage);
      if (!st.length) return null;
      layers.push(st);
      if (st.every((c) => c[3] >= 0.999)) {
        closed = true;
        break;
      }
    }
    const c = S.rgba(cs.backgroundColor);
    if (c && c[3] > 0.004) {
      layers.push([c]);
      if (c[3] >= 0.999) {
        closed = true;
        break;
      }
    }
  }
  let bases = closed ? layers.pop().map((c) => c.slice()) : [S.rgba(getComputedStyle(document.documentElement).backgroundColor) || [255, 255, 255, 1]];
  bases = bases.map((b) => (b[3] < 0.999 ? over(b, [255, 255, 255, 1]) : b));
  for (let i = layers.length - 1; i >= 0; i--) {
    const next = [];
    for (const b of bases) for (const c of layers[i]) next.push(over(c, b));
    bases = next.slice(0, 12);
  }
  return bases;
};
const opacityChain = (el) => {
  let o = 1;
  for (let n = el; n; n = n.parentElement) o *= parseFloat(getComputedStyle(n).opacity);
  return o;
};

S.checkContrast = (scope, col, stats) => {
  for (const el of S.elements(scope)) {
    if (!S.hasOwnText(el)) continue;
    if (el.closest(":disabled, [aria-disabled=true], [data-disabled]")) continue;
    const cs = getComputedStyle(el);
    const fg0 = S.rgba(cs.color);
    if (!fg0) continue;
    const op = opacityChain(el);
    if (op < 0.1) continue; // 正在淡入淡出或隐藏
    const bg = backdrop(el);
    if (!bg) {
      stats.contrastUnknown++;
      continue;
    }
    stats.contrastChecked++;
    let ratio = Infinity, fg, worst;
    for (const b of bg) {
      const f = over([fg0[0], fg0[1], fg0[2], fg0[3] * op], b);
      const L1 = lum(f), L2 = lum(b);
      const r = (Math.max(L1, L2) + 0.05) / (Math.min(L1, L2) + 0.05);
      if (r < ratio) [ratio, fg, worst] = [r, f, b];
    }
    const px = parseFloat(cs.fontSize);
    const large = px >= 24 || (px >= 18.66 && parseInt(cs.fontWeight) >= 700);
    const need = large ? 3 : 4.5;
    if (ratio < need - 0.005) col.add("contrast", el, "对比度", `${ratio.toFixed(2)}（文字 ${S.colorText(fg)} / 背景 ${S.colorText(worst)}${bg.length > 1 ? "（渐变最差色标）" : ""}，${px}px）`, `≥ ${need}`);
  }
};
