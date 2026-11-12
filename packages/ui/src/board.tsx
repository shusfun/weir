// 画板反复出现的形态。页面只传入真实数据；数量、地址、版本缺失显示「未知」，不补示例。
import type { CSSProperties, ReactNode } from "react";
import { Check, X } from "lucide-react";
import { cn } from "cn";
import { CopyButton } from "./copy-button";
import { Sheet, SheetContent } from "./overlay";

type FlowStatus = "complete" | "current" | "upcoming" | "fail" | "unknown";

function FlowBoard({
  label,
  steps,
  className,
}: {
  label: string;
  steps: { label: string; value?: ReactNode | null; caption?: ReactNode; status?: FlowStatus }[];
  className?: string;
}) {
  return (
    <ol data-slot="flow-board" className={cn("weir-flow", className)} aria-label={label} style={{ "--weir-flow-n": String(steps.length) } as CSSProperties}>
      {steps.map((step, index) => {
        const status = step.status ?? "unknown";
        const mark = status === "complete" ? <Check aria-hidden /> : status === "fail" ? <X aria-hidden /> : status === "unknown" ? "?" : index + 1;
        return (
          <li key={step.label} data-status={status}>
            <b>{mark}</b>
            <span className="weir-flow-label">{step.label}</span>
            <span className="weir-flow-value">{step.value == null || step.value === "" ? "未知" : step.value}</span>
            {step.caption && <small>{step.caption}</small>}
          </li>
        );
      })}
    </ol>
  );
}

