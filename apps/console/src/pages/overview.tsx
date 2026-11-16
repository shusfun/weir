import { Link } from "react-router-dom";
import { Button, Card, CardContent, CardHeader, InsetGroup, InsetRow, InsetRows, MetricStrip, PageHeader, StatusLamp } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { objectStatus, type Gate, type NodeItem, type ObjectState, type UserItem } from "../lib/core";
import { useSources } from "../lib/subscription-sources";
import type { ObjectRef } from "../lib/objects";
import { useConsoleContext } from "../lib/console-context";
import { ObjectLink } from "../components/object-link";
import { State } from "../components/feedback";
import { date } from "../lib/models";

type Attention = { object: ObjectRef & { name: string }; state?: ObjectState };
const priority: Record<string, number> = { failed: 0, warning: 1, pending: 2, unavailable: 3, stale: 4, unknown: 5, partial: 6 };

export function OverviewPage() {
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const sources = useSources();
  const queries = [gates, nodes, users, sources];
  const complete = queries.every(q => q.isSuccess);
  const attention: Attention[] = [
    ...(gates.data ?? []).map(g => ({ object: { type: "gate" as const, id: g.id, name: g.name }, state: g.object_state })),
    ...(nodes.data ?? []).map(n => ({ object: { type: "node" as const, id: n.id, name: n.name }, state: n.object_state })),
    ...(users.data ?? []).map(u => ({ object: { type: "user" as const, id: u.id, name: u.name || u.email }, state: u.object_state })),
    ...(sources.data?.items ?? []).map(s => ({ object: { type: "source" as const, id: s.id, name: s.name }, state: s.object_state })),
  ].filter(item => !["ok", "disabled", "revoked"].includes(item.state?.status ?? "unknown"))
    .sort((a, b) => (priority[a.state?.status ?? "unknown"] ?? 5) - (priority[b.state?.status ?? "unknown"] ?? 5));
  useConsoleContext({ page: "概览", list: { total: attention.length, items: attention.slice(0, 20).map(item => item.object) } });
  return <div className="stack">
    <PageHeader size="hero" eyebrow="概览" title={complete ? attention.length ? `${attention.length} 项需要核对` : "当前没有待处理项" : "正在核对运行状态"} description="状态由中心提供；配置生效、端口可达与整条路径实测分别展示。" />
    <MetricStrip items={[
      { label: "VPS 在线", value: gates.data ? `${gates.data.filter(g => g.object_state?.status === "ok").length} / ${gates.data.length}` : null },
      { label: "节点", value: nodes.data?.length ?? null, caption: "连通性见各节点实测" },
      { label: "用户", value: users.data?.length ?? null },
      { label: "订阅源", value: sources.data?.items.length ?? null, caption: "缓存不代表节点连通" },
    ]} />
    <div className="grid gap-[var(--weir-space-5)] lg:grid-cols-[minmax(0,1fr)_minmax(0,361px)]">
      <InsetGroup title="待处理与观测缺口" description="先处理失败，再核对等待结果和缺少的观测。" data-tour="overview-attention">
        {queries.map((q, i) => q.isPending || q.error ? <State key={i} loading={q.isPending} error={q.error} onRetry={() => void q.refetch()} /> : null)}
        <InsetRows>
          {complete && attention.length === 0 && <InsetRow>当前状态没有待处理项。</InsetRow>}
          {attention.map(({ object, state }) => <InsetRow key={`${object.type}:${object.id}`} className="flex items-start gap-[var(--weir-space-3)]">
            <StatusLamp tone={objectStatus(state)[1]} label={objectStatus(state)[0]} />
            <div className="stack-tight min-w-0">
              <strong><ObjectLink to={object} /> · {objectStatus(state)[0]}</strong>
              <small>{objectStatus(state)[2]}</small>
              <small className="text-[var(--weir-ink-3)]">观测时间 {date(state?.observed_at ?? undefined)}</small>
            </div>
          </InsetRow>)}
        </InsetRows>
      </InsetGroup>
      <div className="stack">
        <Card size="sm"><CardHeader title="链路与用量" description="查看授权入口、实际出口及已观测流量。" /><CardContent><Button variant="outline" render={<Link to="/linkmap" />}>打开链路图</Button></CardContent></Card>
        <Card size="sm"><CardHeader title="后台运行" description="查看中心、VPS、Laya、dsh 的各自状态和任务回执。" /><CardContent><Button variant="outline" render={<Link to="/ops/observe?tab=system" />}>查看运行观测</Button></CardContent></Card>
        <Card size="sm"><CardHeader title="操作记录" description="核对操作人、关联编号及异步结果。" /><CardContent><Button variant="outline" render={<Link to="/ops/audit" />}>打开审计</Button></CardContent></Card>
      </div>
    </div>
  </div>;
}
