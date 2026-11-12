// 来源 codedthemes/uiable card；本地改为 iOS inset grouped 分组卡片：圆角 12、浅色下细边框、深色下纯色块，头部用细分隔线。
// 带 onClick 的卡片按下缩小并叠填充色（.weir-press）。
import { ComponentProps, type ReactNode } from "react";
import { cn } from "cn";

// size：sm 13 / default 17 / lg 25，窄屏 lg 21。头、正文和底栏共用横向内边距；外壳不加内边距。
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
      className={cn("weir-card group/card relative min-w-0 text-card-foreground", cardVariants[variant], props.onClick && "weir-press cursor-pointer", className)}
      {...props}
    />
  );
}

function CardHeader({ className, title, description, actions, children, ...props }: Omit<ComponentProps<"div">, "title"> & { title?: ReactNode; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "weir-card-header flex items-start justify-between border-b border-[var(--weir-line-soft)]",
        className,
      )}
      {...props}
    >
      {title != null || description != null ? <div className="min-w-0 flex-1">{title != null && <CardTitle>{title}</CardTitle>}{description != null && <CardDescription>{description}</CardDescription>}</div> : null}
      {children}
      {actions != null && <CardAction>{actions}</CardAction>}
    </div>
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
      className={cn("mt-[var(--weir-space-1)] text-meta text-[var(--weir-ink-3)]", className)}
      {...props}
    />
  );
}

function CardAction({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn("ml-auto flex shrink-0 flex-wrap items-center justify-end gap-[var(--weir-space-2)]", className)}
      {...props}
    />
  );
}

function CardContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn(
        "weir-card-content",
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
        "weir-card-footer flex items-center rounded-b-[var(--weir-radius-card)] border-t border-[var(--weir-line-soft)]",
        className,
      )}
      {...props}
    />
  );
}

export { Card, CardHeader, CardFooter, CardTitle, CardAction, CardDescription, CardContent };
