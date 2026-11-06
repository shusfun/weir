// 自更新的进度查询与版本监视。常驻控制台外壳：离开设置页、刷新过页面后，更新完成仍能发现。
import { useEffect, useRef } from "react";
import { useQuery } from "../lib/query";
import { useLocation } from "react-router-dom";
import { useToast, type Job } from "@weir/ui";
import { api } from "../api";

export interface UpdateRun extends Job {
  phase: "prefetch" | "waiting" | "downloading" | "installing" | "done" | "failed" | "cancelled";
  action: "update" | "rollback";
  from: string;
  version: string;
}
export interface UpdateStatus { current: string; blocker: string; previous: string; run?: UpdateRun }

export const updateStatusKey = ["self-update-status"];

// useUpdateStatus：只读中心本机，不访问 GitHub。更新进行中 1.5 秒一次；平时 30 秒一次，发现别的窗口发起的更新。
// 中心重启期间请求会失败：不重试，下一轮再查，页面保留最后的进度。
export function useUpdateStatus() {
  return useQuery({
    queryKey: updateStatusKey,
    queryFn: () => api("/admin/system/update/status") as Promise<UpdateStatus>,
    refetchInterval: (query) => (query.state.data?.run?.status === "running" ? 1500 : 30_000),
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
    const onUpdatePage = location.pathname === "/system" && (new URLSearchParams(location.search).get("tab") ?? "update") === "update";
    if (onUpdatePage) {
      toast(`已更新到 ${current}`, { tone: "ok", description: "页面将重新加载以使用新版本的控制台。" });
      setTimeout(() => window.location.reload(), 1500);
    } else {
      toast(`已更新到 ${current}`, { tone: "ok", description: "刷新页面后使用新版本的控制台。" });
    }
  }, [current, location.pathname, location.search, toast]);
  return null;
}
