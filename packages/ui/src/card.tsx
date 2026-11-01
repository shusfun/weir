// 来源 codedthemes/uiable card；本地改为 iOS inset grouped 分组卡片：圆角 12、浅色下细边框、深色下纯色块，头部用细分隔线。
// 带 onClick 的卡片按下缩小并叠填充色（.weir-press）。
import { ComponentProps } from "react";
import { cn } from "cn";

// size：sm 12px（紧凑卡片、拓扑节点）/ default 16px / lg 24px（登录、初始化、公开页），窄屏 lg 收为 20px。
// variant：default 面板；inset 嵌套在面板里的浅凹分组（模板策略组 / 规则）；plain 去掉外框，只保留结构（弹窗内分区）。
const cardVariants = {
  default: "weir-panel",
  inset: "rounded-[var(--weir-radius)] border border-transparent bg-[var(--weir-fill-4)]",
  plain: "border-0 bg-transparent shadow-none",
} as const;

function Card({
  className,
  size = "default",
  variant = "default",
  ...props
}: ComponentProps<"div"> & { size?: "default" | "sm" | "lg"; variant?: keyof typeof cardVariants }) {
  return (
    <div
      data-slot="card"
      data-size={size}
      data-variant={variant}
      className={cn("group/card relative min-w-0 text-card-foreground", cardVariants[variant], props.onClick && "weir-press cursor-pointer", className)}
      {...props}
    />
  );
}

function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "flex items-start justify-between gap-3 border-b border-[var(--weir-line-soft)] px-4 py-3 group-data-[size=sm]/card:px-3 group-data-[size=sm]/card:py-2 group-data-[size=lg]/card:px-6 group-data-[size=lg]/card:py-4 group-data-[variant=plain]/card:px-0",
        className,
      )}
      {...props}
    />
  );
}

function CardTitle({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-title"
      className={cn("text-heading font-semibold tracking-[-0.01em] text-[var(--weir-ink)]", className)}
      {...props}
    />
  );
}

function CardDescription({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("mt-1 text-meta text-[var(--weir-ink-3)]", className)}
      {...props}
    />
  );
}

function CardAction({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn("ml-auto flex shrink-0 items-center gap-2", className)}
      {...props}
    />
  );
}

function CardContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn(
        "p-4 group-data-[size=sm]/card:p-3 group-data-[size=lg]/card:p-6 max-sm:group-data-[size=lg]/card:p-5 group-data-[variant=plain]/card:px-0 group-data-[variant=plain]/card:py-3",
        className,
      )}
      {...props}
    />
  );
}

function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "flex items-center gap-2 rounded-b-[var(--weir-radius-card)] border-t border-[var(--weir-line-soft)] px-4 py-3 group-data-[size=sm]/card:px-3 group-data-[size=lg]/card:px-6 group-data-[variant=plain]/card:px-0",
        className,
      )}
      {...props}
    />
  );
}

export { Card, CardHeader, CardFooter, CardTitle, CardAction, CardDescription, CardContent };
