// 交互：找弹层触发按钮、按压反馈取样、键盘 Tab 遍历。真实的按下 / 按键由 Node 一侧经 CDP 派发。
const danger = /删除|移除|撤销|吊销|清空|停用|禁用|重置|退出|注销|恢复|还原|重启|更新到|升级|轮换|断开|卸载|回滚|应用|同步|刷新|测试|发送|拉取|下载|保存|提交|确认|选择文件|上传/;
const labelOf = (el) => (el.getAttribute("aria-label") || el.innerText || el.getAttribute("title") || "").replace(/\s+/g, " ").trim();
const opensFile = (el) => !!(el.closest("label")?.querySelector("input[type=file]") || el.closest("[data-slot=file-picker]"));

// 弹层触发按钮：新建 / 添加类、「更多」菜单、第一个下拉、顶栏按钮（header 为真时）
S.findTriggers = ({ header, max = 7 }) => {
  document.querySelectorAll("[data-weir-trigger]").forEach((n) => n.removeAttribute("data-weir-trigger"));
  const popups = S.popups();
  // 页面内容与顶栏；侧边栏（含个人中心）只在 header 为真时作为「顶栏」类取用
  const inMain = (el) => !el.closest('[aria-label="主导航"]') && !popups.some((p) => p.contains(el));
  const inRail = (el) => !!el.closest("aside, .rail");
  const all = [...document.querySelectorAll("button, [role=button], [role=combobox]")].filter((el) => S.visible(el) && inMain(el) && !el.disabled && el.getAttribute("aria-disabled") !== "true" && !opensFile(el) && !(el.matches("button") && el.type === "submit" && el.form));
  // 表单的提交按钮（「导入」「创建令牌」等）会真的提交，不当作弹层触发按钮
  const out = [];
  const seen = new Set();
  const used = new Set();
  const push = (el, kind, text) => {
    const key = kind + "|" + text;
    if (seen.has(key) || used.has(el) || out.length >= max) return;
    seen.add(key);
    used.add(el);
    el.setAttribute("data-weir-trigger", String(out.length));
    out.push({ idx: out.length, kind, text });
  };
  for (const el of all) {
    const t = labelOf(el).slice(0, 30);
    if (!t || danger.test(t)) continue;
    if (inRail(el)) continue;
    if (/^(新建|添加|创建|导入|邀请|新增)|新建|添加/.test(t) && out.filter((x) => x.kind === "新建").length < 4) push(el, "新建", t);
  }
  for (const el of all) {
    const t = labelOf(el).slice(0, 30);
    if (danger.test(t) || inRail(el)) continue;
    if ((el.getAttribute("aria-haspopup") === "menu" || /^更多|更多操作/.test(t)) && out.filter((x) => x.kind === "菜单").length < 2) push(el, "菜单", t || "更多");
  }
  const sel = all.find((el) => el.matches("[role=combobox], [data-slot=select-trigger]"));
  if (sel) push(sel, "下拉", labelOf(sel).slice(0, 30) || "下拉");
  if (header)
    for (const el of all) {
      const t = el.getAttribute("aria-label") || "";
      if (/^(通知|快速跳转|个人中心|打开导航)/.test(t)) push(el, "顶栏", t.split("：")[0]);
    }
  return out;
};

