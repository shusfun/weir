import { useQueryClient } from "@tanstack/react-query";
import { State } from "./feedback";
// 用户详情页（设计稿 BUserDetail）：头像行 + 一句结论 + 右侧用量环；标签页：概览（能用的节点、额度与期限）、链接、链式代理、流量、记录。
// 账户结论由中心统一提供，只在标题说一次；用量环在不限额度时画成未知。
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Avatar, Button, Card, CheckboxField, TrafficRing, trafficReading, ObjectHeader, SectionHeader, Tabs, TabsList, TabsPanel, TabsTab, useConfirm } from "@weir/ui";
import { useQuery } from "../lib/query";
import { api } from "../api";
import { bytes, date } from "../lib/models";
import { ChainPanel, LinkPanel } from "./link-panel";
import { HistoryCharts } from "./history-charts";
import { UsageRanking } from "./usage-ranking";
import { UserForm } from "./user-form";
import { CatalogGrants, type CatalogNode } from "./node-catalog";
import { useStringDialogParam } from "./object-link";
import { objectStatus, protocolNames, type NodeItem, type UserItem } from "../lib/core";
import { useRowAction } from "../lib/action";

function conclusion(u: UserItem): [string, string] {
  const name = u.name || u.email.split("@")[0];
  // 中心没给结论时不在标题里写「状态未知」，只说明原因
  if (!u.object_state) return [name, "中心尚未提供这个用户的状态结论"];
  return [u.object_state.status === "ok" ? name : `${name} · ${u.object_state.label}`, u.object_state.reason];
}

export function UserDetailView({ user, users, inlineQr = false }: { user: UserItem; users: UserItem[]; inlineQr?: boolean }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [editing, setEditing] = useState(false);
  const granting = useStringDialogParam("grant");
  const catalog = useQuery({ queryKey: ["node-catalog", user.id], queryFn: async () => await api(`/admin/users/${user.id}/node-catalog`) as { items: CatalogNode[]; revision: string } });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const tab = params.get("detail_tab") || "overview";
  const [title, lead] = conclusion(user);
  const idx = users.findIndex((x) => x.id === user.id);
  const mine = (nodes.data ?? []).filter((n) => n.enabled && (user.all_nodes || (user.node_ids ?? []).includes(n.id)));
  return (
    <div className="stack">
      <ObjectHeader
        keepAside
        back={<Link to="/users">用户</Link>}
        crumb={user.name || user.email.split("@")[0]}
        actions={<>
          <Button variant="ghost" disabled={idx <= 0} onClick={() => navigate(`/users?user=${users[idx - 1].id}&detail_tab=${encodeURIComponent(tab)}`)}>上一个</Button>
          <Button variant="ghost" disabled={idx < 0 || idx >= users.length - 1} onClick={() => navigate(`/users?user=${users[idx + 1].id}&detail_tab=${encodeURIComponent(tab)}`)}>下一个</Button>
          <Button variant="outline" onClick={() => setEditing(true)}>编辑</Button>
        </>}
        tone={objectStatus(user.object_state)[1]}
        status={<span className="flex items-center gap-[var(--weir-gap-9)]"><Avatar name={user.name || user.email} /><span>{user.role === "admin" ? "管理员" : "用户"} · {user.email}<br />{user.created_at ? `${date(user.created_at)} 创建` : "创建时间未知"}</span></span>}
        title={<span className="whitespace-pre-line">{title}</span>}
        banner={<p className="m-0 max-w-[521px] text-lead text-[var(--weir-ink-2)]">{lead}</p>}
        aside={<div className="weir-user-traffic-aside"><TrafficRing used={user.used} quota={user.quota} unlimited={user.unlimited} /></div>}
      />
      <Tabs value={tab} onValueChange={(v) => setParams((p) => { p.set("detail_tab", String(v)); return p; }, { replace: true })}>
        <TabsList aria-label="用户详情">
          <TabsTab value="overview">概览</TabsTab>
          <TabsTab value="links">链接{user.links ? ` ${user.links}` : ""}</TabsTab>
          <TabsTab value="chains">链式代理{user.chains ? ` ${user.chains}` : ""}</TabsTab>
          <TabsTab value="traffic">流量</TabsTab>
          <TabsTab value="log">记录</TabsTab>
        </TabsList>
        <TabsPanel value="overview">
          <div className="weir-user-detail-columns">
            <div>
              <SectionHeader title="能用的节点" actions={<Button size="sm" variant="ghost" onClick={() => granting.open(String(user.id))}>调整授权</Button>} />
              {nodes.error ? <State error={nodes.error} onRetry={() => void nodes.refetch()} /> : mine.length === 0 ? <p className="m-0 text-meta text-[var(--weir-ink-3)]">{nodes.isPending ? "正在读取节点" : "还没有分配节点"}</p> : (
                <div className="grid gap-[var(--weir-gap-13)] sm:grid-cols-3">
                  {mine.map((n) => (
                    <Link key={n.id} to={`/nodes/${n.id}`} className="weir-mini-card">
                      <small>{n.gate_name ?? "VPS 未知"}</small>
                      <b className="block truncate">{protocolNames[n.protocol] ?? n.name}</b>
                      <small className="font-[family-name:var(--weir-mono)]">端口 {n.port || "未知"}</small>
                    </Link>
                  ))}
                </div>
              )}
              {catalog.error ? <State error={catalog.error} onRetry={() => catalog.refetch()} /> : catalog.data?.items.filter((item) => item.kind !== "node" && item.authorized).map((item) => <div className="weir-set-row" key={item.ref}><span>{item.name || item.ref}</span><small>{item.state.reason}</small></div>)}
            </div>
            <div>
              <SectionHeader title="额度与期限" />
              <Card className="overflow-hidden p-0">
                <div className="weir-set-row"><span>每月额度</span><b className="font-[family-name:var(--weir-mono)]">{user.unlimited ? "不限" : bytes(user.quota)}</b></div>
                <div className="weir-set-row"><span>重置</span><b>{user.monthly_reset ? "每月重置" : "不重置"}</b></div>
                <div className="weir-set-row"><span>到期</span><b>{user.expires_at ? date(user.expires_at) : "长期"}</b></div>
                <div className="weir-set-row"><span>用完时</span><b>停用并断开连接</b></div>
              </Card>
            </div>
          </div>
          <UserGroups user={user} />
        </TabsPanel>
        <TabsPanel value="links"><LinkPanel base={`/admin/users/${user.id}`} planEnds={user.expires_at} showCredentials inlineQr={inlineQr} /></TabsPanel>
        <TabsPanel value="chains"><ChainPanel base={`/admin/users/${user.id}`} admin /></TabsPanel>
        <TabsPanel value="traffic"><HistoryCharts path={`/admin/users/${user.id}/history`} /><UsageRanking path={`/admin/users/${user.id}/usage`} /></TabsPanel>
        <TabsPanel value="log"><p className="m-0 text-body text-[var(--weir-ink-2)]">这个用户的操作记录在 <Link to={`/ops/audit?q=user:${user.id}`}>操作审计</Link>。</p></TabsPanel>
      </Tabs>
      <UserForm value={editing ? user : null} nodes={nodes.data ?? []} nodesState={{ loading: nodes.isPending, error: nodes.error }} close={() => setEditing(false)} saved={(updated) => { client.setQueryData<UserItem[]>(["users"], old => old?.map(u => u.id === updated.id ? { ...u, ...updated } : u)); void client.invalidateQueries({ queryKey: ["users"] }); setEditing(false); }} />
      {granting.value === String(user.id) && <CatalogGrants user={user} close={granting.close} />}
    </div>
  );
}

