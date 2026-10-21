import { useEffect, useState } from "react";
import { Button } from "@migate/ui";

export const themeStorageKey = "migate-theme";
export type ThemeMode = "light" | "dark" | "system";

const options: Array<{ value: ThemeMode; label: string }> = [
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
  { value: "system", label: "系统" },
];

export function readTheme(): ThemeMode {
  const stored = localStorage.getItem(themeStorageKey);
  return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
}

export function systemIsDark() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function applyTheme(theme: ThemeMode) {
  const dark = theme === "dark" || (theme === "system" && systemIsDark());
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.dataset.theme = theme;
  root.style.colorScheme = dark ? "dark" : "light";
}

export function ThemeControl() {
  const [theme, setTheme] = useState<ThemeMode>(() => readTheme());
  useEffect(() => {
    applyTheme(theme);
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const sync = () => applyTheme(theme);
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, [theme]);
  return (
    <div className="theme-control" role="group" aria-label="界面主题">
      {options.map((option) => (
        <Button
          key={option.value}
          type="button"
          size="sm"
          variant={theme === option.value ? "default" : "outline"}
          aria-pressed={theme === option.value}
          onClick={() => {
            localStorage.setItem(themeStorageKey, option.value);
            setTheme(option.value);
          }}
        >
          {option.label}
        </Button>
      ))}
    </div>
  );
}
