// 订阅源用哪台 VPS 拉取：「自动」加全部已接入的 VPS，离线与本机标出来；订阅源表单和默认设置共用。
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import type { Gate } from "../lib/core";

const gateText = (g: Gate) => g.name + (g.colocated ? " · 本机" : "") + (g.online ? "" : " · 离线");

export function SourceGateSelect({ value, onChange, autoLabel = "自动", id }: { value: number; onChange: (id: number) => void; autoLabel?: string; id?: string }) {
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const list = (gates.data ?? []).filter((g) => g.status === "active");
  const current = list.find((g) => g.id === value);
  return <Select value={String(value)} onValueChange={(v) => onChange(Number(v) || 0)}>
    <SelectTrigger id={id} aria-label="拉取用的 VPS"><SelectValue>{value === 0 ? autoLabel : current ? gateText(current) : gates.isPending ? "正在读取 VPS…" : "已不存在的 VPS"}</SelectValue></SelectTrigger>
    <SelectContent>
      <SelectItem value="0">{autoLabel}</SelectItem>
      {list.map((g) => <SelectItem key={g.id} value={String(g.id)}>{gateText(g)}</SelectItem>)}
    </SelectContent>
  </Select>;
}

// 刷新间隔选项：0 跟随全局（或在默认设置里表示不自动刷新）
export const refreshChoices = [1, 3, 6, 12, 24, 48, 168] as const;
export const refreshText = (h: number) => (h >= 24 && h % 24 === 0 ? `每 ${h / 24} 天` : `每 ${h} 小时`);
