import type { CSSProperties } from "react";

// 四段三次曲线共用拓扑，轮廓连续变成太阳、月亮与显示器。
const outlines = {
  light: "M 18 12 C 18 15.3 15.3 18 12 18 C 8.7 18 6 15.3 6 12 C 6 8.7 8.7 6 12 6 C 15.3 6 18 8.7 18 12 Z",
  dark: "M 20 13 C 19 18 15 21 10 19 C 5 17 3 12 6 7 C 7 5 9 4 12 4 C 8 10 14 16 20 13 Z",
  system: "M 21 5 C 21 5 21 17 21 17 C 21 17 3 17 3 17 C 3 17 3 5 3 5 C 3 5 21 5 21 5 Z",
};
const rays = ["M12 1 L12 3", "M12 21 L12 23", "M1 12 L3 12", "M21 12 L23 12", "M4 4 L5 5", "M19 19 L20 20", "M4 20 L5 19", "M19 5 L20 4"];
export function ThemeIcon({ mode }: { mode: keyof typeof outlines }) {
  const pathStyle = (d: string) => ({ d: `path('${d}')` }) as CSSProperties;
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden className="weir-theme-icon">
    <path d={outlines[mode]} style={pathStyle(outlines[mode])} />
    {rays.map((d, i) => { const shape = mode === "light" ? d : mode === "system" && i === 1 ? "M12 17 L12 21" : mode === "system" && i === 2 ? "M8 21 L16 21" : "M12 12 L12 12"; return <path key={i} d={shape} style={pathStyle(shape)} />; })}
  </svg>;
}
