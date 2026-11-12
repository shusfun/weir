import { ClientDistribution } from "../components/client-distribution";
// 订阅链接：管理员与普通用户共用一页。默认看自己的额度、订阅链接与拉取统计；管理员可切到某个用户（?user=编号）或全部用户（?user=all）。
// 操作平铺在每条链接卡片上；换地址、换凭据、删除在编辑弹窗里；最近 30 天的拉取按客户端、来源汇总。
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { Badge, Card, CardContent, CopyField, DotCrowd, Input, InsetGroup, InsetRow, InsetRows, Meter, Notice, PageHeader, Readout, SectionHeader, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonCards } from "@weir/ui";
import { api } from "../api";
import { client } from "../lib/client";
import { ClientDownload } from "../components/client-download";
import { ClientIcon } from "../components/client-icon";
import { State } from "../components/feedback";
import { LinkPanel, type NodeOption } from "../components/link-panel";
import { bytes, date } from "../lib/models";
import type { LinkStats, NodeItem, UserItem, Withheld } from "../lib/core";
import { useConsoleContext } from "../lib/console-context";
import { RedeemCard } from "../features/accounts/redeem";

interface Summary {
  quota: number;
  unlimited: boolean;
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
  // 没有 user 参数就是本人的订阅（管理员也一样）；all 是全部用户，数字是某个用户
  const scope = admin ? params.get("user") : null;
  const userParam = Number(scope) || null;
  const stats = useStats(!!scope, userParam);
  const byLink = useMemo(() => new Map((stats.data ?? []).map((s) => [s.id, s])), [stats.data]);
  const [query, setQuery] = useState("");
  const [attention, setAttention] = useState<"all" | "care" | "off">("all");
  const pulled = (stats.data ?? []).filter((s) => s.pulls_24h > 0).length;
  const refused = (stats.data ?? []).reduce((n, s) => n + s.refused_30d, 0);
  const header = (
    <PageHeader
      title="订阅链接"
      description={stats.data ? <span className="weir-sum"><span><b>{stats.data.length}</b> 条链接</span><span><b>{pulled}</b> 条 24 小时内拉取过</span>{refused > 0 && <span className="text-[var(--weir-warn-ink)]">{refused} 次不在生效时间内</span>}</span> : "拉取统计未知"}
      actions={admin ? <UserFilter value={scope} onChange={(v) => setParams((p) => { if (v) p.set("user", v); else p.delete("user"); p.delete("focus"); return p; }, { replace: true })} /> : undefined}
    />
  );
  useConsoleContext({
    page: "订阅链接",
    list: {
      filters: admin ? { 用户: scope === "all" ? "全部" : scope ?? "我自己" } : undefined,
      total: stats.data?.length ?? 0,
      items: (stats.data ?? []).slice(0, 20).map((s) => ({ type: "link" as const, name: s.name || "未命名", id: s.id })),
    },
  });
  return (
    <div className="stack">
      {header}
      <div className="flex flex-wrap items-center gap-[9px]">
        <Input className="w-[261px] max-w-full" aria-label="搜索设备或用户" placeholder="搜索设备或用户" value={query} onChange={(e) => setQuery(e.target.value)} />
        <Segmented aria-label="链接筛选" value={attention} onValueChange={setAttention} options={[["all", `全部 ${stats.data?.length ?? ""}`.trim()], ["care", `需要注意 ${(stats.data ?? []).filter((s) => s.enabled && s.pulls_24h === 0).length}`], ["off", `已停用 ${(stats.data ?? []).filter((s) => !s.enabled).length}`]] as const} />
      </div>
      {scope ? <AdminLinks user={userParam} stats={byLink} query={query} attention={attention} /> : <MyLinks admin={admin} stats={byLink} query={query} attention={attention} />}
      <RecentPulls stats={stats.data} />
      <StatsOverview stats={stats} scope={scope ? (userParam ? "这个用户" : "全部用户") : undefined} />
    </div>
  );
}

