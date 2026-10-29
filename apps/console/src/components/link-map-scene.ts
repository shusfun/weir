// 把快照与实时数据换成一帧要画的东西：点的位置（同一位置合并）、连线（入口 VPS → 出口）、直连光环、新数据脉冲。
import type { LinkMap, LiveStore, MapExit } from "../lib/linkmap";
import type { Scene, ScenePoint, SceneArc, View } from "./link-map-draw";
import type { Colors, Pt, World } from "./link-map-geo";

export interface Focus {
  user?: number; // 只看某个用户的路径
  gate?: number; // 只看某台 VPS
}

// exitEnd：出口在地图上的位置；另一台 VPS 的节点落在那台 VPS 上，直连落在入口自己身上（画光环，不画线）。
function exitEnd(m: LinkMap, w: World, x: MapExit): Pt | null {
  if (x.kind === "node" && x.to_gate) {
    const g = m.gates.find((y) => y.id === x.to_gate);
    return g ? w.project(g.place) : null;
  }
  return w.project(x.place);
}

export function buildScene(m: LinkMap, store: LiveStore, w: World, colors: Colors, v: View, focus: Focus, sel: string | null, hover: string | null, motion: boolean, now: number): Scene {
  const wall = Date.now();
  const relevant = new Set<string>();
  const gateOk = new Set<number>();
  const focused = focus.user !== undefined || focus.gate !== undefined;
  for (const p of m.paths) {
    if ((focus.user === undefined || p.user_id === focus.user) && (focus.gate === undefined || p.gate_id === focus.gate)) {
      relevant.add(p.exit_id);
      gateOk.add(p.gate_id);
    }
  }
  if (focus.gate !== undefined) gateOk.add(focus.gate);
  const points: ScenePoint[] = [];
  for (const g of m.gates) {
    const at = w.project(g.place);
    if (!at) continue;
    const tone = !g.online ? "off" : g.entry && store.connected && store.stale(g.id, wall) && store.gateAt.has(g.id) ? "warn" : "ok";
    points.push({ ...at, id: "g:" + g.id, kind: "gate", label: g.name, tone, cluster: 1, hidden: false, dim: focused && !gateOk.has(g.id) && !m.exits.some((x) => relevant.has(x.id) && x.to_gate === g.id) });
  }
  const arcs: SceneArc[] = [];
  const halos: Scene["halos"] = [];
  for (const x of m.exits) {
    const from = m.gates.find((g) => g.id === x.from_gate);
    const a = from ? w.project(from.place) : null;
    const rate = store.smooth(store.exits.get(x.id), wall);
    const stale = store.stale(x.from_gate, wall);
    const dim = focused && !relevant.has(x.id);
    if (x.kind === "direct") {
      if (a) halos.push({ at: a, rate, stale, dim });
      continue;
    }
    const b = exitEnd(m, w, x);
    if (a && b) arcs.push({ id: x.id, from: a, to: b, rate, stale, dim });
    if (b && x.kind !== "node") {
      points.push({ ...b, id: "x:" + x.id, kind: "exit", label: x.label, tone: x.kind === "block" ? "off" : x.owner === "self" ? "info" : "accent", cluster: 1, hidden: false, dim, owner: x.owner });
    }
  }
  // 屏幕上 14 像素内的点合并成一个，放大后自然分开。
  for (let i = 0; i < points.length; i++) {
    const p = points[i];
    if (p.hidden) continue;
    for (let j = i + 1; j < points.length; j++) {
      const q = points[j];
      if (!q.hidden && Math.hypot((p.x - q.x) * v.k, (p.y - q.y) * v.k) < 14) {
        q.hidden = true;
        p.cluster++;
      }
    }
  }
  const pulses: Scene["pulses"] = [];
  if (motion) {
    for (const pu of store.pulses) {
      const age = (now - pu.at) / 1200;
      const g = m.gates.find((y) => y.id === pu.gate);
      const at = g ? w.project(g.place) : null;
      if (at && age >= 0 && age < 1) pulses.push({ at, age });
    }
  }
  return { world: w, colors, points, arcs, halos, pulses, selected: sel, hover, motion };
}

// hitTest：屏幕坐标下最近的点（12 像素内）或连线（8 像素内）。
export function hitTest(s: Scene, v: View, sx: number, sy: number, nearArc: (a: Pt, c: Pt, b: Pt, p: Pt) => number, ctrl: (a: Pt, b: Pt) => Pt): string | null {
  const p = { x: (sx - v.x) / v.k, y: (sy - v.y) / v.k };
  let best: string | null = null;
  let bestD = 12;
  for (const q of s.points) {
    if (q.hidden && q.cluster <= 1) continue;
    const d = Math.hypot(q.x - p.x, q.y - p.y) * v.k;
    if (!q.hidden && d < bestD) {
      best = q.id;
      bestD = d;
    }
  }
  if (best) return best;
  bestD = 8;
  for (const a of s.arcs) {
    const d = nearArc(a.from, ctrl(a.from, a.to), a.to, p) * v.k;
    if (d < bestD) {
      best = "x:" + a.id;
      bestD = d;
    }
  }
  return best;
}
