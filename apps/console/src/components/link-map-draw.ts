// 链路图的绘制：底图 → 连线（粗细与粒子随速率）→ 直连光环 → 点（VPS 圆点、出口菱形）→ 新数据脉冲 → 标签。
// 速率未知或数据中断的线画成灰色虚线；速率为 0 的线变暗、没有粒子。只在两次真实采样之间过渡。
import { arcControl, onArc, type Colors, type Pt, type World } from "./link-map-geo";

export interface ScenePoint extends Pt {
  id: string; // g:<gate> 或 x:<exit>
  kind: "gate" | "exit";
  label: string;
  tone: "ok" | "off" | "warn" | "accent" | "info";
  cluster: number; // 同一位置合并的点数（>1 时画数字）
  hidden: boolean; // 被合并进别的点
  dim: boolean;
  owner?: "admin" | "self";
}
export interface SceneArc {
  id: string; // 出口 ID
  from: Pt;
  to: Pt;
  rate: number | null; // 字节 / 秒；null 为未知
  stale: boolean;
  dim: boolean;
}
export interface Scene {
  world: World;
  colors: Colors;
  points: ScenePoint[];
  arcs: SceneArc[];
  halos: { at: Pt; rate: number | null; stale: boolean; dim: boolean }[];
  pulses: { at: Pt; age: number }[]; // age：0–1
  selected: string | null;
  hover: string | null;
  motion: boolean;
}
export interface View {
  k: number;
  x: number;
  y: number;
}

const strength = (rate: number) => Math.min(1, Math.log10(1 + rate) / 7); // 约 10 MB/s 接近满

export function draw(ctx: CanvasRenderingContext2D, w: number, h: number, dpr: number, v: View, s: Scene, now: number) {
  const c = s.colors;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);
  ctx.setTransform(dpr * v.k, 0, 0, dpr * v.k, dpr * v.x, dpr * v.y);
  ctx.fillStyle = c.sea;
  ctx.fill(s.world.sphere);
  ctx.fillStyle = c.land;
  ctx.fill(s.world.land);
  ctx.strokeStyle = c.border;
  ctx.lineWidth = 0.6 / v.k;
  ctx.stroke(s.world.border);
  const px = (n: number) => n / v.k; // 屏幕像素换成世界坐标

  for (const a of s.arcs) {
    const ctrl = arcControl(a.from, a.to);
    const known = a.rate !== null && !a.stale;
    const str = known ? strength(a.rate as number) : 0;
    const focus = s.selected === "x:" + a.id || s.hover === "x:" + a.id;
    ctx.globalAlpha = a.dim ? 0.18 : 1;
    ctx.beginPath();
    ctx.moveTo(a.from.x, a.from.y);
    ctx.quadraticCurveTo(ctrl.x, ctrl.y, a.to.x, a.to.y);
    if (!known) {
      ctx.setLineDash([px(4), px(4)]);
      ctx.strokeStyle = c.off;
      ctx.lineWidth = px(1.2);
      ctx.stroke();
      ctx.setLineDash([]);
    } else {
      const g = ctx.createLinearGradient(a.from.x, a.from.y, a.to.x, a.to.y);
      g.addColorStop(0, c.accent);
      g.addColorStop(1, c.info);
      ctx.strokeStyle = str === 0 ? c.off : g;
      ctx.lineWidth = px(1 + str * 4 + (focus ? 1.5 : 0));
      if (str > 0 && !a.dim) {
        ctx.shadowColor = c.accent;
        ctx.shadowBlur = 6 + str * 10;
      }
      ctx.stroke();
      ctx.shadowBlur = 0;
      if (s.motion && str > 0 && !a.dim) {
        const n = 2 + Math.round(str * 10);
        const speed = 0.12 + str * 0.5; // 每秒走过的比例
        ctx.fillStyle = c.info;
        for (let i = 0; i < n; i++) {
          const t = ((now / 1000) * speed + i / n) % 1;
          const q = onArc(a.from, ctrl, a.to, t);
          ctx.beginPath();
          ctx.arc(q.x, q.y, px(1.4 + str * 1.8), 0, Math.PI * 2);
          ctx.fill();
        }
      }
    }
  }
  ctx.globalAlpha = 1;

  for (const hl of s.halos) {
    const known = hl.rate !== null && !hl.stale;
    const str = known ? strength(hl.rate as number) : 0;
    ctx.globalAlpha = hl.dim ? 0.18 : 1;
    ctx.beginPath();
    ctx.arc(hl.at.x, hl.at.y, px(14), 0, Math.PI * 2);
    ctx.setLineDash(known ? [px(3), px(5)] : [px(2), px(4)]);
    ctx.lineDashOffset = s.motion && str > 0 ? -(now / 1000) * px(20 + str * 60) : 0;
    ctx.strokeStyle = known && str > 0 ? c.ok : c.off;
    ctx.lineWidth = px(1.2 + str * 2.5);
    ctx.stroke();
    ctx.setLineDash([]);
  }
  ctx.globalAlpha = 1;

  for (const p of s.points) {
    if (p.hidden) continue;
    const color = { ok: c.ok, off: c.off, warn: c.warn, accent: c.accent, info: c.info }[p.tone];
    const r = px(p.kind === "gate" ? 6 : 5);
    ctx.globalAlpha = p.dim ? 0.25 : 1;
    if (p.kind === "gate") {
      const glow = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, r * 3);
      glow.addColorStop(0, color);
      glow.addColorStop(1, "transparent");
      ctx.fillStyle = glow;
      ctx.beginPath();
      ctx.arc(p.x, p.y, r * 3, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.arc(p.x, p.y, r, 0, Math.PI * 2);
      ctx.fill();
    } else {
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.moveTo(p.x, p.y - r * 1.3);
      ctx.lineTo(p.x + r, p.y);
      ctx.lineTo(p.x, p.y + r * 1.3);
      ctx.lineTo(p.x - r, p.y);
      ctx.closePath();
      ctx.fill();
    }
    ctx.strokeStyle = c.land;
    ctx.lineWidth = px(1.5);
    ctx.stroke();
    if (s.selected === p.id || s.hover === p.id) {
      ctx.beginPath();
      ctx.arc(p.x, p.y, r * 2.2, 0, Math.PI * 2);
      ctx.strokeStyle = c.ink;
      ctx.lineWidth = px(1.5);
      ctx.stroke();
    }
    if (p.cluster > 1) {
      ctx.font = `600 ${px(10)}px ${c.font}`;
      ctx.fillStyle = c.ink;
      ctx.textAlign = "left";
      ctx.fillText(String(p.cluster), p.x + r * 1.4, p.y - r * 1.2);
    }
    if (v.k >= 1.6 || s.selected === p.id) {
      ctx.font = `${px(11)}px ${c.font}`;
      ctx.textAlign = "center";
      ctx.lineWidth = px(3);
      ctx.strokeStyle = c.land;
      ctx.strokeText(p.label, p.x, p.y + r * 3.2);
      ctx.fillStyle = c.ink;
      ctx.fillText(p.label, p.x, p.y + r * 3.2);
    }
  }
  ctx.globalAlpha = 1;

  for (const pu of s.pulses) {
    ctx.globalAlpha = 1 - pu.age;
    ctx.beginPath();
    ctx.arc(pu.at.x, pu.at.y, px(6 + pu.age * 22), 0, Math.PI * 2);
    ctx.strokeStyle = c.accent;
    ctx.lineWidth = px(1.5);
    ctx.stroke();
  }
  ctx.globalAlpha = 1;
}