function RecentPulls({ stats }: { stats?: LinkStats[] }) {
  const rows = (stats ?? []).filter((s) => s.last_pull);
  return (
    <section className="stack">
      <SectionHeader className="mt-[25px]" title="最近拉取" actions={<span className="text-meta text-[var(--weir-ink-3)]">来源只对管理员显示</span>} />
      {!stats ? <State loading /> : rows.length === 0 ? <State empty="还没有拉取记录" /> : (
        <Card className="overflow-hidden p-0">
          {rows.slice(0, 8).map((s) => (
            <div key={s.id} className="weir-set-row">
              <div className="min-w-0"><b className="block truncate">{s.name || "未命名"} · {s.last_pull?.client || "未知客户端"}</b><div className="font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{s.last_pull ? date(s.last_pull.at) : "未知"} · {s.last_pull?.ip || "来源未知"}</div></div>
              {s.last_pull?.status === "ok" ? <span className="text-meta text-[var(--weir-ink-3)]">返回 {s.last_pull.target || "订阅"}</span> : <Badge variant="soft" tone="warn">{s.last_pull?.status || "未知"}</Badge>}
            </div>
          ))}
        </Card>
      )}
    </section>
  );
}

function UserFilter({ value, onChange }: { value: string | null; onChange: (v: string | null) => void }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const current = users.data?.find((u) => String(u.id) === value);
  return (
    <Select value={value ?? "me"} onValueChange={(v) => onChange(v === "me" ? null : String(v))}>
      <SelectTrigger aria-label="查看谁的订阅" className="min-w-44">
        <SelectValue>{current ? current.name || current.email : value === "all" ? "全部用户" : "我自己"}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="me">我自己</SelectItem>
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

  const active = items.filter((s) => s.enabled && s.state === "active").length;
  return (
    <Card>
      <CardContent className="grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4">
        <Readout label="生效中的链接" value={`${active} / ${items.length}`} caption={scope} />
        <Readout label="近 24 小时拉取" value={`${sum((s) => s.pulls_24h)} 次`} />
        <Readout label="近 30 天拉取" value={`${sum((s) => s.pulls_30d)} 次`} caption={sum((s) => s.refused_30d) > 0 ? `其中 ${sum((s) => s.refused_30d)} 次不在生效时间内` : undefined} />
        <div className="col-span-2 stack sm:col-span-4"><strong className="text-meta">常用客户端</strong><ClientDistribution clients={[...clients].map(([name,count]) => ({ name,count }))} /></div>
      </CardContent>
    </Card>
  );
}

function MyLinks({ admin, stats, query, attention }: { admin: boolean; stats: Map<number, LinkStats>; query: string; attention: "all" | "care" | "off" }) {
  const summary = useQuery({ queryKey: ["me-subscription"], queryFn: () => api("/me/subscription") as Promise<Summary>, refetchInterval: 30_000 });
  const nodes = useQuery({ queryKey: ["me-nodes"], queryFn: async () => (await api("/me/nodes")).items as NodeOption[] });
  if (summary.isPending || nodes.isPending || summary.error || nodes.error)
    return <State loading={summary.isPending || nodes.isPending} error={summary.error ?? nodes.error} onRetry={() => { void summary.refetch(); void nodes.refetch(); }} skeleton={<SkeletonCards count={2} lines={2} />} />;
  const s = summary.data;
  return (
    <>
      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-3">
          <Readout label="已用流量" value={bytes(s.used)} caption={s.unlimited ? "不限额度" : `共 ${bytes(s.quota)}`} />
          <Readout label="套餐到期" value={s.expires_at ? date(s.expires_at) : "长期有效"} caption="链接的结束时间不能晚于它" />
          <Readout label="用量重置" value={s.monthly_reset ? date(s.next_reset ?? undefined) : "不重置"} caption={s.monthly_reset ? "每月自动清零" : undefined} />
          {!s.unlimited && s.quota > 0 && <Meter className="sm:col-span-3" label="流量使用" value={s.used} max={s.quota} />}
        </CardContent>
      </Card>
      {(s.withheld ?? []).length > 0 && (
        <Notice tone="warn">
          你的套餐有流量额度，以下 WireGuard 节点没有放进订阅：{s.withheld!.map((w) => w.name).join("、")}。WireGuard 没有按用户的流量统计，无法执行额度，只发给不限额度的用户。
        </Notice>
      )}
      {nodes.data.length === 0 && <State empty="暂无可用节点" detail={admin ? "到「VPS 与节点」建好节点后，会自动进入你的订阅。" : "请联系管理员分配节点。"} />}
      <LinkPanel base="/me" planEnds={s.expires_at} stats={stats} variant="tickets" query={query} attention={attention} title="订阅链接" />
      <GroupsCard />
      <RedeemCard />
      <ClientsCard />
    </>
  );
}

// AdminLinks：选了用户时只看这个用户；全部用户时每人一段，各自的「新链接」建在这个用户名下。
function AdminLinks({ user, stats, query, attention }: { user: number | null; stats: Map<number, LinkStats>; query: string; attention: "all" | "care" | "off" }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  if (users.isPending || nodes.isPending || users.error || nodes.error)
    return <State loading={users.isPending || nodes.isPending} error={users.error ?? nodes.error} onRetry={() => { void users.refetch(); void nodes.refetch(); }} skeleton={<SkeletonCards count={2} lines={2} />} />;
  const optionsFor = (u: UserItem): NodeOption[] =>
    nodes.data.filter((n) => n.enabled && (u.all_nodes || (u.node_ids ?? []).includes(n.id)));
  const list = user ? users.data.filter((u) => u.id === user) : users.data;
  if (list.length === 0) return <State empty={user ? "这个用户已不存在" : "还没有用户"} />;
  return (
    <>
      {list.map((u) => (
        <Card key={u.id}>
          <CardContent>
            <LinkPanel
              base={`/admin/users/${u.id}`}
              planEnds={u.expires_at}
              stats={stats}
              variant="tickets"
              query={query}
              attention={attention}
              showCredentials={!!user}
              title={u.name ? `${u.name}（${u.email}）` : u.email}
              description={`已用 ${bytes(u.used)}${u.unlimited ? "（不限额度）" : " / " + bytes(u.quota)} · 套餐${u.expires_at ? " " + date(u.expires_at) + " 到期" : "长期有效"}${u.enabled ? "" : " · 账户已停用"}`}
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
    <InsetGroup title="订阅组" description="管理员分配给你的订阅组，地址各自独立，可直接导入客户端。">
      {q.error ? (
        <State error={q.error} onRetry={() => q.refetch()} />
      ) : (
        <InsetRows>
          {q.data!.map((g) => (
            <InsetRow key={g.id} className="stack">
              <strong>{g.name}{g.expires_at ? <small className="ml-2 text-[var(--weir-ink-3)]">到期 {date(g.expires_at)}</small> : null}</strong>
              <CopyField label={g.name + " 订阅地址"} value={g.url} />
            </InsetRow>
          ))}
        </InsetRows>
      )}
    </InsetGroup>
  );
}

// ClientsCard：管理员维护的客户端目录（只显示启用项）；下载按「GitHub 加速」的直连与镜像测速后选择。
function ClientsCard() {
  const q = useQuery({ queryKey: ["client-catalog"], queryFn: () => client.catalog() });
  if (q.isPending) return null;
  const items = (q.data?.clients ?? []).filter((c) => c.enabled);
  return (
    <InsetGroup title="客户端下载" description="下载入口由管理员维护；导入时在链接的「一键导入页」选择对应客户端。">
      {q.error ? (
        <State error={q.error} onRetry={() => q.refetch()} />
      ) : items.length === 0 ? (
        <State empty="管理员尚未提供客户端下载" />
      ) : (
        <InsetRows as="ul">
          {items.map((c) => (
            <InsetRow as="li" key={c.id} className="flex items-center justify-between gap-2">
              <span className="flex min-w-0 items-center gap-2">
                <ClientIcon icon={c.icon} data={c.icon_data} />
                <span className="min-w-0"><strong>{c.name}</strong><small className="block truncate text-[var(--weir-ink-3)]">{c.description}</small></span>
              </span>
              <ClientDownload client={c} />
            </InsetRow>
          ))}
        </InsetRows>
      )}
    </InsetGroup>
  );
}
