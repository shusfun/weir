// 自更新的进度查询与版本监视。常驻控制台外壳：离开设置页、刷新过页面后，更新完成仍能发现。
import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLocation, useNavigate } from "react-router-dom";
import { Button, useToast, type Job } from "@weir/ui";
import { ArrowUpCircle } from "lucide-react";
import { api } from "../api";

export interface UpdateRun extends Job {
  phase: "prefetch" | "waiting" | "downloading" | "installing" | "done" | "failed" | "cancelled";
  action: "update" | "rollback";
  from: string;
  version: string;
}
export interface UpdateRelease { version: string; published_at: string; notes: string; url: string; unavailable?: string }
export interface UpdateStatus {
  current: string; blocker: string; previous: string; run?: UpdateRun;
  latest: UpdateRelease | null; has_update: boolean; older: UpdateRelease[]; unavailable: UpdateRelease[];
  update_status: "unknown" | "failed" | "stale" | "available" | "unavailable" | "current";
  check: { checked_at: string; succeeded_at: string; next_check_at: string; checking: boolean; stale: boolean; error?: string; trace?: string };
}

export function updateSummary(view?: UpdateStatus, failed = false): string {
  if (failed) return "状态读取失败";
  if (!view) return "未知";
  const labels = { unknown: "未知", failed: "检查失败", stale: "结果已过期", available: "有新版本", unavailable: "新发布不可用", current: "已是最新" };
  return labels[view.update_status];
}

export function UpdateButton() {
  const q = useUpdateStatus();
  const nav = useNavigate();
  const view = q.data;
  if (!q.error && (!view || view.update_status === "current" || view.update_status === "unknown")) return null;
  const label = `${updateSummary(view, !!q.error)}${view?.has_update && view.latest ? ` · ${view.latest.version}` : ""}`;
  return <Button variant="ghost" size="icon-sm" title={label} aria-label={label} onClick={() => nav("/system?setting=update")}><ArrowUpCircle /></Button>;
}

export const updateStatusKey = ["self-update-status"];

// useUpdateStatus：只读中心本机，不访问 GitHub。更新进行中 1.5 秒一次；平时 30 秒一次，发现别的窗口发起的更新。
// 中心重启期间请求会失败：不重试，下一轮再查，页面保留最后的进度。
export function useUpdateStatus() {
  return useQuery({
    queryKey: updateStatusKey,
    queryFn: async () => parseUpdateStatus(await api("/admin/system/update/status")),
    refetchInterval: (query) => (query.state.data?.run?.status === "running" || query.state.data?.check.checking ? 1500 : 30_000),
    retry: false,
  });
}

// UpdateWatcher：以页面加载时的版本为准，中心版本变了就说明控制台已是旧的。在更新卡片所在页直接刷新；
// 在其他页只提示，不打断正在填的表单。
export function UpdateWatcher() {
  const q = useUpdateStatus();
  const toast = useToast();
  const location = useLocation();
  const loaded = useRef("");
  const notified = useRef("");
  const current = q.data?.current ?? "";
  useEffect(() => {
    if (!current) return;
    if (!loaded.current) loaded.current = current;
    if (current === loaded.current || notified.current === current) return;
    notified.current = current;
    const onUpdatePage = location.pathname === "/system" && new URLSearchParams(location.search).get("setting") === "update";
    if (onUpdatePage) {
      toast(`已更新到 ${current}`, { tone: "ok", description: "页面将重新加载以使用新版本的控制台。" });
      setTimeout(() => window.location.reload(), 1500);
    } else {
      toast(`已更新到 ${current}`, { tone: "ok", description: "刷新页面后使用新版本的控制台。" });
    }
  }, [current, location.pathname, location.search, toast]);
  return null;
}

// Validate network input once before sharing it between polling, rows and dialogs.
function parseUpdateStatus(raw: unknown): UpdateStatus {
  const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
  const release = (v: unknown): v is UpdateRelease => object(v) && ["version", "published_at", "notes", "url"].every((k) => typeof v[k] === "string") && (v.unavailable === undefined || typeof v.unavailable === "string");
  const valid = (v: unknown): v is UpdateStatus => {
    if (!object(v) || typeof v.current !== "string" || typeof v.blocker !== "string" || typeof v.previous !== "string" || typeof v.has_update !== "boolean"
      || typeof v.update_status !== "string" || !["unknown", "failed", "stale", "available", "unavailable", "current"].includes(v.update_status)
      || !(v.latest === null || release(v.latest)) || !Array.isArray(v.older) || !v.older.every(release) || !Array.isArray(v.unavailable) || !v.unavailable.every(release)) return false;
    const c = v.check;
    if (!object(c) || !["checked_at", "succeeded_at", "next_check_at"].every((k) => typeof c[k] === "string" && Number.isFinite(Date.parse(c[k])))
      || typeof c.checking !== "boolean" || typeof c.stale !== "boolean" || c.error !== undefined && typeof c.error !== "string") return false;
    const r = v.run;
    if (r === undefined) return true;
    return object(r) && ["id", "kind", "title", "from", "version", "started_at"].every((k) => typeof r[k] === "string")
      && typeof r.status === "string" && ["running", "done", "failed", "cancelled"].includes(r.status)
      && typeof r.phase === "string" && ["prefetch", "waiting", "downloading", "installing", "done", "failed", "cancelled"].includes(r.phase)
      && (r.action === "update" || r.action === "rollback")
      && (r.logs === undefined || Array.isArray(r.logs) && r.logs.every((l) => object(l) && typeof l.text === "string" && typeof l.at === "string"))
      && (r.progress === undefined || object(r.progress) && ["done", "total", "speed"].every((k) => typeof r.progress === "object" && r.progress !== null && k in r.progress && typeof Reflect.get(r.progress, k) === "number") && typeof r.progress.source === "string");
  };
  if (!valid(raw)) throw new Error("版本状态响应无效，请核对中心版本并重试");
  return raw;
}
