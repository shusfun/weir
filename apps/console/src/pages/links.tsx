// 订阅管理：管理员与普通用户共用一页。普通用户看自己的额度、订阅链接与拉取统计；管理员看全部用户，可按用户筛选。
// 每条链接右上角「复制」，三角里是二维码、编辑与生效时间、拉取记录等；最近 30 天的拉取按客户端、来源汇总。
import { useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CopyField, Meter, Notice, PageHeader, Readout, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonCards } from "@weir/ui";
import { api } from "../api";
import { client } from "../lib/client";
import { ClientDownload } from "../components/client-download";
import { ClientIcon } from "../components/client-icon";
import { State } from "../components/feedback";
import { LinkPanel, type NodeOption } from "../components/link-panel";
import { bytes, date } from "../lib/models";
import type { LinkStats, NodeItem, UserItem, Withheld } from "../lib/core";
import { useConsoleContext } from "../lib/console-context";

interface Summary {
  quota: number;
  used: number;
  expires_at?: string | null;
  monthly_reset: boolean;
  next_reset?: string | null;
  withheld?: Withheld[];
}

function useStats(admin: boolean, user: number | null) {
  return useQuery({
    queryKey: ["link-stats", admin, user],
    queryFn: async () => (await api(admin ? `/admin/link-stats${user ? "?user=" + user : ""}` : "/me/link-stats")).items as LinkStats[],
    refetchInterval: 30_000,
  });
}

export function LinksPage({ admin }: { admin: boolean }) {
  const [params, setParams] = useSearchParams();
  const userParam = Number(params.get("user")) || null;
  const stats = useStats(admin, userParam);
  const byLink = useMemo(() => new Map((stats.data ?? []).map((s) => [s.id, s])), [stats.data]);
  const header = (
    <PageHeader
      eyebrow="代理与订阅"
      title="订阅管理"
      description={admin ? "全部用户的订阅链接与拉取统计；可按用户筛选。每条链接可单独设置生效时间。" : "每条链接凭据独立、共享同一份额度；可以给不同设备分别生成，并设置什么时候开始、什么时候结束。"}
      actions={admin ? <UserFilter value={userParam} onChange={(id) => setParams((p) => { if (id) p.set("user", String(id)); else p.delete("user"); p.delete("focus"); return p; }, { replace: true })} /> : undefined}
    />
  );
  useConsoleContext({
    page: "订阅管理",
    list: {
      filters: admin ? { 用户: userParam ? String(userParam) : "全部" } : undefined,
      total: stats.data?.length ?? 0,
      items: (stats.data ?? []).slice(0, 20).map((s) => ({ type: "link" as const, name: s.name || "未命名", id: s.id })),
    },
  });
  return (
    <div className="stack">
      {header}
      <StatsOverview stats={stats} scope={admin ? (userParam ? "这个用户" : "全部用户") : undefined} />
      {admin ? <AdminLinks user={userParam} stats={byLink} /> : <MyLinks stats={byLink} />}
    </div>
  );
}

function UserFilter({ value, onChange }: { value: number | null; onChange: (id: number | null) => void }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const current = users.data?.find((u) => u.id === value);
  return (
    <Select value={value ? String(value) : "all"} onValueChange={(v) => onChange(v === "all" ? null : Number(v))}>
      <SelectTrigger aria-label="按用户筛选" className="min-w-44">
        <SelectValue>{current ? current.name || current.email : "全部用户"}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">全部用户</SelectItem>
        {(users.data ?? []).map((u) => <SelectItem key={u.id} value={String(u.id)}>{u.name ? `${u.name}（${u.email}）` : u.email}</SelectItem>)}
      </SelectContent>
    </Select>
  );
}

// StatsOverview：最近 24 小时 / 30 天的拉取次数、被拒绝（不在生效时间内）的次数、常用客户端。
function StatsOverview({ stats, scope }: { stats: ReturnType<typeof useStats>; scope?: string }) {
  if (stats.isPending || stats.error) return <State loading={stats.isPending} error={stats.error} onRetry={() => stats.refetch()} skeleton={<SkeletonCards count={1} lines={2} />} />;
  const items = stats.data;
  const sum = (f: (s: LinkStats) => number) => items.reduce((n, s) => n + f(s), 0);
  const clients = new Map<string, number>();
  for (const s of items) for (const c of s.clients) clients.set(c.name, (clients.get(c.name) ?? 0) + c.count);
  const top = [...clients.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4);
  const active = items.filter((s) => s.enabled && s.state === "active").length;
  return (
    <Card>
      <CardContent className="grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4">
        <Readout label="生效中的链接" value={`${active} / ${items.length}`} caption={scope} />
        <Readout label="近 24 小时拉取" value={String(sum((s) => s.pulls_24h))} caption="次" />
        <Readout label="近 30 天拉取" value={String(sum((s) => s.pulls_30d))} caption={sum((s) => s.refused_30d) > 0 ? `其中 ${sum((s) => s.refused_30d)} 次不在生效时间内` : "次"} />
        <Readout label="常用客户端" value={top.length ? top[0][0] : "—"} caption={top.length ? top.slice(1).map(([n, c]) => `${n} ${c}`).join(" · ") || `${top[0][1]} 次` : "还没有拉取记录"} />
      </CardContent>
    </Card>
  );
}

