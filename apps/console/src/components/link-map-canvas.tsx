// 链路图画布：平铺世界地图，可拖动、滚轮或双指缩放（不旋转）；悬停显示摘要，点击选中（同一位置的多个点先放大）。
// 画布只是展示，键盘与读屏用旁边的路径列表；系统设置了「减少动态效果」时连线不流动、不出脉冲。
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@weir/ui";
import { Maximize2, Minus, Plus } from "lucide-react";
import { useIsDark } from "../theme";
import type { LinkMap, LiveStore } from "../lib/linkmap";
import { draw, type Scene, type View } from "./link-map-draw";
import { arcControl, buildWorld, centerLon, nearArc, readColors, type World } from "./link-map-geo";
import { buildScene, hitTest, type Focus } from "./link-map-scene";

const minK = 1;
const maxK = 12;

function useReducedMotion() {
  const [reduced, setReduced] = useState(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches);
  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    const on = () => setReduced(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  return reduced;
}

export function LinkMapCanvas({ map, store, focus, selected, onSelect, tooltip }: {
  map: LinkMap;
  store: LiveStore;
  focus: Focus;
  selected: string | null;
  onSelect: (id: string | null) => void;
  tooltip: (id: string) => string;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const tip = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const view = useRef<View>({ k: 1, x: 0, y: 0 });
  const hover = useRef<string | null>(null);
  const scene = useRef<Scene | null>(null);
  const dark = useIsDark();
  const reduced = useReducedMotion();
  const colors = useMemo(() => readColors(), [dark]);
  const lon = useMemo(() => centerLon([...map.gates.map((g) => g.place), ...map.exits.map((x) => x.place)]), [map.version]);
  const world = useMemo<World | null>(() => (size.w > 0 ? buildWorld(size.w, size.h, lon) : null), [size.w, size.h, lon]);
  const live = useRef({ map, focus, selected, onSelect, tooltip });
  live.current = { map, focus, selected, onSelect, tooltip };

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      const w = el.clientWidth;
      setSize({ w, h: Math.round(Math.min(Math.max(w * 0.52, 240), 560)) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  // 每一帧都从快照与实时仓库重新组装场景（数量很小），React 不参与动画。
  useEffect(() => {
    const cv = canvas.current;
    if (!cv || !world) return;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    cv.width = size.w * dpr;
    cv.height = size.h * dpr;
    const ctx = cv.getContext("2d");
    if (!ctx) return;
    let raf = 0;
    const frame = (now: number) => {
      const { map: m, focus: f, selected: sel } = live.current;
      const s = buildScene(m, store, world, colors, view.current, f, sel, hover.current, !reduced, now);
      scene.current = s;
      draw(ctx, size.w, size.h, dpr, view.current, s, now);
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [world, colors, reduced, store, size.w, size.h]);
  // 平移限制在地图范围内；缩放以指针位置为中心。
  const clamp = (v: View): View => {
    const k = Math.min(maxK, Math.max(minK, v.k));
    return { k, x: Math.min(0, Math.max(size.w - size.w * k, v.x)), y: Math.min(0, Math.max(size.h - size.h * k, v.y)) };
  };
  const zoomAt = (factor: number, cx: number, cy: number) => {
    const v = view.current;
    const k = Math.min(maxK, Math.max(minK, v.k * factor));
    view.current = clamp({ k, x: cx - ((cx - v.x) * k) / v.k, y: cy - ((cy - v.y) * k) / v.k });
  };
  useEffect(() => {
    const cv = canvas.current;
    if (!cv) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = cv.getBoundingClientRect();
      zoomAt(Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top);
    };
    cv.addEventListener("wheel", wheel, { passive: false });
    return () => cv.removeEventListener("wheel", wheel);
  }, [size.w, size.h]);

  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const drag = useRef<{ x: number; y: number; vx: number; vy: number; moved: boolean; pinch?: number } | null>(null);
  const local = (e: React.PointerEvent) => {
    const r = canvas.current!.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  };
  const onDown = (e: React.PointerEvent) => {
    canvas.current?.setPointerCapture(e.pointerId);
    const p = local(e);
    pointers.current.set(e.pointerId, p);
    const pts = [...pointers.current.values()];
    drag.current = { x: p.x, y: p.y, vx: view.current.x, vy: view.current.y, moved: false, pinch: pts.length === 2 ? Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y) : undefined };
  };
  const onMove = (e: React.PointerEvent) => {
    const p = local(e);
    const d = drag.current;
    if (pointers.current.has(e.pointerId)) pointers.current.set(e.pointerId, p);
    if (d && pointers.current.size === 2 && d.pinch) {
      const pts = [...pointers.current.values()];
      const dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      zoomAt(dist / d.pinch, (pts[0].x + pts[1].x) / 2, (pts[0].y + pts[1].y) / 2);
      d.pinch = dist;
      d.moved = true;
      return;
    }
    if (d && pointers.current.size === 1) {
      if (Math.hypot(p.x - d.x, p.y - d.y) > 4) d.moved = true;
      if (d.moved) view.current = clamp({ k: view.current.k, x: d.vx + p.x - d.x, y: d.vy + p.y - d.y });
    }
    const s = scene.current;
    const id = s && !d?.moved ? hitTest(s, view.current, p.x, p.y, nearArc, arcControl) : null;
    hover.current = id;
    const t = tip.current;
    if (t) {
      t.hidden = !id;
      if (id) {
        t.textContent = live.current.tooltip(id);
        t.style.transform = `translate(${Math.min(p.x + 12, size.w - 220)}px, ${p.y + 12}px)`;
      }
    }
    if (canvas.current) canvas.current.style.cursor = id ? "pointer" : d?.moved ? "grabbing" : "grab";
  };
  const onUp = (e: React.PointerEvent) => {
    const d = drag.current;
    pointers.current.delete(e.pointerId);
    if (pointers.current.size > 0) return;
    drag.current = null;
    if (!d || d.moved || !scene.current) return;
    const p = local(e);
    const id = hitTest(scene.current, view.current, p.x, p.y, nearArc, arcControl);
    const pt = scene.current.points.find((q) => q.id === id);
    if (pt && pt.cluster > 1 && view.current.k < maxK) {
      zoomAt(2.5, p.x, p.y); // 同一位置有多个点：先放大，分开后再选
      return;
    }
    live.current.onSelect(id);
  };
  const reset = () => (view.current = { k: 1, x: 0, y: 0 });
  const center = (f: number) => zoomAt(f, size.w / 2, size.h / 2);
  return (
    <div ref={wrap} className="relative w-full overflow-hidden rounded-[var(--mg-radius)] border border-[var(--mg-line)] bg-[var(--mg-well-deep)]" style={{ height: size.h || 240 }}>
      <canvas
        ref={canvas}
        role="img"
        aria-label={`链路地图：${map.gates.length} 台 VPS，${map.exits.filter((x) => x.kind !== "direct").length} 个出口；详细内容见下方列表`}
        className="block touch-none select-none"
        style={{ width: size.w, height: size.h, cursor: "grab" }}
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onUp}
        onPointerLeave={() => {
          hover.current = null;
          if (tip.current) tip.current.hidden = true;
        }}
      />
      <div ref={tip} hidden className="pointer-events-none absolute left-0 top-0 max-w-[220px] whitespace-pre-line rounded-[var(--mg-radius-sm)] border border-[var(--mg-line)] bg-[var(--mg-panel)] px-2 py-1 text-xs text-[var(--mg-ink)] shadow" />
      <div className="absolute right-2 top-2 flex flex-col gap-1">
        <Button size="icon-sm" variant="outline" aria-label="放大" onClick={() => center(1.6)}><Plus /></Button>
        <Button size="icon-sm" variant="outline" aria-label="缩小" onClick={() => center(1 / 1.6)}><Minus /></Button>
        <Button size="icon-sm" variant="outline" aria-label="回到全图" onClick={reset}><Maximize2 /></Button>
      </div>
    </div>
  );
}
