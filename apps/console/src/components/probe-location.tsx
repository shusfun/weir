import { Field, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import type { Gate } from "../lib/core";
import { State } from "./feedback";

export function ProbeLocation({ value, change, disabled = false }: { value: number; change: (value: number) => void; disabled?: boolean }) {
  const q = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  return <Field label="测试位置" hint="中心反映中心出口体验；指定 VPS 反映该 VPS 的出口体验。节点所在 VPS 的实测不能证明外部可达。">
    <Select value={String(value)} onValueChange={(id) => change(Number(id))} disabled={disabled}>
      <SelectTrigger><SelectValue>{value === 0 ? "中心" : q.data?.find((g) => g.id === value)?.name ?? `VPS ${value}`}</SelectValue></SelectTrigger>
      <SelectContent><SelectItem value="0">中心</SelectItem>{(q.data ?? []).filter((g) => g.status === "active").map((g) => <SelectItem key={g.id} value={String(g.id)} disabled={!g.online}>{g.name}{!g.online ? "（离线）" : ""}</SelectItem>)}</SelectContent>
    </Select>
    {(q.isPending || q.error) && <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />}
  </Field>;
}
