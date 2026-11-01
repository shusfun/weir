// 静态检查：设计变量、字号层级、动效、横向溢出、滚动条。只读计算样式，不操作页面。
// iOS 文字层级（pt 当 px 用）：设计变量里没有成对的字号 / 行高变量时，按它判定「字号层级」
S.iosType = [
  ["Large Title", 34, 41], ["Title 1", 28, 34], ["Title 2", 22, 28], ["Title 3", 20, 25], ["Headline/Body", 17, 22],
  ["Callout", 16, 21], ["Subheadline", 15, 20], ["Footnote", 13, 18], ["Caption 1", 12, 16], ["Caption 2", 11, 13],
];
const layoutProps = new Set(["all", "width", "height", "min-width", "max-width", "min-height", "max-height", "top", "left", "right", "bottom", "inset", "inset-inline", "inset-block", "margin", "margin-top", "margin-bottom", "margin-left", "margin-right", "margin-inline", "margin-block", "padding", "padding-top", "padding-bottom", "padding-left", "padding-right", "padding-inline", "padding-block", "flex-basis", "grid-template-columns", "grid-template-rows", "gap", "font-size", "line-height", "border-width"]);
const hasOwnText = (el) => {
  if (el.matches("input:not([type=checkbox]):not([type=radio]):not([type=range]), textarea, select")) return true;
  for (const n of el.childNodes) if (n.nodeType === 3 && n.textContent.trim()) return true;
  return false;
};
S.hasOwnText = hasOwnText;
const autofilled = (el) => {
  if (el.tagName !== "INPUT") return false;
  for (const sel of [":autofill", ":-webkit-autofill"]) {
    try {
      if (el.matches(sel)) return true;
    } catch {}
  }
  return false;
};
// 收集范围内的可见元素；SVG 内部的图形元素跳过（颜色随 currentColor）
S.elements = (scope) => [...scope.querySelectorAll("*")].filter((el) => !(el.closest("svg") && el.tagName.toLowerCase() !== "svg") && !el.closest("[data-weir-scan]") && S.visible(el));

// 收集器：同一元素、同一检查、同一实测值只记一条，count 记出现次数
S.collector = () => {
  const map = new Map();
  return {
    add(check, el, prop, actual, expected, extra) {
      const d = el ? S.describe(el) : { tag: "", role: "", text: "", selector: "(页面)", component: "(页面)" };
      const key = [check, prop, actual, d.selector, d.text].join("|");
      const hit = map.get(key);
      if (hit) hit.count++;
      else map.set(key, { check, prop, actual: String(actual), expected: String(expected), el: d, count: 1, ...(extra || {}) });
    },
    list: () => [...map.values()],
  };
};

// 设计变量：color / background-color / border-color / box-shadow 颜色、圆角、字体族。
// 继承来的值只记在第一个设置它的祖先上（向上找到值不同的父元素为止），避免同一处违规在子元素上重复出现。
S.checkTokens = (scope, tok, col, stats) => {
  const els = S.elements(scope);
  const originOf = (el, prop) => {
    const v = getComputedStyle(el)[prop];
    let o = el;
    while (o.parentElement && o.parentElement !== document.documentElement && getComputedStyle(o.parentElement)[prop] === v) o = o.parentElement;
    return o;
  };
  const seenColor = new Set();
  const seenFont = new Set();
  const fontOk = (f) => tok.fonts.size === 0 || tok.fonts.has(S.normFont(f));
  for (const el of els) {
    const cs = getComputedStyle(el);
    stats.elements++;
    if (hasOwnText(el)) {
      const c = S.rgba(cs.color);
      if (c && c[3] > 0.004 && !S.colorToken(tok, c)) {
        const o = originOf(el, "color");
        if (!seenColor.has(o)) {
          seenColor.add(o);
          col.add("tokens", o, "color", S.colorText(c), "--weir-* 颜色");
        }
      }
      if (!fontOk(cs.fontFamily)) {
        const o = originOf(el, "fontFamily");
        if (!seenFont.has(o)) {
          seenFont.add(o);
          col.add("tokens", o, "font-family", cs.fontFamily.slice(0, 60), "--weir-font / --weir-mono");
        }
      }
    }
    if (cs.backgroundImage !== "none" && /gradient/.test(cs.backgroundImage)) stats.gradients++;
    const bg = S.rgba(cs.backgroundColor);
    // 浏览器自动填充的输入框：背景色由浏览器的 UA 样式（!important）给出，页面改不了，不算违规
    if (bg && bg[3] > 0.004 && !S.colorToken(tok, bg) && !autofilled(el)) col.add("tokens", el, "background-color", S.colorText(bg), "--weir-* 颜色");
    for (const side of ["Top", "Right", "Bottom", "Left"]) {
      if (parseFloat(cs[`border${side}Width`]) <= 0 || cs[`border${side}Style`] === "none" || cs[`border${side}Style`] === "hidden") continue;
      const bc = S.rgba(cs[`border${side}Color`]);
      if (bc && bc[3] > 0.004 && !S.colorToken(tok, bc)) {
        col.add("tokens", el, "border-color", S.colorText(bc), "--weir-* 颜色");
        break;
      }
    }
    if (cs.boxShadow && cs.boxShadow !== "none") {
      for (const layer of cs.boxShadow.split(/,(?![^(]*\))/)) {
        const m = layer.match(/(rgba?\([^)]*\)|oklch\([^)]*\)|oklab\([^)]*\)|color\([^)]*\)|#[0-9a-f]{3,8})/i);
        const sc = m && S.rgba(m[1]);
        if (sc && sc[3] > 0.004 && !S.colorToken(tok, sc)) {
          col.add("tokens", el, "box-shadow", layer.trim().slice(0, 70), "阴影颜色取 --weir-* 变量");
          break;
        }
      }
    }
    const r = el.getBoundingClientRect();
    const half = Math.min(r.width, r.height) / 2;
    for (const corner of ["TopLeft", "TopRight", "BottomRight", "BottomLeft"]) {
      const v = cs[`border${corner}Radius`];
      if (v === "0px" || tok.radii.has(v) || /%/.test(v)) continue;
      if (parseFloat(v) >= half - 0.5) continue; // 胶囊 / 圆形
      col.add("tokens", el, "border-radius", v, [...tok.radii].join(" / ") + " 或胶囊");
      break;
    }
  }
};

