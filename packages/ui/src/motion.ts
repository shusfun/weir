// 动效档位：组件不自己猜。html[data-motion] 为 full / balanced / lite；没写时跟随系统
// （减少动态效果或省流量 → lite，否则 balanced）。prefersReducedMotion 并进这里：只有省电档为真。
import { useSyncExternalStore } from "react";

export type MotionTier = "full" | "balanced" | "lite";

const reduceQuery = "(prefers-reduced-motion: reduce)";

function systemTier(): MotionTier {
  if (typeof window === "undefined") return "balanced";
  const reduce = !!window.matchMedia?.(reduceQuery).matches;
  const save = !!(navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData;
  return reduce || save ? "lite" : "balanced";
}

function readMotionTier(): MotionTier {
  if (typeof document === "undefined") return "balanced";
  const set = document.documentElement.dataset.motion;
  if (set === "full" || set === "balanced" || set === "lite") return set;
  return systemTier();
}

function prefersReducedMotion() {
  return readMotionTier() === "lite";
}

function subscribeMotion(onChange: () => void) {
  const media = window.matchMedia?.(reduceQuery);
  media?.addEventListener("change", onChange);
  const conn = (navigator as Navigator & { connection?: EventTarget }).connection;
  conn?.addEventListener?.("change", onChange);
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-motion"] });
  return () => {
    media?.removeEventListener("change", onChange);
    conn?.removeEventListener?.("change", onChange);
    observer.disconnect();
  };
}

function usePrefersReducedMotion() {
  return useSyncExternalStore(subscribeMotion, prefersReducedMotion, () => false);
}

export { prefersReducedMotion, readMotionTier, subscribeMotion, usePrefersReducedMotion };
