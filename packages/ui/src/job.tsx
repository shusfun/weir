// 后台任务的展示：任务位（JobLine，固定占一行）与任务详情（JobView：状态、进度、一行说明、日志、取消）。
// 只管显示：任务数据、轮询或推送、取消请求都由调用方经 props 传入，组件库不依赖数据层与路由。
// 显示是固定行数：进度、结果与失败原因只换文字不增减行，任务从开始到结束，下面的内容不会被顶开。
import { useState, type ReactNode } from "react";
import { X } from "lucide-react";
import { cn } from "cn";
import { Badge } from "./badge";
import { Button } from "./button";
import { Collapsible } from "./collapsible";
import { CodeBlock } from "./display";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./dialog";
import { Progress } from "./progress";

type JobStatus = "running" | "done" | "failed" | "cancelled";

interface Job {
  id: string;
  kind: string;
  title: string;
  status: JobStatus;
  // 下载类任务的字节进度；total 为 0 表示总大小未知
  progress?: { done: number; total: number; speed: number; source: string };
  // 计数类任务的进度（如批量导入第 3 / 10 个）；与 progress 二选一
  count?: { done: number; total: number };
  step?: string;
  step_at?: string;
  step_max?: number;
  cancellable?: boolean;
  logs?: { at: string; text: string }[];
  error?: string;
  result?: Record<string, unknown>;
  // 进行中的结构化结果（如测速已完成的来源）；结束时仍保留
  output?: Record<string, unknown>;
  started_at: string;
  finished_at?: string;
  trace?: string;
  owner?: number;
}

const jobTones = { running: "neutral", done: "ok", failed: "bad", cancelled: "warn" } as const;
const jobLabels = { running: "进行中", done: "完成", failed: "失败", cancelled: "已取消" } as const;

function formatSize(n: number) {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(0) + " KB";
  return n + " B";
}

function clockOf(value: string) {
  const d = new Date(value);
  return Number.isNaN(+d) ? value : d.toLocaleTimeString("zh-CN", { hour12: false });
}

function jobPercent(job: Job) {
  if (job.status === "done") return 100;
  const p = job.count ?? job.progress;
  return p && p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : null;
}

// note：进行中时写在说明末尾的补充（如「实时推送不可用，每秒刷新」）
function jobStatusLine(job: Job, note?: string) {
  if (job.status === "running") {
    const tail = note ? " · " + note : "";
    if (job.count) return `${job.step ? job.step + " · " : ""}${job.count.done} / ${job.count.total}${tail}`;
    const p = job.progress;
    if (!p) return (job.step || "正在准备…") + tail;
    const pct = jobPercent(job);
    return `${pct !== null ? `${pct}% · ${formatSize(p.done)} / ${formatSize(p.total)}` : `已下载 ${formatSize(p.done)}`}${p.speed > 0 ? ` · ${formatSize(p.speed)}/s` : ""}${p.source ? ` · ${p.source}` : ""}${tail}`;
  }
  if (job.error) return job.error;
  return job.finished_at ? `${clockOf(job.finished_at)} 完成` : "完成";
}

function jobLogText(job: Job) {
  const lines = (job.logs ?? []).map((l) => `${clockOf(l.at)} ${l.text}`);
  if (job.error && job.status !== "running")
    lines.push(`${job.finished_at ? clockOf(job.finished_at) : ""} ${job.status === "cancelled" ? "已取消" : "失败"}：${job.error}`.trim());
  return lines.join("\n") || "（还没有日志）";
}

