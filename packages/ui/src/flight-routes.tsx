// 航线（地球背景、登录页共用）：按真实航班飞行图画——两点之间走球面大圆，按距离抬高弧顶；
// 背面的一段淡出；已飞过的一段是由暗到亮的实线，未飞的一段是细虚线；起降点是机场圆点加光晕；
// 小飞机沿切线朝向飞行。用 SVG 画，三个动效档位都有航线：
//   完整：飞机连续飞、尾迹渐隐（rAF）；均衡：降到约 30fps；省电：不跑 rAF，每 2 秒挪一格（CSS 过渡补间）。
// 页面隐藏时全部暂停。坐标投影与背景地球一致（倾角 13°），由调用方传入同一个中心经度与旋转角。
import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { MotionTier } from "./motion";

const RAD = Math.PI / 180;
const TILT = 13 * RAD;

type Point = { lat: number; lon: number };
type Route = { id: string; from: Point; to: Point; tone?: "accent" | "warn" | "ok" };

function unit(p: Point): [number, number, number] {
  const f = p.lat * RAD;
  const l = p.lon * RAD;
  return [Math.cos(f) * Math.cos(l), Math.cos(f) * Math.sin(l), Math.sin(f)];
}

function project(v: [number, number, number], lift: number, lon0: number, cx: number, cy: number, r: number) {
  const lat = Math.asin(Math.max(-1, Math.min(1, v[2])));
  const lon = Math.atan2(v[1], v[0]);
  const d = lon - lon0;
  const cosf = Math.cos(lat);
  const sinf = Math.sin(lat);
  const cosd = Math.cos(d);
  const R = r * lift;
  return {
    x: cx + R * cosf * Math.sin(d),
    y: cy - R * (Math.cos(TILT) * sinf - Math.sin(TILT) * cosf * cosd),
    z: Math.sin(TILT) * sinf + Math.cos(TILT) * cosf * cosd,
  };
}

// 大圆插值：按弧长决定采样数，弧顶抬高与距离成正比（越远飞得越高），近距离也保留一点弧度
function arcPoints(route: Route, lon0: number, cx: number, cy: number, r: number) {
  const a = unit(route.from);
  const b = unit(route.to);
  const angle = Math.acos(Math.max(-1, Math.min(1, a[0] * b[0] + a[1] * b[1] + a[2] * b[2])));
  if (angle < 1e-3) return [];
  const n = Math.max(25, Math.round(angle * 61));
  const peak = 0.05 + 0.23 * Math.min(1, angle / Math.PI);
  const out: { x: number; y: number; z: number }[] = [];
  for (let i = 0; i <= n; i++) {
    const t = i / n;
    const k1 = Math.sin((1 - t) * angle) / Math.sin(angle);
    const k2 = Math.sin(t * angle) / Math.sin(angle);
    const v: [number, number, number] = [k1 * a[0] + k2 * b[0], k1 * a[1] + k2 * b[1], k1 * a[2] + k2 * b[2]];
    out.push(project(v, 1 + peak * Math.sin(Math.PI * t), lon0, cx, cy, r));
  }
  return out;
}

function pathOf(pts: { x: number; y: number }[]) {
  return pts.map((p, i) => `${i ? "L" : "M"}${p.x.toFixed(1)} ${p.y.toFixed(1)}`).join(" ");
}

// 每条航线的进度：完整与均衡由 rAF 推进（均衡隔帧），省电每 2 秒一跳；页面隐藏时停
function useProgress(count: number, tier: MotionTier) {
  const [t, setT] = useState(0);
  useEffect(() => {
    if (!count) return;
    let raf = 0;
    let timer = 0;
    let last = 0;
    const start = performance.now();
    const period = 9000;
    const loop = (now: number) => {
      if (document.visibilityState === "hidden") {
        raf = requestAnimationFrame(loop);
        return;
      }
      if (tier === "full" || now - last >= 33) {
        last = now;
        setT(((now - start) % period) / period);
      }
      raf = requestAnimationFrame(loop);
    };
    if (tier === "lite") {
      const tick = () => {
        if (document.visibilityState !== "hidden") setT((v) => (v + 0.2) % 1);
      };
      timer = window.setInterval(tick, 2000);
    } else {
      raf = requestAnimationFrame(loop);
    }
    return () => {
      cancelAnimationFrame(raf);
      window.clearInterval(timer);
    };
  }, [count, tier]);
  return t;
}

