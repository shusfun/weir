// 卡片抽走：后面压着的两张依次缩小下移、露出边缘——看得见后面还有，堆叠才成立。
// 顶卡跟手拖动并随位移轻微旋转；拖过三分之一宽或甩得够快就飞出，后面的卡依次补位，否则回弹。
// 键盘：区域获得焦点后左右方向键切换；另有“上一张 / 下一张”按钮。减少动态效果时直接切换。
import { useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";
import { prefersReducedMotion } from "./motion";

const depth = 3;

function CardStack<T>({
  items,
  getKey,
  renderItem,
  label = "卡片",
  loop = true,
  onChange,
  className,
}: {
  items: T[];
  getKey: (item: T) => string | number;
  renderItem: (item: T) => ReactNode;
  label?: string;
  loop?: boolean;
  onChange?: (index: number) => void;
  className?: string;
}) {
  const [index, setIndex] = useState(0);
  // 顶卡状态：拖动中的位移，或正在飞出的方向
  const [drag, setDrag] = useState<{ x: number; leaving?: -1 | 1; entering?: boolean } | null>(null);
  const start = useRef<{ x: number; t: number; id: number } | null>(null);
  const width = useRef(320);
  const count = items.length;
  const canNext = loop ? count > 1 : index < count - 1;
  const canPrev = loop ? count > 1 : index > 0;

  const go = (next: number) => {
    const i = ((next % count) + count) % count;
    setIndex(i);
    onChange?.(i);
  };
  const dismiss = (dir: -1 | 1) => {
    if (!canNext || drag?.leaving) return;
    if (prefersReducedMotion()) return go(index + 1);
    setDrag({ x: dir * width.current * 1.3, leaving: dir });
  };
  const back = () => {
    if (!canPrev || drag) return;
    go(index - 1);
    if (prefersReducedMotion()) return;
    // 上一张从左侧飞回顶上：先放到屏外，下一帧去掉位移让它滑回
    setDrag({ x: -width.current * 1.3, entering: true });
    requestAnimationFrame(() => requestAnimationFrame(() => setDrag(null)));
  };

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0 || drag?.leaving || !canNext) return;
    width.current = e.currentTarget.offsetWidth || 320;
    start.current = { x: e.clientX, t: performance.now(), id: e.pointerId };
    e.currentTarget.setPointerCapture(e.pointerId);
    setDrag({ x: 0 });
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!start.current || start.current.id !== e.pointerId) return;
    setDrag({ x: e.clientX - start.current.x });
  };
  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    const s = start.current;
    if (!s || s.id !== e.pointerId) return;
    start.current = null;
    const dx = e.clientX - s.x;
    const speed = Math.abs(dx) / Math.max(1, performance.now() - s.t);
    if (Math.abs(dx) > width.current / 3 || (speed > 0.6 && Math.abs(dx) > 24)) dismiss(dx > 0 ? 1 : -1);
    else setDrag(null);
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return;
    if (e.key === "ArrowRight") {
      e.preventDefault();
      dismiss(1);
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      back();
    }
  };

  const visible = Array.from({ length: Math.min(depth, count) }, (_, k) => items[(index + k) % count]).filter(
    (_, k) => loop || index + k < count,
  );
  const dragging = !!start.current;
  return (
    <div data-slot="card-stack" className={cn("mg-stack", className)}>
      <div
        className="mg-stack-deck"
        role="group"
        aria-roledescription="卡片堆叠"
        aria-label={`${label}，第 ${index + 1} / ${count} 张`}
        tabIndex={0}
        onKeyDown={onKeyDown}
      >
        {visible
          .map((item, k) => {
            const top = k === 0;
            // 顶卡飞出时，后面的卡提前往前补一位
            const slot = drag?.leaving && !top ? k - 1 : k;
            const style = {
              "--mg-stack-k": slot,
              zIndex: depth - k,
              ...(top && drag ? { translate: `${drag.x}px 0`, rotate: `${drag.x * 0.04}deg` } : null),
            } as CSSProperties;
            return (
              <div
                key={getKey(item)}
                className="mg-stack-card"
                data-top={top || undefined}
                data-dragging={(top && dragging) || undefined}
                data-leaving={(top && drag?.leaving) || undefined}
                data-entering={(top && drag?.entering) || undefined}
                aria-hidden={!top || undefined}
                inert={!top}
                style={style}
                onPointerDown={top ? onPointerDown : undefined}
                onPointerMove={top ? onPointerMove : undefined}
                onPointerUp={top ? onPointerUp : undefined}
                onPointerCancel={top ? onPointerUp : undefined}
                onTransitionEnd={(e) => {
                  if (!top || !drag?.leaving || e.target !== e.currentTarget || e.propertyName !== "translate") return;
                  setDrag(null);
                  go(index + 1);
                }}
              >
                {renderItem(item)}
              </div>
            );
          })
          .reverse()}
      </div>
      <div className="mg-stack-controls">
        <Button variant="outline" size="icon-sm" aria-label="上一张" disabled={!canPrev} onClick={back}>
          <ChevronLeft />
        </Button>
        <span className="mg-digits text-xs text-[var(--mg-ink-3)]" aria-live="polite">
          {index + 1} / {count}
        </span>
        <Button variant="outline" size="icon-sm" aria-label="下一张" disabled={!canNext} onClick={() => dismiss(1)}>
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}

export { CardStack };
