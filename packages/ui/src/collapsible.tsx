// 折叠区：基于 @base-ui/react collapsible，替代原生 <details>。标题为强调色文字 + 旋转箭头，展开 / 收起有高度过渡。
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
      className={cn("min-w-0", variant === "card" && "rounded-[var(--mg-radius)] border border-[var(--mg-line)]", className)}
    >
      <CollapsiblePrimitive.Trigger
        data-slot="collapsible-trigger"
        className={cn(
          "group/collapsible flex max-w-full cursor-pointer items-center gap-1.5 rounded-[4px] text-left text-[12.5px] text-[var(--mg-accent-text)] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--mg-accent-text)]",
          variant === "card" && "w-full px-2 py-1.5 text-[var(--mg-ink)]",
        )}
      >
        <ChevronRight className="size-3.5 shrink-0 transition-[rotate] duration-[var(--mg-fast)] group-data-[panel-open]/collapsible:rotate-90" aria-hidden />
        <span className="flex min-w-0 flex-wrap items-center gap-2">{title}</span>
      </CollapsiblePrimitive.Trigger>
      <CollapsiblePrimitive.Panel
        data-slot="collapsible-panel"
        className={cn(
          "h-[var(--collapsible-panel-height)] overflow-hidden transition-[height] duration-[var(--mg-mid)] ease-[var(--mg-ease)] data-[ending-style]:h-0 data-[starting-style]:h-0",
          panelClassName,
        )}
      >
        <div className={cn(variant === "card" ? "px-2 pb-2" : "pt-2")}>{children}</div>
      </CollapsiblePrimitive.Panel>
    </CollapsiblePrimitive.Root>
  );
}

export { Collapsible };
