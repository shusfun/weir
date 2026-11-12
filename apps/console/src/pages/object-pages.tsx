// 独立详情路由。标签写在 ?tab=。旧的 ?gate= / ?node= / ?user= 由列表页转到这里。
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { PageHeader, PageState, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { api } from "../api";
import { State } from "../components/feedback";
import { GateDetail } from "../components/gate-detail";
import { NodeDetail } from "../components/node-detail";
import { HistoryCharts } from "../components/history-charts";
import { UserDetailView } from "../components/user-detail-view";
import { ProfileDetail } from "../components/subscription-profiles";
import { SourceDetail } from "../components/subscription-sources";
import { PublicPageDetailView } from "./public-pages";
import type { Gate, NodeItem, UserItem } from "../lib/core";

function missing(title: string, back: string) {
  return <PageState kind="error" title={title} detail="它可能已被删除，或你没有权限查看。" action={<Link to={back}>返回</Link>} />;
}

export function GateDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  if (gates.isPending || nodes.isPending) return <State loading />;
  if (gates.error || nodes.error) return <State error={gates.error || nodes.error} onRetry={() => { void gates.refetch(); void nodes.refetch(); }} />;
  const gate = gates.data?.find((g) => String(g.id) === id);
  if (!gate) return missing("没有这台 VPS", "/gates");
  return <GateDetail layout="page" gate={gate} nodes={nodes.data ?? []} close={() => navigate("/gates")} />;
}

export function NodeDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  if (gates.isPending || nodes.isPending) return <State loading />;
  if (gates.error || nodes.error) return <State error={gates.error || nodes.error} onRetry={() => { void gates.refetch(); void nodes.refetch(); }} />;
  const node = nodes.data?.find((n) => String(n.id) === id);
  if (!node) return missing("没有这个节点", "/gates");
  return <NodeDetail layout="page" node={node} gate={gates.data?.find((g) => g.id === node.gate_id)} nodes={nodes.data ?? []} close={() => navigate("/gates")} changed={() => { void nodes.refetch(); }} />;
}

export function UserDetailPage() {
  const { id } = useParams();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  if (users.isPending) return <State loading />;
  if (users.error) return <State error={users.error} onRetry={() => users.refetch()} />;
  const user = users.data?.find((u) => String(u.id) === id);
  if (!user) return missing("没有这个用户", "/users");
  return <UserDetailView user={user} users={users.data ?? []} />;
}

export function SourceDetailPage() {
  const { id = "" } = useParams();
  return (
    <div className="stack">
      <p className="text-body"><Link to="/subscriptions?tab=sources">订阅源</Link> / {id}</p>
      <PageHeader size="detail" title="订阅源" description="刷新、编辑和删除沿用列表里的同一套操作。" />
      <SourceDetail id={id} />
    </div>
  );
}

export function ProfileDetailPage() {
  const { id = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") || "overview";
  return (
    <div className="stack">
      <p className="text-body"><Link to="/subscriptions?tab=profiles">订阅组</Link> / {id}</p>
      <PageHeader size="detail" title="订阅组" description="预览、实测、分配和分享沿用原来的操作。" />
      <Tabs value={tab} onValueChange={(v) => setParams((p) => { p.set("tab", String(v)); return p; }, { replace: true })}>
        <TabsList aria-label="订阅组详情"><TabsTab value="overview">概览</TabsTab><TabsTab value="probe">实测</TabsTab></TabsList>
        <TabsPanel value="overview"><ProfileDetail id={id} tab="overview" /></TabsPanel>
        <TabsPanel value="probe"><ProfileDetail id={id} tab="probe" /></TabsPanel>
      </Tabs>
    </div>
  );
}

export function PublicPageDetail() {
  const { id = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") || "content";
  return (
    <div className="stack">
      <p className="text-body"><Link to="/pages">公开页面</Link> / {id}</p>
      <PageHeader size="detail" title="公开页面" description="编辑、留言和删除沿用列表里的同一套操作。" />
      <Tabs value={tab} onValueChange={(v) => setParams((p) => { p.set("tab", String(v)); return p; }, { replace: true })}>
        <TabsList aria-label="公开页详情">
          <TabsTab value="content">内容</TabsTab>
          <TabsTab value="share">分享</TabsTab>
        </TabsList>
        <TabsPanel value="content"><PublicPageDetailView id={id} tab="content" /></TabsPanel>
        <TabsPanel value="share"><PublicPageDetailView id={id} tab="share" /></TabsPanel>
      </Tabs>
    </div>
  );
}
