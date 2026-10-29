// 后台任务（内核 / dsh 下载等）：进度、速度、当前线路、逐条日志与取消。关掉弹窗任务照常进行，回来能接着看。
import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogTitle, Progress } from "@weir/ui";
import { X } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { clock } from "../lib/models";

export interface Job {
  id: string;
  kind: string;
  title: string;
  status: "running" | "done" | "failed" | "cancelled";
  progress?: { done: number; total: number; speed: number; source: string };
  step?: string;
  logs?: { at: string; text: string }[];
  error?: string;
  result?: Record<string, unknown>;
  started_at: string;
  finished_at?: string;
}

export function size(n: number) {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(0) + " KB";
  return n + " B";
}

// useJobs 列出某类任务；有任务在跑时每秒刷新，否则 15 秒一次。
export function useJobs(kind: string) {
  const q = useQuery({
    queryKey: ["jobs", kind],
    queryFn: async () => (await api("/admin/jobs?kind=" + encodeURIComponent(kind))).jobs as Job[],
    refetchInterval: (query) => ((query.state.data ?? []).some((j: Job) => j.status === "running") ? 1000 : 15000),
  });
  return q;
}

// 任务结束后要刷新的数据（按任务类型）。
const refreshAfter: Record<string, string[][]> = {
  kernel: [["kernels"], ["notices"], ["drivers"], ["kernel-releases"]],
  "gate-release": [["gates"]],
  dsh: [["dsh"], ["notices"]],
};

// JobWatcher 在控制台里常驻：任何任务结束（或同一 id 的任务再次结束）时刷新相关数据，
// 不依赖下载窗口是否还开着、是不是它看到了状态变化。
export function JobWatcher() {
  const client = useQueryClient();
  const seen = useRef<Map<string, string> | null>(null);
  const q = useQuery({
    queryKey: ["jobs", "all"],
    queryFn: async () => (await api("/admin/jobs")).jobs as Job[],
    refetchInterval: (query) => ((query.state.data ?? []).some((j: Job) => j.status === "running") ? 1500 : 10000),
  });
  useEffect(() => {
    if (!q.data) return;
    const first = seen.current === null;
    const prev = seen.current ?? new Map<string, string>();
    const next = new Map<string, string>();
    for (const j of q.data) {
      const sig = j.status + "@" + (j.finished_at ?? "");
      next.set(j.id, sig);
      if (!first && j.status !== "running" && prev.get(j.id) !== sig) {
        for (const key of refreshAfter[j.kind] ?? []) void client.invalidateQueries({ queryKey: key });
        void client.invalidateQueries({ queryKey: ["job", j.id] });
      }
    }
    seen.current = next;
  }, [q.data, client]);
  return null;
}

// useStartedJob 返回「任务已开始」的通知函数：同一版本再次下载会复用任务 id，要让已显示的任务重新开始轮询。
export function useStartedJob() {
  const client = useQueryClient();
  return (id: string) => {
    void client.invalidateQueries({ queryKey: ["job", id] });
    void client.invalidateQueries({ queryKey: ["jobs"] });
    return id;
  };
}

