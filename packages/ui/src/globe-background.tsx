// 右下角点阵地球。陆地只用预先算好的 Natural Earth 点（land-dots），不随机生成。
// nodes / links 由调用方按角色过滤后再传入；登录页不要传真实节点。
// pointer-events: none。手机不渲染。离开视口、页面隐藏或省电档时地球不再转；航线三档都画（见 flight-routes.tsx）。
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { cn } from "cn";
import { useMotionTier } from "./motion-tier";
import type { MotionTier } from "./motion";
import { FlightRoutes } from "./flight-routes";

type GlobeTone = "ok" | "warn" | "bad" | "info" | "off" | "unknown";

type GlobeNode = {
  id: string;
  lat: number;
  lon: number;
  tone?: GlobeTone | null;
  label?: string;
};

type GlobeLink = {
  from: string;
  to: string;
  tone?: Exclude<GlobeTone, "off" | "unknown"> | null;
};

const toneVar: Record<GlobeTone, string> = {
  ok: "--weir-ok",
  warn: "--weir-warn",
  bad: "--weir-bad",
  info: "--weir-accent",
  off: "--weir-off",
  unknown: "--weir-ink-3",
};

const RAD = Math.PI / 180;
const phoneQuery = "(max-width: 767.98px)";

let landPromise: Promise<ReadonlyArray<readonly [number, number]>> | null = null;

function loadLand() {
  landPromise ??= import("./land-dots").then((mod) => mod.landDots);
  return landPromise;
}