// 字号层级的取值：tokens.css 里成对的 --weir-text-<名> / --weir-leading-<名>（按当前视口与主题在运行时读取，
// 触屏下角色变量取不同层级也随之变化）；同一组字号 / 行高只留一条。没有成对的字号变量时退回写死的 iOS 层级。
S.typeTiers = (tok) => {
  const px = new Map(tok.sizes.map((x) => [x.name, x.px]));
  const seen = new Set();
  const tiers = [];
  for (const [name, size] of px) {
    const m = name.match(/^--weir-text-(.+)$/);
    const lh = m && px.get(`--weir-leading-${m[1]}`);
    if (!lh || seen.has(`${size}/${lh}`)) continue;
    seen.add(`${size}/${lh}`);
    tiers.push([m[1], size, lh]);
  }
  return tiers.length ? tiers.sort((a, b) => b[1] - a[1]) : S.iosType;
};

// 字号层级：统计出现过的 font-size/line-height 组合，字号与行高都要落在同一个层级上（行高允许 1px 误差）
S.checkType = (scope, tok, col, combos) => {
  const tiers = S.typeTiers(tok);
  const expected = (tiers === S.iosType ? "iOS 层级 " : "字号变量 ") + tiers.map(([, s, l]) => `${s}/${l}`).join("、");
  for (const el of S.elements(scope)) {
    if (!hasOwnText(el)) continue;
    const cs = getComputedStyle(el);
    const fs = Math.round(parseFloat(cs.fontSize) * 10) / 10;
    const lh = cs.lineHeight === "normal" ? "normal" : Math.round(parseFloat(cs.lineHeight) * 10) / 10;
    const key = `${fs}/${lh}`;
    combos[key] = (combos[key] || 0) + 1;
    const ok = tiers.some(([, s, l]) => Math.abs(s - fs) < 0.6 && lh !== "normal" && Math.abs(l - lh) <= 1);
    if (!ok) col.add("type", el, "font-size/line-height", key, expected);
  }
};

// 动效：过渡的属性不能是布局属性（width/height/top/left/margin/padding/all 等），时长要等于某个时长变量
S.checkMotion = (scope, tok, col) => {
  const durOk = (ms) => ms === 0 || [...tok.durations].some((d) => Math.abs(d - ms) < 1);
  const durList = [...tok.durations].filter(Boolean).map((d) => d + "ms").join(" / ");
  const toMs = (s) => (s.trim().endsWith("ms") ? parseFloat(s) : parseFloat(s) * 1000);
  for (const el of S.elements(scope)) {
    const cs = getComputedStyle(el);
    const props = cs.transitionProperty.split(",").map((s) => s.trim());
    const durs = cs.transitionDuration.split(",").map(toMs);
    props.forEach((p, i) => {
      const d = durs[i % durs.length];
      if (!d || p === "none") return;
      // 豁免：折叠区面板按 Base UI 的 --collapsible-panel-height 过渡 height，内容高度未知只能这样展开（design.html#console-visual）
      if (p === "height" && el.matches("[data-slot=collapsible-panel]")) return;
      if (layoutProps.has(p)) col.add("motion", el, "transition-property", p, "只过渡 transform / opacity / 颜色");
      if (!durOk(d)) col.add("motion", el, "transition-duration", `${p} ${d}ms`, durList);
    });
    if (cs.animationName && cs.animationName !== "none") {
      const ad = cs.animationDuration.split(",").map(toMs);
      cs.animationName.split(",").forEach((n, i) => {
        const d = ad[i % ad.length];
        if (d && !durOk(d) && cs.animationIterationCount.split(",")[i % cs.animationIterationCount.split(",").length].trim() !== "infinite")
          col.add("motion", el, "animation-duration", `${n.trim()} ${d}ms`, durList);
      });
    }
  }
  // 正在运行的动画：检查关键帧里的属性
  for (const a of document.getAnimations()) {
    const el = a.effect?.target;
    if (!el || !(el instanceof Element) || !scope.contains(el)) continue;
    const frames = a.effect.getKeyframes?.() || [];
    const bad = new Set();
    for (const f of frames) for (const k of Object.keys(f)) if (layoutProps.has(k.replace(/[A-Z]/g, (m) => "-" + m.toLowerCase()))) bad.add(k);
    if (bad.size) col.add("motion", el, "animation-keyframes", [...bad].join(","), "关键帧只用 transform / opacity");
  }
};

