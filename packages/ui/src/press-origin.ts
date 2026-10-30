// 按压起点：记下最近一次按下的位置（指针坐标；键盘 Enter / 空格时为聚焦元素的中心），
// 弹窗、二级弹窗据此从点击的地方展开、收回到那里。只认 800 ms 内的按压，程序自己打开的弹窗（地址栏、dsh）从原位淡入。
type Origin = { x: number; y: number; at: number };

let last: Origin | null = null;
let installed = false;

function install() {
  if (installed || typeof window === "undefined") return;
  installed = true;
  window.addEventListener(
    "pointerdown",
    (e) => {
      if (e.button === 0) last = { x: e.clientX, y: e.clientY, at: performance.now() };
    },
    true,
  );
  window.addEventListener(
    "keydown",
    (e) => {
      if (e.key !== "Enter" && e.key !== " ") return;
      const el = e.target as Element | null;
      if (!el || !(el instanceof HTMLElement) || el === document.body) return;
      const r = el.getBoundingClientRect();
      last = { x: r.left + r.width / 2, y: r.top + r.height / 2, at: performance.now() };
    },
    true,
  );
}
install();

// pressOrigin 返回最近一次按压的位置；超过 800 ms 视为不是这次打开的起点。
function pressOrigin(): { x: number; y: number } | null {
  if (!last || performance.now() - last.at > 800) return null;
  return { x: last.x, y: last.y };
}

// originFor 把按压点换算成元素自身坐标里的 transform-origin（元素此刻可能带着起始缩放，用布局尺寸换算）。
function originFor(el: HTMLElement, p: { x: number; y: number }): string {
  const r = el.getBoundingClientRect();
  const w = el.offsetWidth || r.width;
  const h = el.offsetHeight || r.height;
  const left = r.left + r.width / 2 - w / 2;
  const top = r.top + r.height / 2 - h / 2;
  return `${Math.round(p.x - left)}px ${Math.round(p.y - top)}px`;
}

export { pressOrigin, originFor };
