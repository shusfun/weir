// dsh 外框：标题栏可拖、右下角可改大小、可缩成胶囊。内容区只放 dsh 自己的界面，这里不画对话。
// 不设模态焦点圈，页面操作仍可用；确认窗的层级更高。页面名或草稿数缺失显示「未知」。
import { useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { Maximize2, Minus, X } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";
import { Meter } from "./readout";
import { StatusLamp } from "./lamp";
import { Step, Steps } from "./display";

function tokenPx(name: string) {
  const n = parseFloat(getComputedStyle(document.documentElement).getPropertyValue(name));
  return Number.isFinite(n) ? n : 0;
}

type DshStep = { label: string; status: "done" | "current" | "upcoming"; caption?: ReactNode };

function DshFrame({
  title = "dsh",
  pageLabel,
  notice = "密码、UUID、令牌不要贴进对话，到 Weir 的确认窗里填。",
  ready = true,
  steps,
  progress,
  draftCount,
  collapsed,
  onCollapsedChange,
  onClose,
  children,
  className,
}: {
  title?: ReactNode;
  pageLabel?: ReactNode | null;
  notice?: ReactNode | null;
  ready?: boolean;
  steps?: DshStep[];
  progress?: number | null;
  draftCount?: number | null;
  collapsed?: boolean;
  onCollapsedChange?: (next: boolean) => void;
  onClose?: () => void;
  children?: ReactNode;
  className?: string;
}) {
  const [innerCollapsed, setInnerCollapsed] = useState(false);
  const isCollapsed = collapsed ?? innerCollapsed;
  const setCollapsed = (next: boolean) => {
    onCollapsedChange?.(next);
    if (collapsed == null) setInnerCollapsed(next);
  };
  const [max, setMax] = useState(false);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [box, setBox] = useState<{ w: number; h: number } | null>(null);
  const frame = useRef<HTMLElement>(null);
  const drag = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null);

  function onDragDown(e: ReactPointerEvent<HTMLElement>) {
    if (max || (e.target as HTMLElement).closest("button")) return;
    drag.current = { x: e.clientX, y: e.clientY, ox: offset.x, oy: offset.y };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function onDragMove(e: ReactPointerEvent<HTMLElement>) {
    const d = drag.current;
    if (!d) return;
    setOffset({ x: d.ox + e.clientX - d.x, y: d.oy + e.clientY - d.y });
  }
  function onResizeDown(e: ReactPointerEvent<HTMLButtonElement>) {
    e.stopPropagation();
    const el = frame.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const origin = { x: e.clientX, y: e.clientY, w: rect.width, h: rect.height };
    e.currentTarget.setPointerCapture(e.pointerId);
    const minW = tokenPx("--weir-dsh-min-w");
    const minH = tokenPx("--weir-dsh-min-h");
    const move = (ev: PointerEvent) => {
      setBox({
        w: Math.max(minW, origin.w + ev.clientX - origin.x),
        h: Math.max(minH, origin.h + ev.clientY - origin.y),
      });
    };
    const up = () => {
      e.currentTarget.removeEventListener("pointermove", move);
      e.currentTarget.removeEventListener("pointerup", up);
    };
    e.currentTarget.addEventListener("pointermove", move);
    e.currentTarget.addEventListener("pointerup", up);
  }

  if (isCollapsed) {
    const drafts = draftCount == null ? "未知" : `${draftCount} 份草稿`;
    return (
      <button type="button" data-slot="dsh-frame" data-collapsed className="weir-dsh-capsule" data-drafts={draftCount ? "" : undefined} onClick={() => setCollapsed(false)}>
        <StatusLamp tone="info" />
        <span>{title} · 已缩小 · {drafts}</span>
      </button>
    );
  }

  const page = pageLabel == null || pageLabel === "" ? "未知" : pageLabel;
  return (
    <section
      ref={frame}
      data-slot="dsh-frame"
      data-max={max || undefined}
      className={cn("weir-dsh", className)}
      aria-label={typeof title === "string" ? title : "dsh"}
      style={max || !box ? { transform: max ? undefined : `translate(${offset.x}px, ${offset.y}px)` } : { width: box.w, height: box.h, transform: `translate(${offset.x}px, ${offset.y}px)` }}
    >
      <header
        className="weir-dsh-bar"
        onPointerDown={onDragDown}
        onPointerMove={onDragMove}
        onPointerUp={() => { drag.current = null; }}
        onDoubleClick={() => setMax((v) => !v)}
      >
        <div>
          <strong>{title}</strong>
          <div className="weir-dsh-page">已读到当前页面：{page}</div>
        </div>
        <div className="weir-dsh-tools">
          <Button type="button" size="icon-xs" variant="ghost" aria-label="缩小" onClick={() => setCollapsed(true)}><Minus /></Button>
          <Button type="button" size="icon-xs" variant="ghost" aria-label={max ? "还原" : "最大化"} onClick={() => setMax((v) => !v)}><Maximize2 /></Button>
          {onClose && <Button type="button" size="icon-xs" variant="ghost" aria-label="关闭" onClick={onClose}><X /></Button>}
        </div>
      </header>
      {notice != null && notice !== "" && <div className="weir-dsh-notice">{notice}</div>}
      <div className="weir-dsh-body">
        {ready ? children : (
          <div className="weir-dsh-setup">
            <p className="weir-dsh-setup-title">还没准备好</p>
            {steps && steps.length > 0 ? (
              <Steps>
                {steps.map((step, i) => (
                  <Step key={step.label} index={i} label={step.label} caption={step.caption} status={step.status === "done" ? "complete" : step.status} />
                ))}
              </Steps>
            ) : (
              <p>未知</p>
            )}
            <Meter label="准备进度" value={progress ?? null} max={progress == null ? null : 100} />
          </div>
        )}
      </div>
      <button type="button" className="weir-dsh-grip" aria-label="调整大小" onPointerDown={onResizeDown} />
    </section>
  );
}

export { DshFrame };
export type { DshStep };
