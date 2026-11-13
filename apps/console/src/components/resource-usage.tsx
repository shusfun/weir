import { Button, Card, CardContent, SectionHeader, Sheet, SheetContent } from "@weir/ui";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { api } from "../api";
import type { GateHost } from "../lib/core";
import { bytes, date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import { State } from "./feedback";
import { HistoryCharts } from "./history-charts";
import { ObjectLink, dataObject } from "./object-link";
import { LocalGateControl } from "./local-gate";

const percent = (v?: number) => v == null ? "未知" : `${v.toFixed(1)}%`;
const size = (v?: number) => v == null ? "未知" : bytes(v);
export const freshHost = (host?: GateHost | null, online = true): GateHost | undefined => {
  const age = host?.observed_at ? Date.now() - Date.parse(host.observed_at) : Infinity;
  return online && age >= -60_000 && age <= 90_000 ? host ?? undefined : undefined;
};

export function ProcessUsage({ host, names, online = true }: { host?: GateHost | null; names: readonly string[]; online?: boolean }) {
  const h = freshHost(host, online);
  return <dl className="m-0 grid gap-2 text-body sm:grid-cols-2" aria-label="进程占用">
    {names.map(name => {
      const raw = h?.processes?.[name];
      const age = raw?.observed_at ? Date.now() - Date.parse(raw.observed_at) : Infinity;
      const p = age >= -60_000 && age <= 90_000 ? raw : undefined;
      return <div key={name} className="min-w-0">
        <dt className="text-meta text-[var(--weir-ink-3)]">{name === "weir-center" ? "Weir 中心进程" : name}</dt>
        <dd className="m-0 tabular-nums">{p?.state === "stopped" ? "未运行" : <>CPU {percent(p?.cpu_percent)} · 内存 {size(p?.memory_rss)}{p?.memory_rss != null && h?.mem_total ? `（${percent(p.memory_rss / h.mem_total * 100)}）` : ""}</>}</dd>
        <small className="block min-h-4 text-[var(--weir-ink-3)]">{p?.error || (!p ? "观测未知" : p.state === "running" && p.cpu_percent == null ? "CPU 等待下一次有效采样" : "")}</small>
      </div>;
    })}
  </dl>;
}

function HostUsage({ host }: { host?: GateHost | null }) {
  const h = freshHost(host);
  return <dl className="m-0 grid grid-cols-1 gap-3 text-body tabular-nums sm:grid-cols-3">
    {[["CPU", percent(h?.cpu_percent)], ["内存", `${size(h?.mem_used)} / ${size(h?.mem_total)}`], ["数据盘", `${size(h?.disk_used)} / ${size(h?.disk_total)}`]].map(([k,v]) => <div key={k}><dt className="text-meta text-[var(--weir-ink-3)]">{k}</dt><dd className="m-0 font-medium">{v}</dd></div>)}
  </dl>;
}

export function CenterResources() {
  const q = useQuery({ queryKey: ["center-resources"], queryFn: async () => (await api("/admin/center/resources")) as { host?: GateHost }, refetchInterval: 10_000 });
  const [params, setParams] = useSearchParams();
  const open = params.get("center") === "1";
  const setOpen = (on: boolean) => setParams(p => { if (on) p.set("center", "1"); else p.delete("center"); return p; }, { replace: !on });
  useConsoleContext(open ? { page: "VPS 与节点", object: { type: "center", id: 1, name: "中心" }, dialog: { title: "中心运行状态" } } : null);
  return <>
    <Card {...dataObject({type: "center", id: 1})}><CardContent className="stack min-h-48">
      <SectionHeader title={<ObjectLink to={{type: "center", id: 1, name: "中心"}} />} description="整机与 Weir 进程占用" actions={<Button size="sm" variant="outline" onClick={() => setOpen(true)}>详情</Button>} />
      <LocalGateControl />
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <>
        <HostUsage host={q.data.host} /><ProcessUsage host={q.data.host} names={["weir-center"]} />
        <small className="text-[var(--weir-ink-3)]">采样 {date(q.data.host?.observed_at)} · CPU 按整机总算力计算</small>
      </>}
    </CardContent></Card>
    <Sheet open={open} onOpenChange={setOpen}><SheetContent side="right" title="中心运行状态" description="每 10 秒采样；历史保留 25 小时，缺失处留空。" className="w-full sm:max-w-[640px]">
      <div className="stack p-4">
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <><HostUsage host={q.data?.host} /><ProcessUsage host={q.data?.host} names={["weir-center"]} /></>}
        <HistoryCharts path="/admin/center/resources/history" host traffic={false} processes={["weir-center"]} />
      </div>
    </SheetContent></Sheet>
  </>;
}
