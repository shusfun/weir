// 超椭圆外形：优先 corner-shape: squircle，不支持时退回 border-radius。
// path 模式用 SVG 路径裁切，描边和投影跟着路径。控制点按 figma-squircle（MIT）的公式自写，不引入该包。
import { useLayoutEffect, useRef, useState, type ComponentProps, type CSSProperties } from "react";
import { cn } from "cn";

function toRad(deg: number) {
  return (deg * Math.PI) / 180;
}

function roundedRect(width: number, height: number, radius: number) {
  const r = Math.min(radius, width / 2, height / 2);
  return `M ${r} 0 H ${width - r} A ${r} ${r} 0 0 1 ${width} ${r} V ${height - r} A ${r} ${r} 0 0 1 ${width - r} ${height} H ${r} A ${r} ${r} 0 0 1 0 ${height - r} V ${r} A ${r} ${r} 0 0 1 ${r} 0 Z`;
}

// 参考 figma-squircle 的角点拆分（直线过渡 + 圆弧 + 对称过渡），路径字符串为自写。
function squirclePath(width: number, height: number, radius: number, smoothing = 0.6) {
  if (width <= 0 || height <= 0) return "";
  const maxR = Math.min(width, height) / 2;
  const r = Math.min(Math.max(radius, 0), maxR);
  if (r <= 0 || smoothing <= 0) return roundedRect(width, height, r);
  const p = Math.min((1 + smoothing) * r, maxR);
  const tight = r <= maxR / 2;
  const diff = tight ? 0 : (r - maxR / 2) / (maxR / 2);
  const angleBeta = 90 * (1 - smoothing * (tight ? 1 : 1 - diff));
  const angleAlpha = 45 * smoothing * (tight ? 1 : 1 - diff);
  const theta = (90 - angleBeta) / 2;
  const p3 = r * Math.tan(toRad(theta / 2));
  const arc = Math.sin(toRad(angleBeta / 2)) * r * Math.SQRT2;
  const c = p3 * Math.cos(toRad(angleAlpha));
  const d = c * Math.tan(toRad(angleAlpha));
  const b = (p - arc - c - d) / 3;
  const a = 2 * b;
  if (![a, b, c, d, p, arc].every((n) => Number.isFinite(n))) return roundedRect(width, height, r);
  const w = width;
  const h = height;
  return [
    `M ${w - p} 0`,
    `c ${a} 0 ${a + b} 0 ${a + b + c} ${d}`,
    `a ${r} ${r} 0 0 1 ${arc} ${arc}`,
    `c ${d} ${c} ${d} ${b + c} ${d} ${a + b + c}`,
    `L ${w} ${h - p}`,
    `c 0 ${a} 0 ${a + b} ${-d} ${a + b + c}`,
    `a ${r} ${r} 0 0 1 ${-arc} ${arc}`,
    `c ${-c} ${d} ${-(b + c)} ${d} ${-(a + b + c)} ${d}`,
    `L ${p} ${h}`,
    `c ${-a} 0 ${-(a + b)} 0 ${-(a + b + c)} ${-d}`,
    `a ${r} ${r} 0 0 1 ${-arc} ${-arc}`,
    `c ${-d} ${-c} ${-d} ${-(b + c)} ${-d} ${-(a + b + c)}`,
    `L 0 ${p}`,
    `c 0 ${-a} 0 ${-(a + b)} ${d} ${-(a + b + c)}`,
    `a ${r} ${r} 0 0 1 ${arc} ${-arc}`,
    `c ${c} ${-d} ${b + c} ${-d} ${a + b + c} ${-d}`,
    "Z",
  ].join(" ");
}

function readPx(el: HTMLElement, name: string) {
  const n = parseFloat(getComputedStyle(el).getPropertyValue(name));
  return Number.isFinite(n) ? n : 21;
}

function Squircle({
  path = false,
  radiusVar = "--weir-radius-surface",
  smoothing = 0.6,
  className,
  style,
  children,
  ...props
}: ComponentProps<"div"> & { path?: boolean; radiusVar?: string; smoothing?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const [shape, setShape] = useState({ d: "", w: 0, h: 0 });
  useLayoutEffect(() => {
    if (!path) return;
    const el = ref.current;
    if (!el) return;
    const draw = () => {
      const rect = el.getBoundingClientRect();
      setShape({ d: squirclePath(rect.width, rect.height, readPx(el, radiusVar), smoothing), w: rect.width, h: rect.height });
    };
    draw();
    const ro = new ResizeObserver(draw);
    ro.observe(el);
    return () => ro.disconnect();
  }, [path, radiusVar, smoothing]);
  return (
    <div
      ref={ref}
      data-slot="squircle"
      data-path={path || undefined}
      className={cn("weir-squircle", className)}
      style={{ "--weir-squircle-radius": `var(${radiusVar})`, ...style } as CSSProperties}
      {...props}
    >
      {path && shape.d && (
        <svg className="weir-squircle-shape" viewBox={`0 0 ${shape.w} ${shape.h}`} aria-hidden="true">
          <path d={shape.d} />
        </svg>
      )}
      <div className="weir-squircle-body" style={path && shape.d ? { clipPath: `path('${shape.d}')` } : undefined}>
        {children}
      </div>
    </div>
  );
}

export { Squircle, squirclePath };
