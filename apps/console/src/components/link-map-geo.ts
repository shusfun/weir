// 链路图的几何：平铺投影（Equal Earth，中心经度按点的分布选，让连线尽量不被左右边界切断）、
// 国家轮廓（Natural Earth 110m，打包进程序，不依赖地图瓦片）、弧线与命中检测。
import { geoEqualEarth, geoPath, type GeoProjection } from "d3-geo";
import { feature, mesh } from "topojson-client";
import type { GeometryCollection, Topology } from "topojson-specification";
import world from "world-atlas/countries-110m.json";
import type { GeoPlace } from "../lib/linkmap";

const topo = world as unknown as Topology<{ countries: GeometryCollection }>;
const countries = feature(topo, topo.objects.countries);
const borders = mesh(topo, topo.objects.countries, (a, b) => a !== b);

export interface Pt {
  x: number;
  y: number;
}

// centerLon：找到点之间最大的经度空档，把地图左右边界放在空档中间。
export function centerLon(places: GeoPlace[]): number {
  const lons = places.filter((p) => p.known && p.lon !== undefined).map((p) => p.lon as number).sort((a, b) => a - b);
  if (lons.length === 0) return 105;
  let gap = 360 - lons[lons.length - 1] + lons[0];
  let edge = lons[lons.length - 1] + gap / 2;
  for (let i = 1; i < lons.length; i++) {
    const g = lons[i] - lons[i - 1];
    if (g > gap) {
      gap = g;
      edge = lons[i - 1] + g / 2;
    }
  }
  let c = edge + 180;
  while (c > 180) c -= 360;
  return c;
}

export interface World {
  projection: GeoProjection;
  land: Path2D;
  border: Path2D;
  sphere: Path2D;
  project: (p: GeoPlace) => Pt | null;
}

// buildWorld 按画布大小与中心经度生成投影与轮廓（只在尺寸或中心变化时重建，平移缩放用画布变换）。
export function buildWorld(width: number, height: number, lon: number): World {
  const projection = geoEqualEarth().rotate([-lon, 0]).fitExtent([[8, 8], [width - 8, height - 8]], { type: "Sphere" });
  const path = geoPath(projection);
  return {
    projection,
    land: new Path2D(path(countries) ?? ""),
    border: new Path2D(path(borders) ?? ""),
    sphere: new Path2D(path({ type: "Sphere" }) ?? ""),
    project: (p) => {
      if (!p.known || p.lat === undefined || p.lon === undefined) return null;
      const xy = projection([p.lon, p.lat]);
      return xy ? { x: xy[0], y: xy[1] } : null;
    },
  };
}

// arcControl：两点之间往上弯的二次贝塞尔控制点；两点重合时画一个小环。
export function arcControl(a: Pt, b: Pt): Pt {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const d = Math.hypot(dx, dy);
  if (d < 1) return { x: a.x + 18, y: a.y - 24 };
  const bend = Math.min(0.35 * d, 140);
  return { x: (a.x + b.x) / 2 + (dy / d) * bend * (dx >= 0 ? -1 : 1), y: (a.y + b.y) / 2 - Math.abs(dx / d) * bend };
}

export function onArc(a: Pt, c: Pt, b: Pt, t: number): Pt {
  const u = 1 - t;
  return { x: u * u * a.x + 2 * u * t * c.x + t * t * b.x, y: u * u * a.y + 2 * u * t * c.y + t * t * b.y };
}

// nearArc：点到弧线的距离（采样 24 段近似）。
export function nearArc(a: Pt, c: Pt, b: Pt, p: Pt): number {
  let best = Infinity;
  for (let i = 0; i <= 24; i++) {
    const q = onArc(a, c, b, i / 24);
    best = Math.min(best, Math.hypot(q.x - p.x, q.y - p.y));
  }
  return best;
}

// readColors 读当前主题的颜色（与控制台的设计变量一致）。
export function readColors() {
  const cs = getComputedStyle(document.documentElement);
  const v = (name: string, fallback: string) => cs.getPropertyValue(name).trim() || fallback;
  return {
    sea: v("--weir-well-deep", "#e9e9ef"),
    land: v("--weir-panel", "#ffffff"),
    border: v("--weir-line", "#d6d6de"),
    ink: v("--weir-ink", "#111"),
    ink3: v("--weir-ink-3", "#888"),
    accent: v("--weir-accent", "#0a84ff"),
    ok: v("--weir-ok", "#30d158"),
    warn: v("--weir-warn", "#ff9f0a"),
    bad: v("--weir-bad", "#ff453a"),
    info: v("--weir-info", "#64d2ff"),
    off: v("--weir-off", "#8e8e93"),
    font: v("--weir-font", "system-ui, sans-serif"),
  };
}
export type Colors = ReturnType<typeof readColors>;