// 流水线（设计稿 CSources）：每一步一张小卡，写数量和说明，底下一个点代表一个对象（最多 41 个）；数量缺失写未知。
function PipelineBoard({ label, steps, className }: { label: string; steps: { name: string; value: number | null | undefined; note?: ReactNode }[]; className?: string }) {
  return (
    <ol data-slot="pipeline-board" className={cn("weir-pipeline", className)} aria-label={label}>
      {steps.map((step, i) => {
        const known = typeof step.value === "number" && Number.isFinite(step.value);
        const dots = known ? Math.min(41, step.value as number) : 0;
        return (
          <li key={step.name}>
            <small>{i + 1}</small>
            <b>{step.name}</b>
            <strong>{known ? step.value : "未知"}</strong>
            {step.note && <small>{step.note}</small>}
            <span className="weir-pipeline-dots" aria-hidden>
              {Array.from({ length: 41 }, (_, k) => <i key={k} data-on={k < dots ? "" : undefined} />)}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function LaneMap({
  label,
  lanes,
  empty = "还没有启用的分流规则",
  className,
}: {
  label: string;
  lanes: { id: string; from: string; to: string }[];
  empty?: ReactNode;
  className?: string;
}) {
  if (!lanes.length) {
    return <p data-slot="lane-map" className={cn("m-0 text-body text-[var(--weir-ink-3)]", className)}>{empty}</p>;
  }
  const froms = [...new Set(lanes.map((lane) => lane.from))];
  const tos = [...new Set(lanes.map((lane) => lane.to))];
  const rows = Math.max(froms.length, tos.length, 1);
  const height = rows * 36 + 28;
  const y = (index: number, count: number) => 28 + (index + 0.5) * ((height - 28) / count);
  const fromY = new Map(froms.map((name, index) => [name, y(index, froms.length)]));
  const toY = new Map(tos.map((name, index) => [name, y(index, tos.length)]));
  return (
    <div data-slot="lane-map" className={cn("weir-lanes", className)}>
      <svg viewBox={`0 0 640 ${height}`} role="img" aria-label={label}>
        {lanes.map((lane) => {
          const y1 = fromY.get(lane.from) ?? 0;
          const y2 = toY.get(lane.to) ?? 0;
          return <path key={lane.id} d={`M 150 ${y1} C 280 ${y1}, 360 ${y2}, 490 ${y2}`} fill="none" stroke="var(--weir-accent)" strokeWidth="1.5" />;
        })}
        {froms.map((name) => <text key={name} x="8" y={fromY.get(name)} fill="var(--weir-ink-2)" fontSize="13">{name.slice(0, 18)}</text>)}
        {tos.map((name) => <text key={name} x="500" y={toY.get(name)} fill="var(--weir-ink)" fontSize="13">{name.slice(0, 16)}</text>)}
      </svg>
    </div>
  );
}

// 订阅链接票据（设计稿 CLinks）：左边是名称、最近拉取时间、地址、21 天拉取条；右边是撕口存根，放二维码和复制。
// days 是近 21 天每天的拉取结果（从旧到新）；缺失时整排画成未知，不画成没拉过。
function Ticket({
  name,
  meta,
  url,
  qr,
  last,
  tone = "unknown",
  days,
  note = "最近 21 天的拉取",
  actions,
  stub,
  className,
}: {
  name: ReactNode;
  meta?: ReactNode;
  url?: string | null;
  qr?: ReactNode;
  /** 右上角最近一次拉取的时间 */
  last?: ReactNode;
  tone?: "ok" | "warn" | "bad" | "unknown";
  days?: ("ok" | "warn" | "none")[] | null;
  note?: ReactNode;
  actions?: ReactNode;
  /** 存根里二维码下方的按钮（默认复制） */
  stub?: ReactNode;
  className?: string;
}) {
  return (
    <article data-slot="ticket" className={cn("weir-ticket", className)}>
      <div className="weir-ticket-main">
        <div className="flex items-center justify-between gap-[9px]">
          <b className="truncate text-headline">{name}</b>
          <span className="flex shrink-0 items-center gap-[5px] text-fine text-[var(--weir-ink-3)]">
            <span className="weir-lamp" data-size="sm" data-tone={tone} />
            {last ?? "未拉取"}
          </span>
        </div>
        {meta && <div className="mt-[3px] truncate text-fine text-[var(--weir-ink-3)]">{meta}</div>}
        <div className="weir-ticket-url">{url ?? "地址未知"}</div>
        <div className="weir-ticket-days" role="img" aria-label={days ? "最近 21 天的拉取" : "最近 21 天的拉取：未知"}>
          {Array.from({ length: 21 }, (_, k) => <i key={k} data-day={days ? days[k] ?? "none" : "unknown"} />)}
        </div>
        <div className="mt-[5px] text-fine text-[var(--weir-ink-3)]">{note}</div>
        {actions && <div className="weir-ticket-actions">{actions}</div>}
      </div>
      <div className="weir-ticket-stub">
        {qr ?? <span className="text-fine text-[var(--weir-ink-3)]">已停用</span>}
        {stub ?? (url && <CopyButton value={url} size="sm">复制</CopyButton>)}
      </div>
    </article>
  );
}

function PhoneFrame({
  title,
  src,
  fallback,
  device = "phone",
  className,
}: {
  title: string;
  src?: string | null;
  fallback?: ReactNode;
  device?: "phone" | "desktop";
  className?: string;
}) {
  return (
    <div data-slot="phone-frame" data-device={device} className={cn("weir-phone", className)}>
      {src ? <iframe title={title} src={src} /> : <div className="weir-phone-fallback">{fallback ?? <p>还没有可预览的内容</p>}</div>}
    </div>
  );
}

function ChoiceBoard({
  label,
  children,
  className,
}: {
  label: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div data-slot="choice-board" role="group" aria-label={label} className={cn("weir-choice-board", className)}>
      {children}
    </div>
  );
}

function ThemeSwatch({ preview }: { preview: "light" | "dark" | "system" }) {
  return <span className="weir-theme-swatch" data-preview={preview} aria-hidden />;
}

function BulkBar({
  count,
  children,
  className,
}: {
  count: number;
  children?: ReactNode;
  className?: string;
}) {
  if (!count) return null;
  return (
    <div data-slot="bulk-bar" className={cn("weir-bulk", className)} role="status">
      <span>已选 {count} 人</span>
      <div className="weir-ticket-actions">{children}</div>
    </div>
  );
}

function VersionCell({ version, upgrade }: { version?: string | null; /** 可升级到的版本：显示「当前 → 新」并标黄 */ upgrade?: string | null }) {
  const text = version == null ? "未知" : version === "" ? "未使用" : upgrade ? `${version} → ${upgrade}` : version;
  return <span data-slot="version-cell" className="weir-ver" data-empty={version ? undefined : true} data-old={version && upgrade ? true : undefined}>{text}</span>;
}

function TraceSheet({
  open,
  onOpenChange,
  title = "这次为什么断了",
  description,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title?: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" title={title} description={description}>
        {children}
      </SheetContent>
    </Sheet>
  );
}

function DraftRecheck({
  blocked,
  title = "执行前被拦下",
  detail = "复判结果未知，没有执行。",
  action,
  className,
}: {
  blocked: boolean;
  title?: ReactNode;
  detail?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  if (!blocked) return null;
  return (
    <section data-slot="draft-recheck" className={cn("weir-recheck", className)} role="status">
      <strong>{title}</strong>
      <p>{detail}</p>
      {action}
    </section>
  );
}

export { BulkBar, ChoiceBoard, DraftRecheck, FlowBoard, LaneMap, PipelineBoard, PhoneFrame, ThemeSwatch, Ticket, TraceSheet, VersionCell };