// useJob 跟踪一个任务；结束时调用 onFinish（如刷新已下载的版本）。
export function useJob(id: string | null, onFinish?: (j: Job) => void) {
  const q = useQuery({
    queryKey: ["job", id],
    enabled: !!id,
    queryFn: async () => (await api("/admin/jobs/" + encodeURIComponent(id!))).job as Job,
    refetchInterval: (query) => (query.state.data?.status === "running" || !query.state.data ? 1000 : false),
  });
  const status = q.data?.status;
  useEffect(() => {
    if (q.data && status !== "running") onFinish?.(q.data);
    // 只在状态变化时通知一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);
  return q;
}

const tones = { running: "neutral", done: "ok", failed: "bad", cancelled: "warn" } as const;
const labels = { running: "进行中", done: "完成", failed: "失败", cancelled: "已取消" } as const;

// 任务的显示都是固定行数：状态行、进度条、一行说明、日志始终在，进度、结果与失败原因只换文字不增减行，
// 任务从开始到结束，下面的内容不会被顶开。失败原因过长时这一行截断，全文在日志最后一行。
function progressOf(job: Job) {
  const p = job.progress;
  if (job.status === "done") return 100;
  return p && p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : null;
}

function statusLine(job: Job) {
  const p = job.progress;
  if (job.status === "running") {
    if (!p) return job.step || "正在准备…";
    const pct = progressOf(job);
    return `${pct !== null ? `${pct}% · ${size(p.done)} / ${size(p.total)}` : `已下载 ${size(p.done)}`}${p.speed > 0 ? ` · ${size(p.speed)}/s` : ""}${p.source ? ` · ${p.source}` : ""}`;
  }
  if (job.error) return job.error;
  return job.finished_at ? `${clock(job.finished_at)} 完成` : "完成";
}

function logText(job: Job) {
  const lines = (job.logs ?? []).map((l) => `${clock(l.at)} ${l.text}`);
  if (job.error && job.status !== "running") lines.push(`${job.finished_at ? clock(job.finished_at) : ""} ${job.status === "cancelled" ? "已取消" : "失败"}：${job.error}`.trim());
  return lines.join("\n") || "（还没有日志）";
}

// JobView 显示一个任务：进度条（总大小未知时只显示已下载）、速度、线路、日志、取消。
export function JobView({ job }: { job: Job }) {
  const client = useQueryClient();
  const { run, isBusy } = useRowAction<string>({ refetch: () => void client.invalidateQueries({ queryKey: ["job", job.id] }) });
  const line = statusLine(job);
  return (
    <div className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" aria-live="polite">
      <div className="flex min-h-6 items-center gap-2">
        <strong className="shrink-0 text-[13px]">{job.title}</strong>
        <Badge variant="soft" tone={tones[job.status]}>{labels[job.status]}</Badge>
        <small className="min-w-0 flex-1 truncate text-[var(--mg-ink-3)]">{job.status === "running" ? job.step : ""}</small>
        <Button className={job.status === "running" ? undefined : "invisible"} size="xs" variant="ghost" disabled={isBusy() || job.status !== "running"} onClick={() => void run("cancel", () => api("/admin/jobs/" + encodeURIComponent(job.id) + "/cancel", "POST"), "已取消，已下载的部分保留")}><X />取消</Button>
      </div>
      <Progress value={progressOf(job)} aria-label={job.title + " 进度"} />
      <small className={"block truncate tabular-nums " + (job.status === "failed" ? "text-[var(--mg-bad-ink)]" : "text-[var(--mg-ink-3)]")} title={line}>{line}</small>
      <Collapsible title={`日志（${job.logs?.length ?? 0} 行）`} defaultOpen={job.status === "failed"}>
        <CodeBlock className="block h-56 overflow-auto whitespace-pre-wrap text-xs">{logText(job)}</CodeBlock>
      </Collapsible>
    </div>
  );
}

// JobByID 读取并显示一个任务（列表里只有摘要，这里取带日志的详情）；详情还没回来时先用摘要占位，不出现空白。
export function JobByID({ id, initial, onFinish }: { id: string; initial?: Job; onFinish?: (j: Job) => void }) {
  const q = useJob(id, onFinish);
  const job = q.data ?? initial;
  if (!job) return <JobLinePlaceholder text="正在读取任务…" />;
  return <JobView job={job} />;
}

function JobLinePlaceholder({ text }: { text: string }) {
  return (
    <div className="flex min-h-9 items-center gap-2 rounded-[var(--mg-radius)] border border-dashed border-[var(--mg-line)] px-3 text-[12.5px] text-[var(--mg-ink-3)]">{text}</div>
  );
}

// JobLine：卡片与下载窗口里固定占一行的任务位。没有任务时显示 idle；有任务时显示状态、进度与「详情」，
// 详情在二级弹窗里看日志、取消。行高不随任务开始、结束变化。
export function JobLine({ job, id, idle, onFinish }: { job?: Job; id?: string | null; idle: string; onFinish?: (j: Job) => void }) {
  const [open, setOpen] = useState(false);
  const live = useJob(id ?? job?.id ?? null, onFinish);
  const j = live.data ?? job;
  if (!j) return <JobLinePlaceholder text={idle} />;
  const pct = progressOf(j);
  const line = statusLine(j);
  return (
    <div className="flex min-h-9 items-center gap-2 rounded-[var(--mg-radius)] border border-[var(--mg-line)] px-3 py-1" aria-live="polite">
      <Badge variant="soft" tone={tones[j.status]}>{labels[j.status]}</Badge>
      <strong className="shrink-0 text-[12.5px]">{j.title}</strong>
      <span className="hidden w-24 shrink-0 sm:block"><Progress value={pct} aria-label={j.title + " 进度"} /></span>
      <small className={"min-w-0 flex-1 truncate tabular-nums " + (j.status === "failed" ? "text-[var(--mg-bad-ink)]" : "text-[var(--mg-ink-3)]")} title={line}>{line}</small>
      <Button size="xs" variant="ghost" onClick={() => setOpen(true)}>详情</Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent size="lg">
          <DialogTitle>{j.title}</DialogTitle>
          <DialogDescription>关掉这个窗口任务照常进行；取消后已下载的部分保留，再次下载时续传。</DialogDescription>
          <JobByID id={j.id} initial={j} />
        </DialogContent>
      </Dialog>
    </div>
  );
}

// latestJob：某类任务里最近的一个（正在进行的优先），只看 30 分钟内开始的。卡片上的任务位一直显示它，
// 不在任务结束后定时消失，避免页面无故跳动。
export function latestJob(jobs: Job[] | undefined, prefix: string) {
  const recent = (jobs ?? []).filter((j) => j.id.startsWith(prefix) && (j.status === "running" || Date.now() - Date.parse(j.started_at) < 30 * 60_000));
  return recent.find((j) => j.status === "running") ?? recent.sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))[0];
}