// ---------- 按压反馈 ----------
const pressSel = 'button, [role=button], a[href], [role=menuitem], [role=option], [role=tab], [role=switch], [role=checkbox], [data-object], [data-slot=card], [data-slot=select-trigger], tr, li';
const reactProps = (el) => {
  const k = Object.keys(el).find((x) => x.startsWith("__reactProps$"));
  return k ? el[k] : null;
};
const pressKind = (el) => {
  const role = el.getAttribute("role");
  if (role === "menuitem" || role === "option") return "菜单项";
  if (role === "tab") return "标签";
  if (el.matches("[data-object], tr, li")) return "列表行";
  if (el.matches("[data-slot=card]")) return "卡片";
  if (el.tagName === "A") return "链接";
  return "按钮";
};
S.pressCandidates = ({ popupOnly, seen, max = 20 }) => {
  document.querySelectorAll("[data-weir-press]").forEach((n) => n.removeAttribute("data-weir-press"));
  const scope = S.scopeRoot(popupOnly);
  if (!scope) return [];
  const seenSet = new Set(seen);
  const out = [];
  for (const el of scope.querySelectorAll(pressSel)) {
    if (out.length >= max) break;
    if (!S.visible(el) || el.disabled || el.getAttribute("aria-disabled") === "true" || el.closest("[data-weir-scan]") || opensFile(el)) continue;
    if (danger.test(labelOf(el).slice(0, 40))) continue;
    // 行与卡片只有挂了点击处理才算可按
    if (el.matches("tr, li, [data-slot=card]") && !el.matches("[data-object]")) {
      const p = reactProps(el);
      if (!p || !(p.onClick || p.onPointerDown || p.onMouseDown)) continue;
    }
    const kind = pressKind(el);
    const sig = [kind, S.component(el), el.tagName, el.getAttribute("role") || "", [...el.classList].filter((c) => !/[:[\]/]/.test(c)).sort().slice(0, 8).join(".")].join("|");
    if (seenSet.has(sig)) continue;
    seenSet.add(sig);
    el.setAttribute("data-weir-press", String(out.length));
    out.push({ id: out.length, sig, kind });
  }
  return out;
};
const snapOne = (el, pseudo) => {
  const cs = getComputedStyle(el, pseudo);
  return [cs.transform, cs.scale, cs.translate, cs.opacity, cs.backgroundColor, cs.boxShadow, cs.filter, cs.borderTopColor, cs.color].join(";");
};
const pressSnap = (el) => {
  const parts = [snapOne(el), snapOne(el, "::before"), snapOne(el, "::after"), el.childElementCount, el.getAnimations({ subtree: true }).length, el.getAttribute("data-pressed") ?? "", el.getAttribute("data-active") ?? ""];
  [...el.querySelectorAll("*")].slice(0, 4).forEach((c) => parts.push(snapOne(c)));
  return parts.join("§");
};
S.pressPrep = (id) => {
  const el = document.querySelector(`[data-weir-press="${id}"]`);
  if (!el) return null;
  let r = el.getBoundingClientRect();
  if (r.top < 0 || r.bottom > innerHeight || r.left < 0 || r.right > innerWidth) {
    el.scrollIntoView({ block: "center", inline: "center" });
    r = el.getBoundingClientRect();
  }
  const x = r.left + r.width / 2, y = r.top + r.height / 2;
  const hit = document.elementFromPoint(x, y);
  return { x, y, hit: !!hit && (hit === el || el.contains(hit)), desc: S.describe(el) };
};
S.pressBefore = (id) => {
  const el = document.querySelector(`[data-weir-press="${id}"]`);
  S.pressBase = el ? pressSnap(el) : "";
  return !!el;
};
// 按下后在 40 / 100 / 180 ms 取样，任一与按下前不同即算有反馈
S.pressSample = async (id) => {
  const el = document.querySelector(`[data-weir-press="${id}"]`);
  if (!el) return { changed: null };
  for (const t of [40, 60, 80]) {
    await new Promise((r) => setTimeout(r, t));
    if (pressSnap(el) !== S.pressBase) return { changed: true };
  }
  return { changed: false };
};
// 取样期间拦掉 click / submit，按压不触发真实操作
const block = (e) => {
  e.preventDefault();
  e.stopImmediatePropagation();
};
S.blockClicks = (on) => {
  for (const t of ["click", "auxclick", "dblclick", "submit"]) (on ? addEventListener : removeEventListener)(t, block, true);
};

