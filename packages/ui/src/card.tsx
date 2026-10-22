// 来源 codedthemes/uiable card；本地改为陶瓷 / 石墨面板材质，头部为刻线分隔。
import { ComponentProps } from "react";
import { cn } from "cn";

// size：sm 16px（紧凑卡片、拓扑节点）/ default 20px / lg 24px（登录、初始化、公开页），窄屏 lg 收为 20px。
// variant：default 面板；inset 嵌套在面板里的浅凹分组（模板策略组 / 规则）；plain 去掉外框，只保留结构（弹窗内分区）。
const cardVariants = {
  default: "mg-panel",
  inset: "rounded-[var(--mg-radius)] border border-transparent bg-[color-mix(in_srgb,var(--mg-panel)_70%,var(--mg-well))]",
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
      className={cn("group/card relative min-w-0 text-card-foreground", cardVariants[variant], className)}
      {...props}
    />
  );
}

function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "flex items-start justify-between gap-3 border-b border-[var(--mg-line-soft)] px-5 py-4 group-data-[size=sm]/card:px-4 group-data-[size=sm]/card:py-3 group-data-[size=lg]/card:px-6 group-data-[variant=plain]/card:px-0",
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
      className={cn("text-sm font-semibold tracking-[-0.01em] text-[var(--mg-ink)]", className)}
      {...props}
    />
  );
}

function CardDescription({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("mt-1 text-xs text-[var(--mg-ink-3)]", className)}
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
        "p-5 group-data-[size=sm]/card:p-4 group-data-[size=lg]/card:p-6 max-sm:group-data-[size=lg]/card:p-5 group-data-[variant=plain]/card:px-0 group-data-[variant=plain]/card:py-3.5",
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
        "flex items-center gap-2 rounded-b-[var(--mg-radius-lg)] border-t border-[var(--mg-line-soft)] px-5 py-3 group-data-[size=sm]/card:px-4 group-data-[size=lg]/card:px-6 group-data-[variant=plain]/card:px-0",
        className,
      )}
      {...props}
    />
  );
}

export { Card, CardHeader, CardFooter, CardTitle, CardAction, CardDescription, CardContent };