type UserGroupItem = { id: number; name: string; enabled: boolean; expires_at?: string | null; granted: boolean; last_used_at?: string | null };

// 订阅组分区：勾选即分配、取消即撤销；其余分配和它们的订阅地址保持不变。
function UserGroups({ user }: { user: UserItem }) {
  const confirm = useConfirm();
  const q = useQuery({ queryKey: ["user-subscription-groups", user.id], queryFn: async () => (await api(`/admin/users/${user.id}/subscription-groups`)) as { items: UserGroupItem[] } });
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch, invalidate: [["subscription-profiles"]] });
  const items = q.data?.items ?? [];
  async function toggle(g: UserGroupItem, on: boolean) {
    if (!on && !(await confirm({ title: `取消分配「${g.name}」？`, description: "这个用户的这个订阅地址随即失效；其他订阅组不受影响。", confirmText: "取消分配", tone: "danger" }))) return;
    const ids = items.filter((x) => x.granted).map((x) => x.id);
    const next = on ? [...ids, g.id] : ids.filter((id) => id !== g.id);
    await run(g.id, () => api(`/admin/users/${user.id}/subscription-groups`, "PUT", { profile_ids: next }), on ? `已分配「${g.name}」` : `已取消「${g.name}」`);
  }
  const note = (g: UserGroupItem) => [!g.enabled ? "已停用" : "", g.expires_at ? `到期 ${date(g.expires_at)}` : "", g.granted ? (g.last_used_at ? `最近更新 ${date(g.last_used_at)}` : "还没更新过") : ""].filter(Boolean).join(" · ") || undefined;
  return (
    <div className="stack mt-[var(--weir-gap-29)]">
      <SectionHeader title="订阅组" actions={<Link to="/groups?tab=profiles">管理订阅组</Link>} />
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => void q.refetch()} /> : items.length === 0 ? (
        <p className="m-0 text-meta text-[var(--weir-ink-3)]">还没有订阅组，先到 <Link to="/groups?tab=profiles&create=profile">外部订阅组</Link> 新建。</p>
      ) : (
        <Card className="overflow-hidden p-0">
          {items.map((g) => (
            <div className="weir-set-row" key={g.id}>
              <CheckboxField label={g.name} description={note(g)} disabled={isBusy()} checked={g.granted} onCheckedChange={(v) => void toggle(g, v)} />
              {isBusy(g.id) && <small className="text-meta text-[var(--weir-ink-3)]">正在保存</small>}
            </div>
          ))}
        </Card>
      )}
    </div>
  );
}
