// Gate 的程序版本：与中心不同时 Gate 自己按「GitHub 加速」的来源下载中心的版本（失败退回从中心下载），
// 失败后 10 分钟内不自动重试；没跟上时标出来，显示它随心跳上报的进度或失败原因，并可手动「更新到 x」。
import { Badge, Button, Progress, transferText } from "@weir/ui";
import { RefreshCw } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import type { Gate } from "../lib/core";
import { date } from "../lib/models";

export function gateLags(g: Gate) {
  return !!g.version && !!g.center_version && g.version !== g.center_version;
}

function legacyReadonlyInstallLock(g: Gate) {
  const error = g.self_update?.error?.toLowerCase() ?? "";
  const lock = error.includes("install.lock") || error.includes("install lock") || error.includes("安装锁");
  const readonly = error.includes("read-only") || error.includes("readonly") || error.includes("只读");
  return g.version === "0.1.7" && lock && readonly;
}

export function GateVersion({ gate: g }: { gate: Gate }) {
  if (!g.version) return <Badge variant="soft" tone="neutral">Gate 版本未知</Badge>;
  if (!gateLags(g)) return <Badge variant="soft" tone="neutral" title="与中心版本相同">Gate {g.version}</Badge>;
  return <Badge variant="soft" tone="warn" title={`中心是 ${g.center_version}，Gate 会自更新到同一版本`}>Gate {g.version} · 未跟上中心 {g.center_version}</Badge>;
}

// updateText：自更新这一行写什么。tone 决定颜色；进度百分比在总大小未知时改写已下载多少
function updateText(g: Gate): [string, "bad" | "muted"] | null {
  const su = g.self_update;
  if (legacyReadonlyInstallLock(g)) return ["旧版 Gate 的安装锁位于只读文件系统，面板重试无法修复；需从主机面板运行官方安装器完成迁移。", "bad"];
  if (su?.state === "downloading") {
    return [`正在下载 ${su.version} · ${transferText({ done: su.done ?? 0, total: su.total, speed: su.speed, source: su.source })}`, "muted"];
  }
  if (su?.state === "replacing") return [`正在替换为 ${su.version}`, "muted"];
  if (su?.error) return [`自更新到 ${su.version} 失败（${date(su.at)}）：${su.error}`, "bad"];
  if (gateLags(g)) return [g.online ? `会自动更新到中心的 ${g.center_version}` : `离线，上线后会自动更新到 ${g.center_version}`, "muted"];
  return null;
}

// GateUpdateLine：VPS 卡片与详情里的自更新行。没跟上中心时右侧是「更新到 x」，失败后不必等 10 分钟。
export function GateUpdateLine({ gate: g }: { gate: Gate }) {
  const { run, isBusy } = useRowAction<string>({ invalidate: [["gates"]] });
  const text = updateText(g);
  if (!text) return null;
  const working = g.self_update?.state === "downloading" || g.self_update?.state === "replacing";
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      {working && <span className="w-24 shrink-0"><Progress value={g.self_update?.state === "downloading" && g.self_update.total ? Math.round(((g.self_update.done ?? 0) / g.self_update.total) * 100) : null} aria-label="VPS 自更新进度" /></span>}
      <p className={"min-w-0 grow basis-0 text-body " + (text[1] === "bad" ? "text-[var(--weir-bad-ink)]" : "text-[var(--weir-ink-3)]")}>{text[0]}</p>
      {gateLags(g) && !legacyReadonlyInstallLock(g) && (
        <Button
          size="xs"
          variant="outline"
          disabled={!g.online || working || isBusy()}
          busy={isBusy("update")}
          busyText="正在通知…"
          title={g.online ? undefined : "VPS 离线，上线后会自动更新"}
          onClick={() => void run("update", () => api(`/admin/gates/${g.id}/update`, "POST"), `已通知「${g.name}」更新到 ${g.center_version}`)}
        >
          <RefreshCw />更新到 {g.center_version}
        </Button>
      )}
    </div>
  );
}