function MyLinks({ stats }: { stats: Map<number, LinkStats> }) {
  const summary = useQuery({ queryKey: ["me-subscription"], queryFn: () => api("/me/subscription") as Promise<Summary>, refetchInterval: 30_000 });
  const nodes = useQuery({ queryKey: ["me-nodes"], queryFn: async () => (await api("/me/nodes")).items as NodeOption[] });
  if (summary.isPending || nodes.isPending || summary.error || nodes.error)
    return <State loading={summary.isPending || nodes.isPending} error={summary.error ?? nodes.error} onRetry={() => { void summary.refetch(); void nodes.refetch(); }} skeleton={<SkeletonCards count={2} lines={2} />} />;
  const s = summary.data;
  return (
    <>
      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-3">
          <Readout label="已用流量" value={bytes(s.used)} caption={s.quota ? `共 ${bytes(s.quota)}` : "不限额度"} />
          <Readout label="套餐到期" value={s.expires_at ? date(s.expires_at) : "长期有效"} caption="链接的结束时间不能晚于它" />
          <Readout label="用量重置" value={s.monthly_reset ? date(s.next_reset ?? undefined) : "不重置"} caption={s.monthly_reset ? "每月自动清零" : undefined} />
          {s.quota > 0 && <Meter className="sm:col-span-3" label="流量使用" value={s.used} max={s.quota} />}
        </CardContent>
      </Card>
      {(s.withheld ?? []).length > 0 && (
        <Notice tone="warn">
          你的套餐有流量额度，以下 WireGuard 节点没有放进订阅：{s.withheld!.map((w) => w.name).join("、")}。WireGuard 没有按用户的流量统计，无法执行额度，只发给不限额度的用户。
        </Notice>
      )}
      {nodes.data.length === 0 ? (
        <State empty="暂无可用节点" detail="请联系管理员分配节点。" />
      ) : (
        <Card><CardContent><LinkPanel base="/me" nodes={nodes.data} planEnds={s.expires_at} stats={stats} /></CardContent></Card>
      )}
      <GroupsCard />
      <ClientsCard />
    </>
  );
}

// AdminLinks：选了用户时只看这个用户；全部用户时每人一段，各自的「新链接」建在这个用户名下。
function AdminLinks({ user, stats }: { user: number | null; stats: Map<number, LinkStats> }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  if (users.isPending || nodes.isPending || users.error || nodes.error)
    return <State loading={users.isPending || nodes.isPending} error={users.error ?? nodes.error} onRetry={() => { void users.refetch(); void nodes.refetch(); }} skeleton={<SkeletonCards count={2} lines={2} />} />;
  const optionsFor = (u: UserItem): NodeOption[] =>
    nodes.data.filter((n) => n.enabled && (u.all_nodes || (u.node_ids ?? []).includes(n.id))).map((n) => ({ id: n.id, name: n.name, gate_name: n.gate_name }));
  const list = user ? users.data.filter((u) => u.id === user) : users.data;
  if (list.length === 0) return <State empty={user ? "这个用户已不存在" : "还没有用户"} />;
  return (
    <>
      {list.map((u) => (
        <Card key={u.id}>
          <CardContent>
            <LinkPanel
              base={`/admin/users/${u.id}`}
              nodes={optionsFor(u)}
              planEnds={u.expires_at}
              stats={stats}
              showCredentials={!!user}
              title={u.name ? `${u.name}（${u.email}）` : u.email}
              description={`已用 ${bytes(u.used)}${u.quota ? " / " + bytes(u.quota) : "（不限额度）"} · 套餐${u.expires_at ? " " + date(u.expires_at) + " 到期" : "长期有效"}${u.enabled ? "" : " · 账户已停用"}`}
            />
          </CardContent>
        </Card>
      ))}
    </>
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
              <strong>{g.name}{g.expires_at ? <small className="ml-2 text-[var(--weir-ink-3)]">到期 {date(g.expires_at)}</small> : null}</strong>
              <CopyField label={g.name + " 订阅地址"} value={g.url} />
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
          <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((c) => (
              <li key={c.id} className="flex min-w-0 items-center justify-between gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
                <span className="flex min-w-0 items-center gap-2">
                  <ClientIcon icon={c.icon} data={c.icon_data} />
                  <span className="min-w-0"><strong>{c.name}</strong><small className="block truncate text-[var(--weir-ink-3)]">{c.description}</small></span>
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
