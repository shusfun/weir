// 我的订阅：用量与到期一览；自己管理多条订阅链接与自建链式代理（只能使用管理员分配的节点）。
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, Meter, PageHeader, Readout } from "@migate/ui";
import { api } from "../api";
import { State } from "../components/feedback";
import { ChainPanel, LinkPanel, type NodeOption } from "../components/link-panel";
import { bytes, date } from "../lib/models";

interface Summary {
  quota: number;
  used: number;
  expires_at?: string | null;
  monthly_reset: boolean;
  next_reset?: string | null;
}

export function SubscriptionPage() {
  const summary = useQuery({ queryKey: ["me-subscription"], queryFn: () => api("/me/subscription") as Promise<Summary>, refetchInterval: 30_000 });
  const nodes = useQuery({ queryKey: ["me-nodes"], queryFn: async () => (await api("/me/nodes")).items as NodeOption[] });
  const header = <PageHeader eyebrow="个人" title="我的订阅" description="每条链接凭据独立、共享同一份额度；可以给不同设备分别生成。" />;
  if (summary.isPending || nodes.isPending || summary.error || nodes.error)
    return (
      <div className="stack">
        {header}
        <State loading={summary.isPending || nodes.isPending} error={summary.error ?? nodes.error} onRetry={() => { void summary.refetch(); void nodes.refetch(); }} />
      </div>
    );
  const s = summary.data;
  return (
    <div className="stack">
      {header}
      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-3">
          <Readout label="已用流量" value={bytes(s.used)} caption={s.quota ? `共 ${bytes(s.quota)}` : "不限额度"} />
          <Readout label="到期时间" value={s.expires_at ? date(s.expires_at) : "长期有效"} />
          <Readout label="用量重置" value={s.monthly_reset ? date(s.next_reset ?? undefined) : "不重置"} caption={s.monthly_reset ? "每月自动清零" : undefined} />
          {s.quota > 0 && <Meter className="sm:col-span-3" label="流量使用" value={s.used} max={s.quota} />}
        </CardContent>
      </Card>
      {nodes.data.length === 0 ? (
        <State empty="暂无可用节点" detail="请联系管理员分配节点。" />
      ) : (
        <>
          <Card><CardContent><LinkPanel base="/me" nodes={nodes.data} /></CardContent></Card>
          <Card><CardContent><ChainPanel base="/me" nodes={nodes.data} /></CardContent></Card>
        </>
      )}
    </div>
  );
}
