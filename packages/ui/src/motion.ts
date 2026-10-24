// 动效公共工具：读取“减少动态效果”偏好。CSS 动画已由 theme.css 全局压到 1ms，
// 这里给 JS 驱动的动效（水波、爆散、拖动惯性）用：偏好开启时直接跳到终态，不插入动画元素。
import { useSyncExternalStore } from "react";

const query = "(prefers-reduced-motion: reduce)";

function prefersReducedMotion() {
  return typeof window !== "undefined" && !!window.matchMedia?.(query).matches;
}

function subscribe(onChange: () => void) {
  const media = window.matchMedia?.(query);
  media?.addEventListener("change", onChange);
  return () => media?.removeEventListener("change", onChange);
}

function usePrefersReducedMotion() {
  return useSyncExternalStore(subscribe, prefersReducedMotion, () => false);
}

export { prefersReducedMotion, usePrefersReducedMotion };