function FlightRoutes({
  routes,
  size,
  centerLon,
  rotation = 0,
  tier,
  className,
}: {
  routes: readonly Route[];
  /** 画布边长（与背景地球同尺寸，正方形） */
  size: number;
  centerLon: number;
  rotation?: number;
  tier: MotionTier;
  className?: string;
}) {
  const gid = useId().replace(/:/g, "");
  const progress = useProgress(routes.length, tier);
  const cx = size / 2;
  const cy = size / 2;
  const r = size * 0.46;
  const lon0 = (centerLon + rotation) * RAD;
  const arcs = useMemo(() => routes.map((route) => ({ route, pts: arcPoints(route, lon0, cx, cy, r) })), [routes, lon0, cx, cy, r]);
  const lite = tier === "lite";
  return (
    <svg className={className} viewBox={`0 0 ${size} ${size}`} width={size} height={size} aria-hidden data-tier={tier}>
      <defs>
        <linearGradient id={`${gid}-trail`} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="var(--weir-dot-line)" stopOpacity="0" />
          <stop offset="1" stopColor="var(--weir-dot-line)" stopOpacity="1" />
        </linearGradient>
        <radialGradient id={`${gid}-halo`}>
          <stop offset="0" stopColor="var(--weir-dot-line)" stopOpacity="0.55" />
          <stop offset="1" stopColor="var(--weir-dot-line)" stopOpacity="0" />
        </radialGradient>
      </defs>
      {arcs.map(({ route, pts }, i) => {
        if (pts.length < 2) return null;
        const color = route.tone === "warn" ? "var(--weir-warn)" : route.tone === "ok" ? "var(--weir-ok)" : "var(--weir-dot-line)";
        // 相位错开，交汇处不重叠成一团
        const phase = (progress + i * 0.37) % 1;
        const head = Math.max(1, Math.round(phase * (pts.length - 1)));
        const tail = Math.max(0, head - Math.round(pts.length * 0.35));
        const front = pts.filter((p) => p.z > -0.05);
        const flown = pts.slice(tail, head + 1).filter((p) => p.z > -0.05);
        const plane = pts[head];
        const prev = pts[Math.max(0, head - 1)];
        const heading = (Math.atan2(plane.y - prev.y, plane.x - prev.x) * 180) / Math.PI;
        const ends = [pts[0], pts[pts.length - 1]];
        return (
          <g key={route.id} className="weir-flight">
            {/* 背面淡出：只画朝向我们的部分，整条航线先画一条细虚线作为航路 */}
            <path d={pathOf(front)} fill="none" stroke={color} strokeOpacity="0.45" strokeWidth="1" strokeDasharray="2 5" strokeLinecap="round" />
            {flown.length > 1 && <path d={pathOf(flown)} fill="none" stroke={route.tone ? color : `url(#${gid}-trail)`} strokeWidth="1.5" strokeLinecap="round" style={{ filter: "var(--weir-glow)" }} />}
            {ends.map((p, k) => p.z > 0 && (
              <g key={k} transform={`translate(${p.x.toFixed(1)} ${p.y.toFixed(1)})`} opacity={Math.min(1, p.z + 0.21)}>
                <circle r="9" fill={`url(#${gid}-halo)`} />
                <circle r="3" fill="var(--weir-base)" stroke={color} strokeWidth="1.5" />
              </g>
            ))}
            {plane.z > 0 && (
              <g className="weir-flight-plane" transform={`translate(${plane.x.toFixed(1)} ${plane.y.toFixed(1)}) rotate(${heading.toFixed(1)}) scale(1.5)`} style={lite ? { transition: "transform 2s linear" } : undefined}>
                {/* 机身、主翼、尾翼：朝 +x 方向，长约 11px */}
                <path d="M6 0 L-1 -1.1 L-2 -5 L-3.4 -5 L-2.6 -1.2 L-5 -1 L-6 -2.6 L-6.8 -2.6 L-6.3 0 L-6.8 2.6 L-6 2.6 L-5 1 L-2.6 1.2 L-3.4 5 L-2 5 L-1 1.1 Z" fill={color} />
              </g>
            )}
          </g>
        );
      })}
    </svg>
  );
}

export { FlightRoutes };
export type { Route as FlightRoute };