function usePhone() {
  return useSyncExternalStore(
    (onChange) => {
      const mq = window.matchMedia(phoneQuery);
      mq.addEventListener("change", onChange);
      return () => mq.removeEventListener("change", onChange);
    },
    () => window.matchMedia(phoneQuery).matches,
    () => false,
  );
}

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function readMs(name: string, fallback: number) {
  const n = parseFloat(cssVar(name));
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function readFloat(name: string, fallback: number) {
  const n = parseFloat(cssVar(name));
  return Number.isFinite(n) && n >= 0 ? n : fallback;
}

function project(lat: number, lon: number, lon0: number, tilt: number, cx: number, cy: number, r: number) {
  const f = lat * RAD;
  const d = lon * RAD - lon0;
  const cosf = Math.cos(f);
  const sinf = Math.sin(f);
  const cosd = Math.cos(d);
  return {
    x: cx + r * cosf * Math.sin(d),
    y: cy - r * (Math.cos(tilt) * sinf - Math.sin(tilt) * cosf * cosd),
    z: Math.sin(tilt) * sinf + Math.cos(tilt) * cosf * cosd,
  };
}

function sunVector(now: Date): [number, number, number] {
  const doy = Math.floor((now.getTime() - Date.UTC(now.getUTCFullYear(), 0, 0)) / 86400000);
  const decl = 23.44 * Math.sin((2 * Math.PI * (284 + doy)) / 365) * RAD;
  const hours = now.getUTCHours() + now.getUTCMinutes() / 60;
  const sl = (12 - hours) * 15 * RAD;
  return [Math.cos(decl) * Math.cos(sl), Math.cos(decl) * Math.sin(sl), Math.sin(decl)];
}

function illumination(lat: number, lon: number, sun: [number, number, number]) {
  const f = lat * RAD;
  const l = lon * RAD;
  const v = Math.cos(f) * Math.cos(l) * sun[0] + Math.cos(f) * Math.sin(l) * sun[1] + Math.sin(f) * sun[2];
  return Math.max(0, Math.min(1, (v + 0.13) / 0.25));
}

function finiteNode(node: GlobeNode) {
  return Number.isFinite(node.lat) && Number.isFinite(node.lon);
}

function autoFocus(nodes: readonly GlobeNode[], explicitLon?: number): { lon: number; tiltDeg: number } {
  if (explicitLon !== undefined) return { lon: explicitLon, tiltDeg: 13 };
  const valid = nodes.filter(finiteNode);
  if (!valid.length) return { lon: 151, tiltDeg: -7 };
  let sx = 0;
  let sy = 0;
  let sumLat = 0;
  for (const n of valid) {
    sx += Math.cos(n.lon * RAD);
    sy += Math.sin(n.lon * RAD);
    sumLat += n.lat;
  }
  const meanLon = Math.atan2(sy, sx) / RAD;
  const meanLat = sumLat / valid.length;
  // 球心在视口右下角，只有左上象限可见：中心经度向右偏移 33°、倾角随平均纬度调整，让真实节点落在可见扇区
  const tiltDeg = Math.max(-13, Math.min(13, meanLat - 23));
  return { lon: meanLon + 33, tiltDeg };
}

function paint(
  canvas: HTMLCanvasElement,
  land: ReadonlyArray<readonly [number, number]>,
  rot: number,
  nodes: readonly GlobeNode[],
  centerLon: number,
  tiltDeg: number,
) {
  const cssW = canvas.clientWidth;
  const cssH = canvas.clientHeight;
  if (cssW < 2 || cssH < 2) return;
  const dpr = Math.min(window.devicePixelRatio || 1, 1.25);
  const bw = Math.round(cssW * dpr);
  const bh = Math.round(cssH * dpr);
  if (canvas.width !== bw || canvas.height !== bh) {
    canvas.width = bw;
    canvas.height = bh;
  }
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssW, cssH);
  const cx = cssW / 2;
  const cy = cssH / 2;
  const radius = Math.min(cssW, cssH) * 0.46;
  const lon0 = (centerLon + rot) * RAD;
  const tilt = tiltDeg * RAD;
  const baseAlpha = readFloat("--weir-globe", 0.45);
  const panel = cssVar("--weir-panel");
  const line = cssVar("--weir-line");
  const accentSoft = cssVar("--weir-accent-soft");
  const dot = cssVar("--weir-dot");
  const dotLine = cssVar("--weir-dot-line");
  const ink2 = cssVar("--weir-ink-2");
  const ink3 = cssVar("--weir-ink-3");
  const font = cssVar("--weir-font") || "system-ui, sans-serif";
  if (accentSoft) {
    ctx.globalAlpha = 0.85;
    ctx.beginPath();
    ctx.arc(cx, cy, radius + 4, 0, Math.PI * 2);
    ctx.strokeStyle = accentSoft;
    ctx.lineWidth = 7;
    ctx.stroke();
  }
  ctx.globalAlpha = baseAlpha;
  ctx.beginPath();
  ctx.arc(cx, cy, radius, 0, Math.PI * 2);
  if (panel) {
    ctx.fillStyle = panel;
    ctx.fill();
  }
  if (line) {
    ctx.globalAlpha = Math.min(1, baseAlpha * 1.45);
    ctx.strokeStyle = line;
    ctx.lineWidth = 1;
    ctx.stroke();
  }
  ctx.save();
  ctx.beginPath();
  ctx.arc(cx, cy, radius, 0, Math.PI * 2);
  ctx.clip();
  const sun = sunVector(new Date());
  if (dot) {
    for (let lat = -81; lat <= 81; lat += 9) {
      const n = Math.max(1, Math.round(41 * Math.cos(lat * RAD)));
      for (let i = 0; i < n; i++) {
        const lon = -180 + (i * 360) / n;
        const p = project(lat, lon, lon0, tilt, cx, cy, radius);
        if (p.z <= 0.03) continue;
        ctx.globalAlpha = baseAlpha * p.z * (0.25 + 0.39 * illumination(lat, lon, sun));
        ctx.fillStyle = dot;
        ctx.beginPath();
        ctx.arc(p.x, p.y, 0.6 + 0.5 * p.z, 0, Math.PI * 2);
        ctx.fill();
      }
    }
  }
  if (dotLine || ink3) {
    for (const [lat, lon] of land) {
      const p = project(lat, lon, lon0, tilt, cx, cy, radius);
      if (p.z <= 0.02) continue;
      const lit = illumination(lat, lon, sun);
      ctx.globalAlpha = baseAlpha * p.z * (0.37 + 0.79 * lit);
      ctx.fillStyle = lit > 0.5 ? dotLine || ink3 : ink3 || dotLine;
      ctx.beginPath();
      ctx.arc(p.x, p.y, 1.0 + 1.15 * p.z, 0, Math.PI * 2);
      ctx.fill();
    }
  }
  ctx.restore();
  ctx.globalAlpha = 1;
  const labelBoxes: { l: number; r: number; t: number; b: number }[] = [];
  ctx.font = `500 11px ${font}`;
  ctx.textBaseline = "middle";
  // 航线由 FlightRoutes 用 SVG 叠在上面画（各档位都显示），这里画前景高亮节点与微标签
  for (const node of nodes) {
    if (!finiteNode(node)) continue;
    const p = project(node.lat, node.lon, lon0, tilt, cx, cy, radius);
    if (p.z <= 0.05) continue;
    const tone = node.tone ?? "unknown";
    const color = cssVar(toneVar[tone]);
    if (!color) continue;
    if (tone === "unknown") {
      ctx.globalAlpha = 0.85 * p.z;
      ctx.strokeStyle = color;
      ctx.lineWidth = 1.25;
      ctx.setLineDash([3, 3]);
      ctx.beginPath();
      ctx.arc(p.x, p.y, 5, 0, Math.PI * 2);
      ctx.stroke();
      ctx.setLineDash([]);
      continue;
    }
    const alert = tone === "bad" || tone === "warn";
    if (tone !== "off") {
      ctx.globalAlpha = p.z * (alert ? 0.31 : 0.21);
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.arc(p.x, p.y, alert ? 13 : 9, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = Math.min(1, 0.25 + 0.85 * p.z);
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(p.x, p.y, tone === "off" ? 3.5 : 4.5, 0, Math.PI * 2);
    ctx.fill();
    if (node.label && p.x < cx - 15 && p.y < cy - 15 && p.x > 65 && p.y > 21) {
      const text = node.label.length > 22 ? node.label.slice(0, 21) + "…" : node.label;
      const w = ctx.measureText(text).width;
      const box = { l: p.x - 11 - w, r: p.x - 9, t: p.y - 8, b: p.y + 8 };
      if (box.l > 12 && !labelBoxes.some((b) => !(box.r < b.l || box.l > b.r || box.b < b.t || box.t > b.b))) {
        labelBoxes.push(box);
        ctx.globalAlpha = Math.min(0.85, 0.35 + 0.55 * p.z);
        ctx.fillStyle = ink2 || color;
        ctx.textAlign = "right";
        ctx.fillText(text, p.x - 10, p.y);
      }
    }
  }
  ctx.globalAlpha = 1;
}

function unit(lat: number, lon: number): [number, number, number] {
  const f = lat * RAD;
  const l = lon * RAD;
  return [Math.cos(f) * Math.cos(l), Math.cos(f) * Math.sin(l), Math.sin(f)];
}

// 全球公开机房（登录页的随机航线只在这些点之间飞，不代表任何真实节点或流量）
const DATACENTERS: readonly GlobeNode[] = [
  [35.68, 139.69], [22.32, 114.17], [1.35, 103.82], [37.57, 126.98], [50.11, 8.68], [52.37, 4.9], [51.5, -0.12],
  [39.04, -77.49], [34.05, -118.24], [37.37, -121.92], [-23.55, -46.63], [-33.87, 151.21], [19.08, 72.88],
].map(([lat, lon], i) => ({ id: `dc${i}`, lat, lon, tone: "off" as const }));

// 随机挑 9 对机场；每 13 秒换一对（换的是数据，不额外重绘地球）
function useDemoLinks(on: boolean) {
  const pick = () => {
    const a = Math.floor(Math.random() * DATACENTERS.length);
    let b = a;
    while (b === a) b = Math.floor(Math.random() * DATACENTERS.length);
    return { from: DATACENTERS[a].id, to: DATACENTERS[b].id };
  };
  const [list, setList] = useState<GlobeLink[]>(() => (on ? Array.from({ length: 9 }, pick) : []));
  useEffect(() => {
    if (!on) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState === "hidden") return;
      setList((cur) => cur.map((l, i) => (i === Math.floor(Math.random() * cur.length) ? pick() : l)));
    }, 13000);
    return () => window.clearInterval(timer);
  }, [on]);
  return list;
}