// JobView 显示一个任务。onCancel 不传时不显示取消键；cancelBusy / disabled 由调用方的请求状态给出。
// openLogs：一开始就展开日志（主要看输出的任务）。
function JobView({
  job,
  note,
  openLogs,
  onCancel,
  cancelBusy,
  disabled,
  className,
}: {
  job: Job;
  note?: string;
  openLogs?: boolean;
  onCancel?: () => void;
  cancelBusy?: boolean;
  disabled?: boolean;
  className?: string;
}) {
  const line = jobStatusLine(job, note);
  // 已经开始写入的操作不能取消：键位保留（不跳动），只是不可点
  const cancellable = job.status === "running" && job.cancellable !== false;
  return (
    <div className={cn("grid min-w-0 gap-4 rounded-[var(--weir-radius-card)] [&>*]:min-w-0 border border-[var(--weir-line-soft)] p-3", className)} aria-live="polite">
      <div className="flex min-h-6 items-center gap-2">
        <strong className="shrink-0 text-body">{job.title}</strong>
        <Badge variant="soft" tone={jobTones[job.status]}>{jobLabels[job.status]}</Badge>
        <small className="min-w-0 flex-1 truncate text-[var(--weir-ink-3)]">{job.status === "running" ? job.step : ""}</small>
        {onCancel && (
          <Button
            className={job.status === "running" ? undefined : "invisible"}
            size="xs"
            variant="ghost"
            title={job.status === "running" && !cancellable ? "已经开始写入，不能再取消" : undefined}
            disabled={disabled || !cancellable}
            busy={cancelBusy}
            busyText="正在取消…"
            onClick={onCancel}
          >
            <X />
            取消
          </Button>
        )}
      </div>
      <Progress value={jobPercent(job)} aria-label={job.title + " 进度"} />
      <small className={cn("block truncate tabular-nums", job.status === "failed" ? "text-[var(--weir-bad-ink)]" : "text-[var(--weir-ink-3)]")} title={line}>
        {line}
      </small>
      <Collapsible title={`日志（${job.logs?.length ?? 0} 行）`} defaultOpen={openLogs || job.status === "failed"}>
        <CodeBlock className="block h-56 overflow-auto overscroll-contain whitespace-pre-wrap">{jobLogText(job)}</CodeBlock>
      </Collapsible>
    </div>
  );
}

// JobPlaceholder：任务位上还没有任务、正在读取或任务已不在时的一行浅色文字，不画框，高度与 JobLine 相同。
function JobPlaceholder({ text, tone, className }: { text: ReactNode; tone?: "bad"; className?: string }) {
  return (
    <div
      className={cn(
        "flex min-h-9 items-center gap-2 text-meta touch:min-h-10",
        tone === "bad" ? "text-[var(--weir-bad-ink)]" : "text-[var(--weir-ink-3)]",
        className,
      )}
    >
      {text}
    </div>
  );
}

// JobLine：卡片与弹窗里固定占一行的任务位。没有任务时显示 idle；有任务时显示状态、进度与「详情」，
// 详情在二级弹窗里显示 detail（不传时用 JobView 显示同一个任务）。行高不随任务开始、结束变化。
function JobLine({
  job,
  idle,
  note,
  detail,
  detailDescription = "关掉这个窗口任务照常进行。",
  className,
}: {
  job?: Job | null;
  idle: ReactNode;
  note?: string;
  detail?: ReactNode;
  detailDescription?: ReactNode;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  if (!job) return <JobPlaceholder text={idle} className={className} />;
  const line = jobStatusLine(job, note);
  return (
    <div className={cn("flex min-h-9 min-w-0 items-center gap-2 py-1 touch:min-h-10", className)} aria-live="polite">
      <Badge variant="soft" tone={jobTones[job.status]}>{jobLabels[job.status]}</Badge>
      <strong className="shrink-0 text-meta">{job.title}</strong>
      <span className="hidden w-24 shrink-0 sm:block">
        <Progress value={jobPercent(job)} aria-label={job.title + " 进度"} />
      </span>
      <small className={cn("min-w-0 flex-1 truncate tabular-nums", job.status === "failed" ? "text-[var(--weir-bad-ink)]" : "text-[var(--weir-ink-3)]")} title={line}>
        {line}
      </small>
      <Button size="xs" variant="ghost" onClick={() => setOpen(true)}>
        详情
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent size="lg">
          <DialogTitle>{job.title}</DialogTitle>
          {detailDescription && <DialogDescription>{detailDescription}</DialogDescription>}
          {detail ?? <JobView job={job} note={note} />}
        </DialogContent>
      </Dialog>
    </div>
  );
}

export { JobView, JobLine, JobPlaceholder, formatSize, jobPercent, jobStatusLine, jobLogText, jobTones, jobLabels };
export type { Job, JobStatus };
