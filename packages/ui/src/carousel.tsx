// 照片跟手：滑一下，照片跟着手指转起来。一个连续的位置变量 --mg-carousel-pos 同时驱动轨道位移
// 和每张图的 rotateY / 缩放（离中心越远转得越多），拖动时逐帧跟手，松手按距离和速度吸附到最近一张，
// 用 --mg-decel 慢慢停住。拖动只写 CSS 变量，不触发 React 重渲染。
// 键盘：区域获得焦点后左右方向键切换；另有前后按钮与页码点。减少动态效果时直接跳转。
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";

function Carousel({
  slides,
  label = "图片",
  slideWidth = 74,
  className,
  onChange,
}: {
  slides: ReactNode[];
  label?: string;
  // 单张宽度（占容器百分比），小于 100 时两侧露出相邻的图
  slideWidth?: number;
  className?: string;
  onChange?: (index: number) => void;
}) {
  const [index, setIndex] = useState(0);
  const [dragging, setDragging] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; id: number; samples: { x: number; t: number }[]; moved: boolean } | null>(null);
  const count = slides.length;
  const gap = 16;

  const step = () => {
    const w = root.current?.offsetWidth ?? 0;
    return (w * slideWidth) / 100 + gap;
  };
  const setPos = (pos: number) => root.current?.style.setProperty("--mg-carousel-pos", String(pos));
  const go = (next: number) => {
    const i = Math.min(count - 1, Math.max(0, next));
    setIndex(i);
    setPos(i);
    if (i !== index) onChange?.(i);
  };
  useEffect(() => setPos(index), [index]);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0 || count < 2) return;
    drag.current = { x: e.clientX, id: e.pointerId, samples: [{ x: e.clientX, t: performance.now() }], moved: false };
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    const dx = e.clientX - d.x;
    if (!d.moved) {
      if (Math.abs(dx) < 6) return;
      // 超过 6px 才算拖动：之后捕获指针，轻点仍然能点到图里的链接
      d.moved = true;
      e.currentTarget.setPointerCapture(e.pointerId);
      setDragging(true);
    }
    d.samples.push({ x: e.clientX, t: performance.now() });
    if (d.samples.length > 5) d.samples.shift();
    let pos = index - dx / step();
    // 两端越界时加阻尼，像拉橡皮筋
    if (pos < 0) pos *= 0.35;
    if (pos > count - 1) pos = count - 1 + (pos - (count - 1)) * 0.35;
    setPos(pos);
  };
  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    drag.current = null;
    if (!d.moved) return;
    setDragging(false);
    const dx = e.clientX - d.x;
    const first = d.samples[0];
    const last = d.samples[d.samples.length - 1];
    const velocity = (last.x - first.x) / Math.max(1, last.t - first.t); // px/ms
    // 惯性：按松手速度再往前推约 180ms 的距离，再四舍五入到最近一张
    const projected = index - (dx + velocity * 180) / step();
    go(Math.round(projected));
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return;
    if (e.key === "ArrowRight") go(index + 1);
    else if (e.key === "ArrowLeft") go(index - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(count - 1);
    else return;
    e.preventDefault();
  };

  return (
    <div data-slot="carousel" className={cn("mg-carousel", className)}>
      <div
        ref={root}
        className="mg-carousel-viewport"
        data-dragging={dragging || undefined}
        role="region"
        aria-roledescription="轮播"
        aria-label={`${label}，第 ${index + 1} / ${count} 张`}
        tabIndex={0}
        style={{ "--mg-carousel-slide": slideWidth + "%", "--mg-carousel-gap": gap + "px" } as CSSProperties}
        onKeyDown={onKeyDown}
        onDragStart={(e) => e.preventDefault()}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
      >
        <div className="mg-carousel-track">
          {slides.map((slide, k) => (
            <div
              key={k}
              className="mg-carousel-slide"
              aria-roledescription="幻灯片"
              aria-label={`${k + 1} / ${count}`}
              aria-hidden={k !== index || undefined}
              style={{ "--mg-carousel-k": k } as CSSProperties}
            >
              {slide}
            </div>
          ))}
        </div>
      </div>
      <div className="mg-carousel-controls">
        <Button variant="outline" size="icon-sm" aria-label="上一张" disabled={index === 0} onClick={() => go(index - 1)}>
          <ChevronLeft />
        </Button>
        <span className="mg-carousel-dots">
          {slides.map((_, k) => (
            <button
              key={k}
              type="button"
              className="mg-carousel-dot"
              aria-label={`第 ${k + 1} 张`}
              aria-current={k === index || undefined}
              onClick={() => go(k)}
            />
          ))}
        </span>
        <Button variant="outline" size="icon-sm" aria-label="下一张" disabled={index === count - 1} onClick={() => go(index + 1)}>
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}

export { Carousel };
