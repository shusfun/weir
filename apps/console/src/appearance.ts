// 外观（主题、动效档位、卡顿提示、背景地球）跟着账号保存。首屏先读本机缓存，不等接口、不闪；登录后用账号上的值校正。
import type { ThemeMode } from "./theme";
import { applyTheme, readTheme } from "./theme";
import { api } from "./api";

type AccountAppearance = { theme: ThemeMode; motion: MotionChoice; follow_reduce: boolean; stutter: boolean; globe: boolean };

function toAccount(a: Appearance): AccountAppearance {
  return { theme: a.theme, motion: a.motion, follow_reduce: a.followReduce, stutter: a.stutter, globe: a.globe };
}

// syncAccountAppearance 登录后调用：账号上有就覆盖本机缓存；没有就把本机的补存上去。失败不影响使用，只保留本机值。
export async function syncAccountAppearance() {
  try {
    const r = (await api("/me/appearance")) as { saved: boolean; appearance?: AccountAppearance };
    if (r.saved && r.appearance) {
      const a = r.appearance;
      const next = { theme: a.theme, motion: a.motion, followReduce: a.follow_reduce, stutter: a.stutter, globe: a.globe };
      writeLocal(next);
      applyAppearance(next);
      return;
    }
    await api("/me/appearance", "PUT", toAccount(readAppearance()));
  } catch {
    // 中心暂时不可用：继续用本机缓存，下次登录再同步
  }
}

// pushAccountAppearance 保存到账号；返回失败原因，调用方提示「只保存在这台浏览器」。
export async function pushAccountAppearance(next: Appearance): Promise<string> {
  try {
    await api("/me/appearance", "PUT", toAccount(next));
    return "";
  } catch (e) {
    return e instanceof Error ? e.message : String(e);
  }
}

export type MotionChoice = "full" | "balanced" | "lite";

const motionKey = "weir-motion";
const followKey = "weir-motion-follow";
const stutterKey = "weir-stutter-hint";
const globeKey = "weir-globe";
const stutterAtKey = "weir-stutter-at";

export type Appearance = {
  theme: ThemeMode;
  motion: MotionChoice;
  followReduce: boolean;
  stutter: boolean;
  globe: boolean;
};

export function readAppearance(): Appearance {
  return {
    theme: readTheme(),
    motion: readMotion(),
    followReduce: readFlag(followKey, true),
    stutter: readFlag(stutterKey, true),
    globe: readFlag(globeKey, true),
  };
}

export function readMotion(): MotionChoice {
  try {
    const stored = localStorage.getItem(motionKey);
    return stored === "full" || stored === "lite" ? stored : "balanced";
  } catch {
    return "balanced";
  }
}

export function readGlobe() {
  return readFlag(globeKey, true);
}

function readFlag(key: string, fallback: boolean) {
  try {
    const stored = localStorage.getItem(key);
    if (stored === "0") return false;
    if (stored === "1") return true;
  } catch {
    // 浏览器禁止存储时用默认
  }
  return fallback;
}

function systemWantsLite() {
  const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  const save = !!(navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData;
  return !!(reduce || save);
}

// 跟随系统时：系统要求减少动态效果就用省电，否则用选中的档位。
export function applyAppearance(next: Appearance) {
  applyTheme(next.theme);
  const tier = next.followReduce && systemWantsLite() ? "lite" : next.motion;
  document.documentElement.dataset.motion = tier;
  window.dispatchEvent(new Event("weir-appearance"));
}

export function saveAppearance(next: Appearance) {
  writeLocal(next);
  void pushAccountAppearance(next);
}

export function writeAppearance(next: Appearance) {
  writeLocal(next);
}

function writeLocal(next: Appearance) {
  try {
    localStorage.setItem("weir-theme", next.theme);
    localStorage.setItem(motionKey, next.motion);
    localStorage.setItem(followKey, next.followReduce ? "1" : "0");
    localStorage.setItem(stutterKey, next.stutter ? "1" : "0");
    localStorage.setItem(globeKey, next.globe ? "1" : "0");
  } catch {
    // 仅本次生效
  }
  applyAppearance(next);
}

export function bootAppearance() {
  applyAppearance(readAppearance());
  const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
  media?.addEventListener("change", () => applyAppearance(readAppearance()));
  const conn = (navigator as Navigator & { connection?: EventTarget }).connection;
  conn?.addEventListener?.("change", () => applyAppearance(readAppearance()));
}

export function stutterPromptDue() {
  try {
    const at = Number(localStorage.getItem(stutterAtKey) || 0);
    return !at || Date.now() - at > 7 * 86400e3;
  } catch {
    return false;
  }
}

export function markStutterPrompted() {
  try {
    localStorage.setItem(stutterAtKey, String(Date.now()));
  } catch {
    // 提示过就行，写不进也不再弹
  }
}
