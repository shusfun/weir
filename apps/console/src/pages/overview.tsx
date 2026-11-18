import { useQuery } from "@tanstack/react-query";
import { Badge, Button, InsetRow, InsetRows, MetricStrip, PageHeader, SectionHeader, Segmented, TextLink } from "@weir/ui";
import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { State } from "../components/feedback";
import { ObjectLink } from "../components/object-link";
import { useConsoleContext } from "../lib/console-context";
import { objectStatus, type NodeItem, type ObjectState, type UserItem } from "../lib/core";
import type { ObjectRef } from "../lib/objects";
import { sourcesQuery } from "../lib/subscription-sources";

const categories = [["all", "全部"], ["user", "账户"], ["node", "节点"], ["source", "订阅源"]] as const;
type Category = (typeof categories)[number][0];
type WorkItem = { object: ObjectRef & { type: Exclude<Category, "all">; name: string }; label: string; reason: string; rank: number };
const routes = { user: "/users", node: "/gates?view=nodes", source: "/subscriptions" };
const readOptions = { refetchInterval: false, refetchOnWindowFocus: false, retry: false } as const;
const rowLimit = 12;

function workItem(object: WorkItem["object"], state?: ObjectState, rank?: number, label?: string): WorkItem {
  const [status, , reason] = objectStatus(state);
  const priority = state?.status === "failed" ? 0 : state?.status === "warning" ? 1 : 2;
  return { object, label: label ?? status, reason, rank: rank ?? priority };
}

export function OverviewPage() {
  const [category, setCategory] = useState<Category>("all");
  const [limit, setLimit] = useState(rowLimit);
  const nodes = useQuery({ ...readOptions, queryKey: ["nodes"], queryFn: async ({ signal }) => (await api("/admin/nodes", "GET", undefined, { signal })).items as NodeItem[] });
  const users = useQuery({ ...readOptions, queryKey: ["users"], queryFn: async ({ signal }) => (await api("/admin/users", "GET", undefined, { signal })).items as UserItem[] });
  const sources = useQuery({ ...readOptions, ...sourcesQuery });
  const inventory = [
    { key: "user", label: "账户", manage: "管理用户", query: users, count: users.isSuccess ? users.data.length : null },
    { key: "node", label: "节点", manage: "管理节点", query: nodes, count: nodes.isSuccess ? nodes.data.length : null },
    { key: "source", label: "订阅源", manage: "管理订阅源", query: sources, count: sources.isSuccess ? sources.data.items.length : null },
  ] as const;
  const attention: WorkItem[] = [
    ...(users.isSuccess ? users.data : []).flatMap(user => user.enabled && user.object_state?.status !== "ok"
      ? [workItem({ type: "user", id: user.id, name: user.name || user.email }, user.object_state)] : []),
    ...(nodes.isSuccess ? nodes.data : []).flatMap(node => {
      const state = node.object_state;
      if (!node.enabled || state?.blocked_by_gate || state?.status === "ok") return [];
      if (state?.status === "partial" && node.test?.external === "passed" && node.test.udp.status === "not_applicable") return [];
      return [workItem({ type: "node", id: node.id, name: node.name }, state, undefined, state?.status === "partial" ? "观测不完整" : undefined)];
    }),
    ...(sources.isSuccess ? sources.data.items : []).flatMap(source => {
      if (!source.enabled) return [];
      const state = source.object_state;
      const object = { type: "source" as const, id: source.id, name: source.name };
      if (source.last_error) return [workItem(object, state, 0, "刷新失败")];
      if (state?.status === "partial" || state?.status === "ok") return [];
      return [workItem(object, state, state?.status === "stale" ? 1 : undefined)];
    }),
  ].sort((first, second) => first.rank - second.rank);
  const selected = category === "all" ? inventory : inventory.filter(item => item.key === category);
  const visible = attention.filter(item => category === "all" || item.object.type === category);
  const actionable = attention.filter(item => item.rank < 2).length;
  const unobserved = attention.length - actionable;
  const incomplete = inventory.filter(item => !item.query.isSuccess).length;
  const empty = inventory.every(item => item.count === 0);
  const conclusion = actionable ? actionable + " 项需要处理" : unobserved ? unobserved + " 项待核对" : incomplete ? "待办尚未核对完整" : empty ? "还没有纳管对象" : "暂无待办";
  const summary = [actionable > 0 && unobserved > 0 ? unobserved + " 项待核对" : "", incomplete ? incomplete + " 个分类尚未取得数据" : ""].filter(Boolean).join(" · ");
  useConsoleContext({ page: "概览", list: { filters: { 分类: categories.find(([key]) => key === category)![1] }, total: visible.length, items: visible.slice(0, 20).map(item => item.object) } });
  return <div className="stack">
    <PageHeader eyebrow="管理工作台" title={conclusion} description={summary || undefined} className="weir-workbench-heading" />
    <div className="grid min-w-0 items-start gap-[var(--weir-space-6)] xl:grid-cols-[minmax(0,1fr)_minmax(0,321px)]">
      <section className="stack min-w-0" data-tour="overview-attention">
        <SectionHeader title="待办事项" flush />
        <Segmented aria-label="待办分类" value={category} onValueChange={value => { setCategory(value); setLimit(rowLimit); }} options={categories} />
        {selected.filter(item => !item.query.isSuccess).map(({ key, label, query }) => <div key={key} className="stack-tight">
          <strong>{label} · {query.error ? "读取失败" : "读取中"}</strong>
          <State loading={query.isPending} error={query.error} onRetry={() => void query.refetch()} />
        </div>)}
        {visible.length > 0 ? <InsetRows as="ul">
          {visible.slice(0, limit).map(({ object, label, reason, rank }) => <InsetRow as="li" key={object.type + ":" + object.id} className="flex flex-wrap items-start justify-between gap-[var(--weir-space-2)]">
            <div className="min-w-0 flex-1 break-words">
              <strong><ObjectLink to={object} /></strong>
              <small className="block text-[var(--weir-ink-2)]">{categories.find(([key]) => key === object.type)?.[1]} · {reason}</small>
            </div>
            <Badge variant="status" tone={rank === 0 ? "bad" : rank === 1 ? "warn" : "neutral"}>{label}</Badge>
          </InsetRow>)}
        </InsetRows> : selected.every(item => item.query.isSuccess) && <State empty={selected.every(item => item.count === 0) ? "该范围尚无纳管对象" : "该范围暂无待办"} />}
        {visible.length > limit && <Button size="sm" variant="outline" className="w-fit" onClick={() => setLimit(limit + rowLimit)}>显示更多（还有 {visible.length - limit} 项）</Button>}
      </section>
      <aside className="stack min-w-0" data-tour="overview-inventory">
        <SectionHeader title="管理库存" flush />
        <MetricStrip items={inventory.map(item => ({ label: item.label, value: item.count }))} />
        <div className="flex flex-wrap gap-x-[var(--weir-space-4)] gap-y-[var(--weir-space-2)]">
          {inventory.map(item => <TextLink key={item.key} render={<Link to={routes[item.key]} />}>{item.manage}</TextLink>)}
          <TextLink render={<Link to="/ops/observe" />}>查看运行观测</TextLink>
        </div>
      </aside>
    </div>
  </div>;
}