// ---------- 键盘 ----------
// 遍历期间关掉过渡，focusin 时样式立即是聚焦态；focusout 时（已失焦）记一次非聚焦态，两者相同即焦点不可见。
const focusSig = (el) => {
  const one = (n, p) => {
    const cs = getComputedStyle(n, p);
    return [cs.outlineStyle === "none" ? "none" : cs.outlineStyle + cs.outlineWidth + cs.outlineColor, cs.boxShadow, cs.borderTopColor, cs.borderBottomColor, cs.backgroundColor, cs.color, cs.textDecorationLine].join(";");
  };
  const parts = [one(el), one(el, "::before"), one(el, "::after")];
  for (let n = el.parentElement, i = 0; n && i < 2; n = n.parentElement, i++) parts.push(one(n));
  return parts.join("§");
};
S.keyStart = ({ popupOnly }) => {
  const style = document.createElement("style");
  style.setAttribute("data-weir-scan", "");
  style.textContent = "*,*::before,*::after{transition:none!important;animation-duration:0s!important;animation-delay:0s!important}";
  document.head.appendChild(style);
  const k = (S.key = { log: [], on: new Map(), off: new Map(), style, popupOnly, scope: S.scopeRoot(popupOnly) });
  k.fin = (e) => {
    const el = e.target;
    if (!(el instanceof Element)) return;
    k.log.push(el);
    if (!k.on.has(el)) k.on.set(el, focusSig(el));
  };
  k.fout = (e) => {
    const el = e.target;
    if (el instanceof Element && !k.off.has(el)) k.off.set(el, focusSig(el));
  };
  document.addEventListener("focusin", k.fin, true);
  document.addEventListener("focusout", k.fout, true);
  if (!popupOnly) {
    // 从页面顶部开始：在 body 开头放一个临时的 tabindex=-1 元素并聚焦
    const s = document.createElement("div");
    s.tabIndex = -1;
    s.setAttribute("data-weir-scan", "");
    s.style.cssText = "position:absolute;top:0;left:0;width:1px;height:1px;outline:none";
    document.body.prepend(s);
    s.focus({ preventScroll: true });
    k.sentinel = s;
    k.log.length = 0;
  }
};
// Node 一侧每按一批 Tab 调一次：回到第一个元素（循环）或焦点离开页面两次即结束
S.keyProgress = () => {
  const k = S.key;
  const log = k.log.filter((el) => !el.hasAttribute("data-weir-scan"));
  const first = log[0];
  const cycled = !!first && log.lastIndexOf(first) > 0;
  const left = document.activeElement === document.body || !document.hasFocus();
  // 非模态弹层（通知、菜单）在焦点移出时自己关闭，这也算走完
  const gone = k.popupOnly && (!k.scope?.isConnected || !S.visible(k.scope));
  return { steps: log.length, cycled, left, gone };
};
S.keyFinish = ({ capped, gone }) => {
  const k = S.key;
  const col = S.collector();
  document.removeEventListener("focusin", k.fin, true);
  document.removeEventListener("focusout", k.fout, true);
  k.style.remove();
  k.sentinel?.remove();
  document.activeElement?.blur?.();
  const visited = new Set(k.log);
  let invisible = 0;
  // base-ui 的焦点守卫（弹层前后的透明 span）只用来把焦点圈在弹层里，不算可见元素
  const guard = (el) => el.hasAttribute("data-base-ui-focus-guard") || (el.tagName === "SPAN" && el.getBoundingClientRect().width < 2);
  for (const el of visited) {
    if (el.hasAttribute("data-weir-scan") || !el.isConnected || guard(el)) continue;
    const on = k.on.get(el), off = k.off.get(el);
    if (on && off && on === off) {
      invisible++;
      col.add("keyboard", el, "焦点不可见", "聚焦前后 outline / box-shadow / 边框 / 背景无变化", "聚焦时有可见焦点环");
    }
  }
  // 无法聚焦：可交互、可见、未禁用，却没被 Tab 走到（组合控件内部按方向键移动，只要求组里至少一个能聚焦）
  let unreachable = 0;
  if (gone || !k.scope?.isConnected) {
    S.key = null;
    return { steps: k.log.length, invisible, unreachable, items: col.list(), gone: true };
  }
  const scope = k.scope;
  const groups = new Map();
  for (const el of scope.querySelectorAll(S.interactiveSel)) {
    if (!S.visible(el) || el.disabled || guard(el) || el.closest("[data-weir-scan], [inert], fieldset:disabled")) continue;
    if (el.getAttribute("aria-disabled") === "true") continue;
    const group = el.parentElement?.closest(S.compositeSel);
    // 菜单、列表框由触发按钮或输入框用方向键 / aria-activedescendant 控制，不要求 Tab 可达
    if (group && group.matches("[role=menu], [role=listbox], [role=menubar]")) continue;
    if (group) {
      if (!groups.has(group)) groups.set(group, false);
      if (visited.has(el)) groups.set(group, true);
      continue;
    }
    if (visited.has(el) || [...visited].some((v) => el.contains(v))) continue;
    const ti = el.tabIndex;
    if (ti < 0 || !capped) {
      unreachable++;
      col.add("keyboard", el, "无法聚焦", ti < 0 ? `tabindex ${ti}` : "Tab 遍历一圈没有到达", "可用 Tab 聚焦");
    }
  }
  for (const [g, ok] of groups) if (!ok && !capped) col.add("keyboard", g, "组合控件无法聚焦", "组内没有元素被 Tab 走到", "组内至少一项可用 Tab 聚焦");
  S.key = null;
  return { steps: k.log.length, invisible, unreachable, items: col.list() };
};

// 每个状态扫完后复位滚动位置，下一个状态从顶部开始
S.resetScroll = () => {
  scrollTo(0, 0);
  for (const el of document.querySelectorAll("*")) if (el.scrollTop || el.scrollLeft) {
    el.scrollTop = 0;
    el.scrollLeft = 0;
  }
};

// 一次跑完静态检查
S.runStatic = ({ checks, popupOnly, narrow }) => {
  const scope = S.scopeRoot(popupOnly);
  if (!scope) return { missing: true };
  const tok = S.tokens();
  const col = S.collector();
  const stats = { elements: 0, gradients: 0, inlineLinks: 0, tapTargets: 0, occluded: 0, contrastChecked: 0, contrastUnknown: 0 };
  const combos = {};
  const has = (c) => checks.includes(c);
  if (has("tokens")) S.checkTokens(scope, tok, col, stats);
  if (has("type")) S.checkType(scope, tok, col, combos);
  if (has("tap") && narrow) S.checkTap(scope, col, stats);
  if (has("contrast")) S.checkContrast(scope, col, stats);
  if (has("motion")) S.checkMotion(scope, tok, col);
  let overflow = null;
  if (has("overflow") && narrow) overflow = S.checkOverflow(scope, col);
  if (has("scroll")) S.checkScroll(scope, col);
  return { items: col.list(), stats, combos, overflow, dark: tok.dark, tokenCount: tok.names.length, typeTokens: tok.sizes.length, durations: [...tok.durations], radii: [...tok.radii] };
};
