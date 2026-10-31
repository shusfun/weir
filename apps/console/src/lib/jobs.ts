// 后台任务的读取与等待：事件流连着时由它推送（events.ts 写进缓存），断开时每秒查一次。
// 中心重启后任务的记录不在了（只在内存里）：查到 404 时说明这一点并停止轮询，不一直显示「进行中」。
import { QueryObserver } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { isLive } from "./live";
import { queryClient } from "./query";

export interface Job {
  id: string;
  kind: string;
  title: string;
  status: "running" | "done" | "failed" | "cancelled";
  progress?: { done: number; total: number; speed: number; source: string };
  step?: string;
  step_at?: string;
  step_max?: number;
  cancellable?: boolean;
  logs?: { at: string; text: string }[];
  error?: string;
  result?: Record<string, unknown>;
  started_at: string;
  finished_at?: string;
  trace?: string;
  owner?: number;
}

export const jobGoneText = "中心重启过，这次任务的记录已不在（任务只保存在中心内存里）；操作可能已经完成，请刷新后核对。";

export const isJobGone = (e: unknown) => e instanceof ApiError && e.code === "job_not_found";

export const jobKey = (id: string | null) => ["job", id] as const;

export async function fetchJob(id: string) {
  return (await api("/me/jobs/" + encodeURIComponent(id))).job as Job;
}

// jobRefetch：连着事件流时不轮询（推送会写进缓存），否则运行中每秒一次；任务已不在时停止。
export function jobRefetch(query: { state: { data?: Job; error: unknown } }) {
  if (isJobGone(query.state.error)) return false;
  const running = !query.state.data || query.state.data.status === "running";
  return running && !(isLive() && query.state.data) ? 1000 : false;
}

export const jobRetry = (n: number, e: unknown) => !isJobGone(e) && n < 3;

// 本标签页正在等结果的任务：结束时由等待的人提示，事件流不再另外提示
const awaited = new Set<string>();
export const isAwaited = (id: string) => awaited.has(id);

// awaitJob 等一个后台任务结束；onUpdate 收到每次变化（等待行据此显示步骤）。
export function awaitJob(first: Job, onUpdate?: (j: Job) => void): Promise<Job> {
  awaited.add(first.id);
  const key = jobKey(first.id);
  if (!queryClient.getQueryData(key)) queryClient.setQueryData(key, first);
  const observer = new QueryObserver<Job>(queryClient, { queryKey: key, queryFn: () => fetchJob(first.id), refetchInterval: jobRefetch, retry: jobRetry });
  return new Promise((resolve, reject) => {
    let unsub = () => {};
    const check = () => {
      const r = observer.getCurrentResult();
      if (r.data) onUpdate?.(r.data);
      if (isJobGone(r.error)) {
        unsub();
        reject(new ApiError(jobGoneText, "job_not_found", 404));
      } else if (r.data && r.data.status !== "running") {
        unsub();
        resolve(r.data);
      }
    };
    unsub = observer.subscribe(check);
    check();
  });
}

// putJob 把事件流推来的任务写进缓存：单个任务与各个任务列表。
export function putJob(j: Job) {
  queryClient.setQueryData(jobKey(j.id), j);
  for (const [key, list] of queryClient.getQueriesData<Job[]>({ queryKey: ["jobs"] })) {
    if (!Array.isArray(list)) continue;
    const kind = key[1];
    if (kind !== "all" && kind !== j.kind) continue;
    const summary = { ...j, logs: undefined };
    const i = list.findIndex((x) => x.id === j.id);
    queryClient.setQueryData<Job[]>(key, i >= 0 ? list.map((x) => (x.id === j.id ? summary : x)) : [summary, ...list]);
  }
}
