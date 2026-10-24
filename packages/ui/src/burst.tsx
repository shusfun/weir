// 点击爆散：关键的那一下值得一次爆发，反馈的分量要配得上动作。只留给少数关键时刻
// （完成初始化、第一次连上设备），日常操作不用。
// burst(origin)：origin 可以是元素（从中心散开）、指针事件（从按压点散开）或坐标。
// 粒子挂在 body 上、position: fixed，不受容器 overflow 裁剪；约 700ms 后自动移除，同时最多一组。
// 减少动态效果时不播放。
import { useCallback, useEffect, useRef } from "react";
import { prefersReducedMotion } from "./motion";

type BurstOrigin = Element | { clientX: number; clientY: number } | { x: number; y: number };
type BurstOptions = { count?: number; distance?: number; colors?: string[] };

const defaultColors = ["var(--mg-accent)", "var(--mg-ok)", "var(--mg-warn)", "var(--mg-info)"];

function center(el: Element) {
  const r = el.getBoundingClientRect();
  return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
}

function point(origin: BurstOrigin) {
  if (origin instanceof Element) return center(origin);
  if ("clientX" in origin) {
    // 键盘触发的 click（detail = 0）没有指针坐标，改从被按下的控件中心散开
    if (origin instanceof MouseEvent && origin.detail === 0 && origin.target instanceof Element)
      return center(origin.target.closest("button, a, [role=button]") ?? origin.target);
    return { x: origin.clientX, y: origin.clientY };
  }
  return origin;
}

function useBurst() {
  const live = useRef<HTMLElement | null>(null);
  useEffect(() => () => live.current?.remove(), []);
  return useCallback((origin: BurstOrigin, { count = 12, distance = 56, colors = defaultColors }: BurstOptions = {}) => {
    if (prefersReducedMotion() || typeof document === "undefined") return;
    live.current?.remove();
    const { x, y } = point(origin);
    const host = document.createElement("div");
    host.className = "mg-burst";
    host.setAttribute("aria-hidden", "true");
    host.style.left = x + "px";
    host.style.top = y + "px";
    const ring = document.createElement("span");
    ring.className = "mg-burst-ring";
    host.appendChild(ring);
    for (let i = 0; i < count; i += 1) {
      const p = document.createElement("span");
      // 均匀分布再加一点抖动，避免看起来像机械的星形
      const angle = (360 / count) * i + (Math.random() * 16 - 8);
      const reach = distance * (0.7 + Math.random() * 0.5);
      if (i % 3 === 1) p.dataset.shape = "bar";
      p.style.setProperty("--mg-burst-angle", angle + "deg");
      p.style.setProperty("--mg-burst-distance", reach + "px");
      p.style.setProperty("--mg-burst-color", colors[i % colors.length]);
      p.style.setProperty("--mg-burst-size", 5 + (i % 2) * 2 + "px");
      host.appendChild(p);
    }
    document.body.appendChild(host);
    live.current = host;
    window.setTimeout(() => {
      host.remove();
      if (live.current === host) live.current = null;
    }, 760);
  }, []);
}

export { useBurst };
export type { BurstOrigin, BurstOptions };
