// 标签键：一个可点的小键帽，带状态灯、名称与一段说明（如节点的「协议 · 端口」）。按下有内凹与水波，
// 点开的弹窗从按下的位置展开。add 变体是虚线框的「＋ 新增」，与同一行的标签等高。
// ChipLabel 是同样外观的只读标签（平放、不凸起），用在不能点的地方，不做成灰掉的按钮。
import type { ComponentProps, ReactNode } from "react";
import { cn } from "cn";
import { StatusLamp, type LampTone } from "./lamp";
import { Ripple } from "./ripple";

function Chip({
  tone,
  meta,
  add = false,
  selected = false,
  className,
  children,
  ...props
}: ComponentProps<"button"> & { tone?: LampTone; meta?: ReactNode; add?: boolean; selected?: boolean }) {
  return (
    <button
      type="button"
      data-slot="chip"
      data-add={add || undefined}
      className={cn(
        "weir-key relative inline-flex min-h-8 max-w-full min-w-0 items-center gap-2 rounded-full px-3 py-1 text-[12.5px] font-medium select-none disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-3.5 [&_svg]:shrink-0",
        add && "weir-key-ghost border border-dashed border-[var(--weir-line)] text-[var(--weir-ink-2)] hover:text-[var(--weir-ink)]",
        selected && "is-pressed",
        className,
      )}
      {...props}
    >
      {tone && <StatusLamp size="sm" tone={tone} />}
      {/* 图标与文字同一行（SVG 默认是块元素） */}
      <span className="inline-flex min-w-0 items-center gap-1 truncate whitespace-nowrap">{children}</span>
      {meta && <small className="shrink-0 font-normal text-[var(--weir-ink-3)] tabular-nums">{meta}</small>}
      <Ripple />
    </button>
  );
}

function ChipLabel({ tone, meta, className, children, ...props }: ComponentProps<"span"> & { tone?: LampTone; meta?: ReactNode }) {
  return (
    <span
      data-slot="chip-label"
      className={cn("inline-flex min-h-8 max-w-full min-w-0 items-center gap-2 rounded-full border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-3 py-1 text-[12.5px] font-medium", className)}
      {...props}
    >
      {tone && <StatusLamp size="sm" tone={tone} />}
      <span className="min-w-0 truncate">{children}</span>
      {meta && <small className="shrink-0 font-normal text-[var(--weir-ink-3)] tabular-nums">{meta}</small>}
    </span>
  );
}

export { Chip, ChipLabel };
