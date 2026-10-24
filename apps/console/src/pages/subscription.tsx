// 我的订阅：用量与到期一览；自己管理多条订阅链接与自建链式代理（只能使用管理员分配的节点）。
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CopyButton, Input, Meter, PageHeader, Readout, SectionHeader, SkeletonCards } from "@migate/ui";
import { api } from "../api";
import { client } from "../lib/client";
import { ClientDownload } from "../components/client-download";
import { ClientIcon } from "../components/client-icon";
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
        <State
          loading={summary.isPending || nodes.isPending}
          error={summary.error ?? nodes.error}
          onRetry={() => { void summary.refetch(); void nodes.refetch(); }}
          skeleton={<SkeletonCards count={3} lines={2} />}
        />
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
      <GroupsCard />
      <ClientsCard />
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

interface Group {
  id: number;
  name: string;
  url: string;
  expires_at?: string | null;
  last_used_at?: string | null;
}

// GroupsCard：管理员分配给本人的订阅组（外部机场节点，不占用本人额度）；没有分配时不显示。
function GroupsCard() {
  const q = useQuery({ queryKey: ["me-groups"], queryFn: async () => (await api("/me/groups")).items as Group[] });
  if (q.isPending || (q.data && q.data.length === 0)) return null;
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title="订阅组" description="管理员分配给你的订阅组，地址各自独立，可直接导入客户端。" />
        {q.error ? (
          <State error={q.error} onRetry={() => q.refetch()} />
        ) : (
          q.data!.map((g) => (
            <div key={g.id} className="stack">
              <strong>{g.name}{g.expires_at ? <small className="ml-2 text-[var(--mg-ink-3)]">到期 {date(g.expires_at)}</small> : null}</strong>
              <div className="subscription-url">
                <Input readOnly className="font-mono text-xs" aria-label={g.name + " 订阅地址"} value={g.url} />
                <CopyButton value={g.url}>复制</CopyButton>
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}

// ClientsCard：管理员维护的客户端目录（只显示启用项）；下载经加速镜像测速后选择。
function ClientsCard() {
  const q = useQuery({ queryKey: ["client-catalog"], queryFn: () => client.catalog() });
  if (q.isPending) return null;
  const items = (q.data?.clients ?? []).filter((c) => c.enabled);
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title="客户端下载" description="下载入口由管理员维护；导入时在链接的「一键导入页」选择对应客户端。" />
        {q.error ? (
          <State error={q.error} onRetry={() => q.refetch()} />
        ) : items.length === 0 ? (
          <State empty="管理员尚未提供客户端下载" />
        ) : (
          <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((c) => (
              <li key={c.id} className="flex items-center justify-between gap-2 rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
                <span className="flex min-w-0 items-center gap-2">
                  <ClientIcon icon={c.icon} data={c.icon_data} />
                  <span className="min-w-0"><strong>{c.name}</strong><small className="block truncate text-[var(--mg-ink-3)]">{c.description}</small></span>
                </span>
                <ClientDownload client={c} />
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
