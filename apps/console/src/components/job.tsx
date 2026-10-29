// 后台任务（内核 / dsh 下载等）：进度、速度、当前线路、逐条日志与取消。关掉弹窗任务照常进行，回来能接着看。
import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Collapsible, Notice, Progress } from "@weir/ui";
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

// JobView 显示一个任务：进度条（总大小未知时只显示已下载）、速度、线路、日志、取消。
export function JobView({ job, compact }: { job: Job; compact?: boolean }) {
  const client = useQueryClient();
  const { run, isBusy } = useRowAction<string>({ refetch: () => void client.invalidateQueries({ queryKey: ["job", job.id] }) });
  const p = job.progress;
  const pct = p && p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : null;
  return (
    <div className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" aria-live="polite">
      <div className="flex flex-wrap items-center gap-2">
        <strong className="text-[13px]">{job.title}</strong>
        <Badge variant="soft" tone={tones[job.status]}>{labels[job.status]}</Badge>
        {job.step && job.status === "running" && <small className="text-[var(--mg-ink-3)]">{job.step}</small>}
        {job.status === "running" && (
          <Button className="ml-auto" size="xs" variant="ghost" disabled={isBusy()} onClick={() => void run("cancel", () => api("/admin/jobs/" + encodeURIComponent(job.id) + "/cancel", "POST"), "已取消，已下载的部分保留")}><X />取消</Button>
        )}
      </div>
      {job.status === "running" && p && (
        <>
          <Progress value={pct} aria-label={job.title + " 下载进度"} />
          <small className="text-[var(--mg-ink-3)]">
            {pct !== null ? `${pct}% · ${size(p.done)} / ${size(p.total)}` : `已下载 ${size(p.done)}`}
            {p.speed > 0 ? ` · ${size(p.speed)}/s` : ""}
            {p.source ? ` · ${p.source}` : ""}
          </small>
        </>
      )}
      {job.error && <Notice tone={job.status === "cancelled" ? "warn" : "bad"}>{job.error}</Notice>}
      {!compact && job.logs && job.logs.length > 0 && (
        <Collapsible title={`日志（${job.logs.length} 行）`} defaultOpen={job.status === "failed"}>
          <CodeBlock className="block max-h-56 overflow-auto whitespace-pre-wrap text-xs">{job.logs.map((l) => `${clock(l.at)} ${l.text}`).join("\n")}</CodeBlock>
        </Collapsible>
      )}
    </div>
  );
}

// JobByID 读取并显示一个任务（列表里只有摘要，这里取带日志的详情）。
export function JobByID({ id, onFinish }: { id: string; onFinish?: (j: Job) => void }) {
  const q = useJob(id, onFinish);
  if (!q.data) return null;
  return <JobView job={q.data} />;
}
