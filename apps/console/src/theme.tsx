import { useEffect, useState, useSyncExternalStore } from "react";
import { Button } from "@migate/ui";
import { Sun, Moon, Monitor } from "lucide-react";

export const themeStorageKey = "migate-theme";
export type ThemeMode = "light" | "dark" | "system";

// 单个图标按钮循环切换：浅色 → 深色 → 跟随系统
const modes = {
  light: { icon: Sun, label: "浅色", next: "dark" },
  dark: { icon: Moon, label: "深色", next: "system" },
  system: { icon: Monitor, label: "跟随系统", next: "light" },
} as const;

export function readTheme(): ThemeMode {
  try {
    const stored = localStorage.getItem(themeStorageKey);
    return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
  } catch {
    return "system";
  }
}

export function systemIsDark() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

// 与 tokens.css 的 --mg-base 保持一致，供浏览器地址栏 / 状态栏着色
const themeColors = { light: "#f2f2f7", dark: "#1c1c1e" } as const;
// 当前生效模式；浏览器禁止存储时仍以本次选择为准
let activeTheme: ThemeMode = "system";

export function applyTheme(theme: ThemeMode) {
  activeTheme = theme;
  const dark = theme === "dark" || (theme === "system" && systemIsDark());
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.dataset.theme = theme;
  root.style.colorScheme = dark ? "dark" : "light";
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark ? themeColors.dark : themeColors.light);
}

// 全局监听系统明暗变化：登录页、公开页等没有主题按钮的页面也能实时跟随
export function watchSystemTheme() {
  applyTheme(readTheme());
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (activeTheme === "system") applyTheme("system");
  });
}

// 订阅 <html> 上的 dark class，供需要显式传入明暗模式的第三方组件（React Flow 等）使用
function subscribeDark(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
  return () => observer.disconnect();
}
const readDark = () => document.documentElement.classList.contains("dark");

export function useIsDark() {
  return useSyncExternalStore(subscribeDark, readDark);
}

// 读取当前主题下 CSS 令牌的实际值；用于不能直接写 var() 的场景（如 SVG marker 的 id）
export function tokenValue(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export function ThemeControl({ withLabel = false }: { withLabel?: boolean }) {
  const [theme, setTheme] = useState<ThemeMode>(() => activeTheme);
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);
  const current = modes[theme];
  const next = modes[current.next];
  const Icon = current.icon;
  return (
    <Button
      type="button"
      variant={withLabel ? "outline" : "ghost"}
      size={withLabel ? "default" : "icon-sm"}
      aria-label={`界面主题：${current.label}，点击切换为${next.label}`}
      title={`主题：${current.label}（点击切换为${next.label}）`}
      onClick={() => {
        try {
          localStorage.setItem(themeStorageKey, current.next);
        } catch {
          // 浏览器禁止存储时仅本次生效
        }
        setTheme(current.next);
      }}
    >
      <Icon />
      {withLabel && <span>主题：{current.label}</span>}
    </Button>
  );
}