function GlobeBackground({
  nodes: givenNodes = [],
  links: givenLinks = [],
  centerLon,
  demo = false,
  className,
}: {
  nodes?: readonly GlobeNode[];
  links?: readonly GlobeLink[];
  centerLon?: number;
  /** 登录页：不传真实节点，画公开机房之间的随机航线 */
  demo?: boolean;
  className?: string;
}) {
  const demoLinks = useDemoLinks(demo);
  const nodes = demo ? DATACENTERS : givenNodes;
  const links = demo ? demoLinks : givenLinks;
  const focus = useMemo(() => (demo && centerLon === undefined ? { lon: 151, tiltDeg: -5 } : autoFocus(nodes, centerLon)), [demo, nodes, centerLon]);
  const tier = useMotionTier();
  const phone = usePhone();
  const hostRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [near, setNear] = useState(false);
  const [side, setSide] = useState(0);
  const [rotation, setRotation] = useState(0);
  const rot = useRef(0);
  const live = useRef({ nodes, links, tier, lon: focus.lon, tiltDeg: focus.tiltDeg });
  live.current = { nodes, links, tier, lon: focus.lon, tiltDeg: focus.tiltDeg };

  useEffect(() => {
    const el = hostRef.current;
    if (!el || phone) return;
    let leave = 0;
    const io = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) {
        window.clearTimeout(leave);
        setNear(true);
      } else {
        leave = window.setTimeout(() => setNear(false), 700);
      }
    }, { rootMargin: "320px" });
    io.observe(el);
    return () => {
      io.disconnect();
      window.clearTimeout(leave);
    };
  }, [phone]);

  useEffect(() => {
    if (phone || !near) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    let alive = true;
    let timer = 0;
    let land: ReadonlyArray<readonly [number, number]> = [];
    const draw = () => {
      if (!alive || document.visibilityState === "hidden") return;
      const cur = live.current;
      paint(canvas, land, rot.current, cur.nodes, cur.lon, cur.tiltDeg);
      setRotation(rot.current);
    };
    const stop = () => {
      window.clearInterval(timer);
      timer = 0;
    };
    const arm = (mode: MotionTier) => {
      stop();
      if (document.visibilityState === "hidden") return;
      draw();
      if (mode === "lite") return;
      const ms = mode === "full" ? readMs("--weir-fast", 150) : 60000;
      timer = window.setInterval(() => {
        if (!alive || document.visibilityState === "hidden") return;
        if (live.current.tier === "lite") {
          stop();
          return;
        }
        if (live.current.tier === "full") rot.current = (rot.current + 0.15) % 360;
        draw();
      }, ms);
    };
    loadLand().then((pts) => {
      if (!alive) return;
      land = pts;
      draw();
    });
    const onVis = () => (document.visibilityState === "hidden" ? stop() : arm(live.current.tier));
    document.addEventListener("visibilitychange", onVis);
    const ro = new ResizeObserver(() => {
      setSide(canvas.clientWidth);
      draw();
    });
    ro.observe(canvas);
    const mo = new MutationObserver(() => draw());
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    arm(tier);
    return () => {
      alive = false;
      stop();
      document.removeEventListener("visibilitychange", onVis);
      ro.disconnect();
      mo.disconnect();
      canvas.width = 0;
      canvas.height = 0;
    };
  }, [phone, near, tier, nodes, links, focus.lon, focus.tiltDeg]);

  const routes = useMemo(() => {
    const byId = new Map(nodes.filter(finiteNode).map((node) => [node.id, node]));
    return links.flatMap((link) => {
      const a = byId.get(link.from);
      const b = byId.get(link.to);
      return a && b && a.id !== b.id ? [{ id: `${link.from}-${link.to}`, from: a, to: b, tone: link.tone === "warn" || link.tone === "bad" ? "warn" as const : undefined }] : [];
    });
  }, [nodes, links]);
  if (phone) return null;
  return (
    <div ref={hostRef} data-slot="globe-background" className={cn("weir-globe", className)} aria-hidden="true">
      {near && <canvas ref={canvasRef} />}
      {near && side > 0 && routes.length > 0 && <FlightRoutes className="weir-globe-routes" routes={routes} size={side} centerLon={focus.lon} tiltDeg={focus.tiltDeg} rotation={rotation} tier={tier} />}
    </div>
  );
}

export { GlobeBackground };
export type { GlobeNode, GlobeLink, GlobeTone };
