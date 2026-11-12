// 后台任务（内核 / dsh 下载、SSH 安装、可能超过反代时限的操作）：进度、速度、当前线路、逐条日志与取消。
// 关掉弹窗任务照常进行，回来能接着看。事件流连着时由中心推送，断开时每秒查一次并写明。
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { formatSize, JobLine as UIJobLine, JobPlaceholder, JobView as UIJobView, useToast } from "@weir/ui";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { startEvents } from "../lib/events";
import { fetchJob, isJobGone, jobGoneText, jobKey, jobRefetch, jobRetry, type Job } from "../lib/jobs";
import { isLive, pollingNote, useLive } from "../lib/live";

export type { Job };

export const size = formatSize;

// listRefetch：事件流连着时任务的变化由它推送，只留 60 秒一次的核对；断开时有任务在跑每秒一次，否则 15 秒。
function listRefetch(fast: number, slow: number) {
  return (query: { state: { data?: Job[] } }) => (isLive() ? 60_000 : (query.state.data ?? []).some((j) => j.status === "running") ? fast : slow);
}

// useJobs 列出某类任务（管理员）。
export function useJobs(kind: string) {
  const q = useQuery({
    queryKey: ["jobs", kind],
    queryFn: async () => (await api("/admin/jobs?kind=" + encodeURIComponent(kind))).jobs as Job[],
    refetchInterval: listRefetch(1000, 15000),
  });
  return q;
}

// 任务结束后要刷新的数据（按任务类型）。
const refreshAfter: Record<string, string[][]> = {
  kernel: [["kernels"], ["notices"], ["drivers"], ["kernel-releases"], ["kernel-recommended"]],
  "gate-release": [["gates"]],
  "gate-ssh": [["gates"], ["nodes"]],
  dsh: [["dsh"], ["notices"]],
  laya: [["laya-hosting"], ["observe-system-health"]],
  "observation-analysis": [["observe-analyses"]],
  // 转成后台任务的操作由发起它的页面刷新；页面已经关掉时由事件流的提示整体刷新（events.ts）
  action: [],
};

// JobWatcher 在控制台里常驻：任何任务结束（或同一 id 的任务再次结束）时刷新相关数据，
// 不依赖下载窗口是否还开着、是不是它看到了状态变化。
export function JobWatcher() {
  const client = useQueryClient();
  const seen = useRef<Map<string, string> | null>(null);
  const q = useQuery({
    queryKey: ["jobs", "all"],
    queryFn: async () => (await api("/admin/jobs")).jobs as Job[],
    refetchInterval: listRefetch(1500, 10000),
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

// ConsoleEvents 在登录后的控制台里常驻：连上事件流（管理员与普通用户都连）。自己发起、但已经没有页面在等的操作
// （关掉了窗口、刷新过页面）结束时，在这里提示结果并刷新数据。
export function ConsoleEvents() {
  const toast = useToast();
  const client = useQueryClient();
  const ref = useRef({ toast, client });
  ref.current = { toast, client };
  useEffect(
    () =>
      startEvents((j) => {
        const { toast, client } = ref.current;
        const res = (j.result ?? {}) as { status?: number; body?: { error?: { detail?: string; code?: string } } };
        if (j.status === "done" && (res.status ?? 0) < 400) toast(j.title + "：已完成", { tone: "ok" });
        else if (j.status === "cancelled") toast(j.title + "：已取消", { description: j.error, tone: "warn" });
        else toast(j.title + "：没有完成", { description: res.body?.error?.detail || j.error || res.body?.error?.code, tone: "bad" });
        void client.invalidateQueries();
      }),
    [],
  );
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

// useJob 跟踪一个任务；结束时调用 onFinish（如刷新已下载的版本）。查到 404（中心重启过）时停止，gone 为真。
export function useJob(id: string | null, onFinish?: (j: Job) => void) {
  const q = useQuery({
    queryKey: jobKey(id),
    enabled: !!id,
    queryFn: () => fetchJob(id!),
    refetchInterval: jobRefetch,
    retry: jobRetry,
  });
  const status = q.data?.status;
  useEffect(() => {
    if (q.data && status !== "running") onFinish?.(q.data);
    // 只在状态变化时通知一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);
  return { ...q, gone: isJobGone(q.error) };
}

// JobView 显示一个任务（展示在 @weir/ui）：这里接上事件流状态与取消请求。
// openLogs：一开始就展开日志（SSH 安装这类主要看输出的任务）。
export function JobView({ job, openLogs }: { job: Job; openLogs?: boolean }) {
  const client = useQueryClient();
  const live = useLive();
  const { run, isBusy } = useRowAction<string>({ refetch: () => void client.invalidateQueries({ queryKey: ["job", job.id] }) });
  return (
    <UIJobView
      job={job}
      note={live ? undefined : pollingNote}
      openLogs={openLogs}
      disabled={isBusy()}
      cancelBusy={isBusy("cancel")}
      onCancel={() => void run("cancel", () => api("/me/jobs/" + encodeURIComponent(job.id) + "/cancel", "POST"), "已请求取消")}
    />
  );
}

// JobByID 读取并显示一个任务（列表里只有摘要，这里取带日志的详情）；详情还没回来时先用摘要占位，不出现空白。
export function JobByID({ id, initial, onFinish, openLogs }: { id: string; initial?: Job; onFinish?: (j: Job) => void; openLogs?: boolean }) {
  const q = useJob(id, onFinish);
  if (q.gone) return <JobPlaceholder text={jobGoneText} tone="bad" />;
  const job = q.data ?? initial;
  if (!job) return <JobPlaceholder text={q.error ? "读取任务失败：" + (q.error as Error).message : "正在读取任务…"} tone={q.error ? "bad" : undefined} />;
  return <JobView job={job} openLogs={openLogs} />;
}

// JobLine：卡片与下载窗口里固定占一行的任务位（展示在 @weir/ui）。这里跟踪任务，详情窗读取带日志的详情。
export function JobLine({ job, id, idle, onFinish, detailDescription }: { job?: Job; id?: string | null; idle: string; onFinish?: (j: Job) => void; detailDescription?: string }) {
  const tracked = useJob(id ?? job?.id ?? null, onFinish);
  const streaming = useLive();
  if (tracked.gone) return <JobPlaceholder text={jobGoneText} tone="bad" />;
  const j = tracked.data ?? job;
  return (
    <UIJobLine
      job={j}
      idle={idle}
      note={streaming ? undefined : pollingNote}
      detailDescription={detailDescription ?? "关掉这个窗口任务照常进行；取消后已下载的部分保留，再次下载时续传。"}
      detail={j && <JobByID id={j.id} initial={j} />}
    />
  );
}

// latestJob：某类任务里最近的一个（正在进行的优先），只看 30 分钟内开始的。卡片上的任务位一直显示它，
// 不在任务结束后定时消失，避免页面无故跳动。
export function latestJob(jobs: Job[] | undefined, prefix: string) {
  const recent = (jobs ?? []).filter((j) => j.id.startsWith(prefix) && (j.status === "running" || Date.now() - Date.parse(j.started_at) < 30 * 60_000));
  return recent.find((j) => j.status === "running") ?? recent.sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))[0];
}
