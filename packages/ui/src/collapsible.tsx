// 折叠区：基于 @base-ui/react collapsible，替代原生 <details>。标题为强调色文字 + 旋转箭头，展开 / 收起按轻快弹簧过渡高度。
// 高度过渡是规范里唯一允许的布局属性过渡：内容高度未知，只能按 Base UI 的 --collapsible-panel-height 展开（见 design.html#console-visual）。
// variant="card"：带边框的独立分块（如草稿差异的单个文件）；默认为内联的“展开更多”。
import type { ReactNode } from "react";
import { Collapsible as CollapsiblePrimitive } from "@base-ui/react/collapsible";
import { ChevronRight } from "lucide-react";
import { cn } from "cn";

function Collapsible({
  title,
  children,
  defaultOpen,
  open,
  onOpenChange,
  variant = "inline",
  className,
  panelClassName,
}: {
  title: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  variant?: "inline" | "card";
  className?: string;
  panelClassName?: string;
}) {
  return (
    <CollapsiblePrimitive.Root
      data-slot="collapsible"
      defaultOpen={defaultOpen}
      open={open}
      onOpenChange={onOpenChange ? (v) => onOpenChange(v) : undefined}
      className={cn("min-w-0", variant === "card" && "rounded-[var(--weir-radius)] border border-[var(--weir-line)]", className)}
    >
      <CollapsiblePrimitive.Trigger
        data-slot="collapsible-trigger"
        className={cn(
          // 行内标题视觉紧凑，触屏下行高做到 44；card 变体整行可点，按下叠填充色
          "group/collapsible weir-press relative flex max-w-full min-h-7 cursor-pointer items-center gap-1 rounded-[var(--weir-radius-xs)] text-left text-body text-[var(--weir-accent-text)] touch:min-h-11",
          variant === "card" && "w-full rounded-[var(--weir-radius-9)] px-3 py-1 text-[var(--weir-ink)]",
        )}
      >
        <ChevronRight className="size-4 shrink-0 transition-[rotate] duration-[var(--weir-spring-dur)] ease-[var(--weir-spring)] group-data-[panel-open]/collapsible:rotate-90" aria-hidden />
        <span className="flex min-w-0 flex-wrap items-center gap-2">{title}</span>
      </CollapsiblePrimitive.Trigger>
      <CollapsiblePrimitive.Panel
        data-slot="collapsible-panel"
        className={cn(
          "h-[var(--collapsible-panel-height)] overflow-hidden transition-[height] duration-[var(--weir-spring-dur)] ease-[var(--weir-spring)] data-[ending-style]:h-0 data-[starting-style]:h-0",
          panelClassName,
        )}
      >
        <div className={cn(variant === "card" ? "px-3 pb-3" : "pt-2")}>{children}</div>
      </CollapsiblePrimitive.Panel>
    </CollapsiblePrimitive.Root>
  );
}

export { Collapsible };
