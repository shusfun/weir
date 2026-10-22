// MiGate 页面骨架：页头、区块、五态页面状态、分页、骨架、键帽。
import { ComponentProps, ReactNode } from "react";
import { cn } from "cn";
import { AlertCircle, Inbox, LoaderCircle, Lock, CheckCircle2, ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "./button";

function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  className,
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <header data-slot="page-header" className={cn("flex flex-wrap items-end justify-between gap-x-6 gap-y-3", className)}>
      <div className="min-w-0 flex-1">
        {eyebrow && <div className="mg-engraved mb-1.5">{eyebrow}</div>}
        <h2 className="m-0 text-[19px] font-semibold tracking-[-0.02em] text-[var(--mg-ink)]">{title}</h2>
        {description && <p className="mt-1.5 mb-0 max-w-[72ch] text-[13px] leading-relaxed text-[var(--mg-ink-3)]">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

// 区块标题：卡片 / 分区顶部的标题 + 说明 + 右侧操作。size=sm 用于列表项、步骤卡片内的小标题行。
function SectionHeader({
  title,
  description,
  actions,
  level = "h2",
  size = "default",
  align = "center",
  flush = false,
  className,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  level?: "h2" | "h3";
  size?: "default" | "sm";
  // 说明较长、换行时让右侧操作顶部对齐
  align?: "center" | "start";
  // 去掉底部间距（标题行后紧跟紧凑内容）
  flush?: boolean;
  className?: string;
}) {
  const Heading = size === "sm" ? "strong" : level;
  return (
    <div
      data-slot="section-header"
      className={cn(
        "flex min-w-0 flex-wrap justify-between gap-x-3 gap-y-2",
        align === "start" ? "items-start" : "items-center",
        flush ? "mb-0" : size === "sm" ? "mb-1" : "mb-4",
        className,
      )}
    >
      <div className="min-w-0 flex-1">
        <Heading
          className={cn(
            "m-0 block min-w-0 wrap-anywhere text-[var(--mg-ink)] [&>svg]:mr-1.5 [&>svg]:inline [&>svg]:align-[-3px]",
            size === "sm" ? "text-[13px] font-semibold" : "text-[14.5px] font-[650] tracking-[-0.015em]",
          )}
        >
          {title}
        </Heading>
        {description && <p className="mt-1 mb-0 text-[12.5px] leading-relaxed text-[var(--mg-ink-3)]">{description}</p>}
      </div>
      {actions && (
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 [&>svg]:text-[var(--mg-ink-engraved)]">{actions}</div>
      )}
    </div>
  );
}

type StateKind = "loading" | "empty" | "error" | "success" | "forbidden";
const stateIcons = { loading: LoaderCircle, empty: Inbox, error: AlertCircle, success: CheckCircle2, forbidden: Lock };

// size=sm：卡片内的行内状态（无结果、无采样），横向排列、不占大块高度
function PageState({
  kind,
  title,
  detail,
  action,
  size = "default",
  className,
}: {
  kind: StateKind;
  title: ReactNode;
  detail?: ReactNode;
  action?: ReactNode;
  size?: "default" | "sm";
  className?: string;
}) {
  const Icon = stateIcons[kind];
  if (size === "sm")
    return (
      <div
        data-slot="page-state"
        data-kind={kind}
        role={kind === "error" || kind === "forbidden" ? "alert" : "status"}
        className={cn("flex items-center gap-2.5 py-4 text-[12.5px] text-[var(--mg-ink-3)]", className)}
      >
        <Icon className={cn("size-4 shrink-0 text-[var(--mg-ink-engraved)]", kind === "loading" && "animate-spin")} />
        <span className="min-w-0">
          {title}
          {detail && <span className="block text-xs">{detail}</span>}
        </span>
        {action}
      </div>
    );
  return (
    <div
      data-slot="page-state"
      data-kind={kind}
      role={kind === "error" || kind === "forbidden" ? "alert" : "status"}
      className={cn("flex min-h-52 flex-col items-center justify-center gap-3 px-6 py-8 text-center", className)}
    >
      <span className="mg-well grid size-12 place-items-center rounded-full text-[var(--mg-ink-engraved)] in-data-[kind=error]:text-[var(--mg-bad)] in-data-[kind=success]:text-[var(--mg-ok)]">
        <Icon className={cn("size-5", kind === "loading" && "animate-spin")} />
      </span>
      <strong className="text-[13.5px] font-semibold text-[var(--mg-ink-2)]">{title}</strong>
      {detail && <p className="m-0 max-w-[48ch] text-xs leading-relaxed text-[var(--mg-ink-3)]">{detail}</p>}
      {action}
    </div>
  );
}

// 分页：bar 作为卡片底栏（上方刻线、与卡片同内边距）；inline 放在卡片内容里，不加边线和水平内边距。
// busy 时禁用翻页并把总数显示为“…”，避免加载中误显示为未知。name 区分同页多个分页器（读屏地标）。
function Pager({
  page,
  pageSize,
  total,
  onPage,
  label = "条",
  busy = false,
  variant = "bar",
  name,
  className,
}: {
  page: number;
  pageSize: number;
  total: number | undefined;
  onPage: (page: number) => void;
  label?: string;
  busy?: boolean;
  variant?: "bar" | "inline";
  name?: string;
  className?: string;
}) {
  const pages = total === undefined ? undefined : Math.max(1, Math.ceil(total / pageSize));
  return (
    <nav
      aria-label={name ? name + "分页" : "分页"}
      className={cn(
        "flex items-center justify-between gap-3 text-xs text-[var(--mg-ink-3)]",
        variant === "bar" ? "border-t border-[var(--mg-line-soft)] px-5 py-3" : "pt-3",
        className,
      )}
    >
      <span className="mg-digits">
        {total === undefined ? (busy ? "共 … " + label : "总数未知") : `共 ${total} ${label}`} · 第 {page}
        {pages ? ` / ${pages}` : ""} 页
      </span>
      <span className="flex gap-1.5">
        <Button variant="outline" size="icon-sm" aria-label="上一页" disabled={busy || page <= 1} onClick={() => onPage(page - 1)}>
          <ChevronLeft />
        </Button>
        <Button
          variant="outline"
          size="icon-sm"
          aria-label="下一页"
          disabled={busy || pages === undefined || page >= pages}
          onClick={() => onPage(page + 1)}
        >
          <ChevronRight />
        </Button>
      </span>
    </nav>
  );
}

function Skeleton({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="skeleton" aria-hidden className={cn("mg-skeleton h-4", className)} {...props} />;
}

function Kbd({ className, ...props }: ComponentProps<"kbd">) {
  return <kbd data-slot="kbd" className={cn("mg-kbd", className)} {...props} />;
}

export { PageHeader, SectionHeader, PageState, Pager, Skeleton, Kbd };
export type { StateKind };
