// Gate 的程序版本：与中心不同时 Gate 会从中心自更新到同一版本；没跟上时标出来，并写出它上报的自更新失败原因。
import { Badge } from "@weir/ui";
import type { Gate } from "../lib/core";
import { date } from "../lib/models";

export function gateLags(g: Gate) {
  return !!g.version && !!g.center_version && g.version !== g.center_version;
}

export function GateVersion({ gate: g }: { gate: Gate }) {
  if (!g.version) return <Badge variant="soft" tone="neutral">Gate 版本未知</Badge>;
  if (!gateLags(g)) return <Badge variant="soft" tone="neutral" title="与中心版本相同">Gate {g.version}</Badge>;
  return <Badge variant="soft" tone="warn" title={`中心是 ${g.center_version}，Gate 会自更新到同一版本`}>Gate {g.version} · 未跟上中心 {g.center_version}</Badge>;
}

// SelfUpdateError：Gate 自更新失败的原因（Gate 心跳上报；更新成功后消失）。
export function SelfUpdateError({ gate: g }: { gate: Gate }) {
  const su = g.self_update;
  if (!su?.error) return null;
  return <p className="text-sm text-[var(--weir-bad-ink)]">自更新到 {su.version} 失败（{date(su.at)}）：{su.error}</p>;
}
