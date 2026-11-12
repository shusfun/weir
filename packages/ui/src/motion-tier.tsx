// 动效档位上下文。不传 tier 时读 html[data-motion]（见 motion.ts）。
// apply 才把档位写回 <html>，让 CSS 的 [data-motion=lite] 与 JS 一致；子树覆盖只影响 useMotionTier。
import { createContext, useContext, useEffect, type ReactNode } from "react";
import { useSyncExternalStore } from "react";
import { readMotionTier, subscribeMotion, type MotionTier } from "./motion";

const MotionContext = createContext<MotionTier | null>(null);

function useObservedTier() {
  return useSyncExternalStore(subscribeMotion, readMotionTier, () => "balanced" as MotionTier);
}

function MotionProvider({
  tier,
  apply = false,
  children,
}: {
  tier?: MotionTier | "system";
  apply?: boolean;
  children: ReactNode;
}) {
  const observed = useObservedTier();
  const value: MotionTier = tier == null || tier === "system" ? observed : tier;
  useEffect(() => {
    if (!apply || tier == null) return;
    const root = document.documentElement;
    const prev = root.dataset.motion;
    if (tier === "system") delete root.dataset.motion;
    else root.dataset.motion = tier;
    return () => {
      if (prev == null) delete root.dataset.motion;
      else root.dataset.motion = prev;
    };
  }, [apply, tier]);
  return <MotionContext.Provider value={value}>{children}</MotionContext.Provider>;
}

function useMotionTier() {
  const ctx = useContext(MotionContext);
  const observed = useObservedTier();
  return ctx ?? observed;
}

export { MotionProvider, useMotionTier };