// 横向溢出（只在窄屏判定）：页面 scrollWidth > clientWidth，并找出超出视口、没有被可滚动 / 裁剪祖先包住的元素
S.checkOverflow = (scope, col) => {
  const se = document.scrollingElement;
  const vw = document.documentElement.clientWidth;
  const out = { scrollWidth: se.scrollWidth, clientWidth: se.clientWidth, culprits: [] };
  const clipped = (el) => {
    for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
      const ncs = getComputedStyle(n);
      // 被裁剪 / 可滚动的祖先包住，或在固定定位的层里（不参与页面滚动宽度）
      if (ncs.overflowX !== "visible" || ncs.position === "fixed") return true;
    }
    return false;
  };
  const els = S.elements(scope).filter((el) => {
    const r = el.getBoundingClientRect();
    return (r.right > vw + 1 || r.left < -1) && !clipped(el) && getComputedStyle(el).position !== "fixed";
  });
  const set = new Set(els);
  for (const el of els) if (!set.has(el.parentElement)) out.culprits.push(el);
  if (se.scrollWidth > se.clientWidth) {
    col.add("overflow", null, "scrollWidth", `${se.scrollWidth} > ${se.clientWidth}`, "scrollWidth ≤ clientWidth");
    for (const el of out.culprits.slice(0, 10)) {
      const r = el.getBoundingClientRect();
      col.add("overflow", el, "超出视口", `left ${Math.round(r.left)} right ${Math.round(r.right)}（视口 ${vw}）`, "0 ≤ left，right ≤ " + vw);
    }
  }
  // 弹层自身超出视口也记下（弹层通常是 fixed，页面不会出现横向滚动）
  for (const p of S.popups()) {
    const r = p.getBoundingClientRect();
    if (r.right > vw + 1 || r.left < -1) col.add("overflow", p, "弹层超出视口", `left ${Math.round(r.left)} right ${Math.round(r.right)}`, "0 ≤ left，right ≤ " + vw);
  }
  return { scrollWidth: se.scrollWidth, clientWidth: se.clientWidth };
};

// 滚动条：每个可滚动容器比较 offsetWidth 与 clientWidth（扣掉边框），差值 > 0 即滚动条占位且静止时可见；
// base-ui ScrollArea 的覆盖式滚动条看 scrollbar 元素在没有悬停 / 滚动时的不透明度
S.checkScroll = (scope, col) => {
  const list = [];
  const root = document.documentElement;
  const rootGutter = window.innerWidth - root.clientWidth;
  if (root.scrollHeight > root.clientHeight && rootGutter > 0) col.add("scroll", null, "页面滚动条占位", `${rootGutter}px`, "0px（覆盖式滚动条）");
  for (const el of S.elements(scope)) {
    const cs = getComputedStyle(el);
    const oy = cs.overflowY, ox = cs.overflowX;
    const sy = el.scrollHeight > el.clientHeight + 1 && /(auto|scroll|overlay)/.test(oy);
    const sx = el.scrollWidth > el.clientWidth + 1 && /(auto|scroll|overlay)/.test(ox);
    if (!sy && !sx) continue;
    const gutterY = el.offsetWidth - el.clientWidth - parseFloat(cs.borderLeftWidth) - parseFloat(cs.borderRightWidth);
    const gutterX = el.offsetHeight - el.clientHeight - parseFloat(cs.borderTopWidth) - parseFloat(cs.borderBottomWidth);
    list.push(S.describe(el).selector);
    if (sy && gutterY > 0.5) col.add("scroll", el, "纵向滚动条占位", `${Math.round(gutterY)}px（scrollbar-width: ${cs.scrollbarWidth}）`, "0px，静止时不可见");
    if (sx && gutterX > 0.5) col.add("scroll", el, "横向滚动条占位", `${Math.round(gutterX)}px（scrollbar-width: ${cs.scrollbarWidth}）`, "0px，静止时不可见");
  }
  for (const bar of scope.querySelectorAll("[data-slot=scroll-area-scrollbar]")) {
    if (bar.hasAttribute("data-hovering") || bar.hasAttribute("data-scrolling")) continue;
    const cs = getComputedStyle(bar);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const r = bar.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    if (parseFloat(cs.opacity) > 0.05) col.add("scroll", bar, "覆盖式滚动条静止可见", `opacity ${cs.opacity}`, "静止时 opacity 0");
  }
  return list.length;
};
