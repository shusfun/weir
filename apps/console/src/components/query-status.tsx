// 全局读取状态：中心断线、后台查询失败。浮在右上角、不占页面布局，出现和消失都不挤动侧栏、顶栏与主区。
// 断线用警告色（恢复后自动消失）；后台读取失败用错误色并给一个小的「重试」。
import { useSyncExternalStore } from "react";
import { onlineManager } from "@tanstack/react-query";
import { Button } from "@weir/ui";
import { ApiError, isTransient } from "../api";
import { useQueryFailures, visibleBackgroundFailures } from "../lib/query";

export function QueryStatus() {
  const online = useSyncExternalStore((notify) => onlineManager.subscribe(notify), () => onlineManager.isOnline(), () => true);
  const failures = useQueryFailures();
  const transient = failures.filter((f) => isTransient(f.error));
  const disconnected = transient.length > 0;
  const upstreamOnly = disconnected && transient.every((f) => f.error instanceof ApiError && f.error.code === "upstream_unavailable");
  const background = visibleBackgroundFailures(failures);
  const reasons = [...new Set(background.map((f) => String(f.error)))];
  if (online && !disconnected && background.length === 0) return null;
  const bad = background.length > 0;
  return (
    <div className="query-status" data-tone={bad ? "bad" : "warn"} role={bad ? "alert" : "status"}>
      <span className="query-status-lamp" aria-hidden />
      <div className="min-w-0 flex-1">
        {!online && <p>网络已断开，当前显示上次读取的数据；恢复后自动继续</p>}
        {online && disconnected && <p>{upstreamOnly ? "上游服务暂时不可用，正在重试，恢复后自动继续" : "中心暂时连不上（更新或重启时属正常），恢复后自动继续"}</p>}
        {reasons.map((reason) => <p key={reason} title={reason}>后台读取失败：{reason}</p>)}
      </div>
      {bad && <Button size="sm" variant="ghost" onClick={() => { for (const f of background) void f.query.fetch().catch(() => {}); }}>重试</Button>}
    </div>
  );
}
