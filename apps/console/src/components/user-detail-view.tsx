import { useQueryClient } from "@tanstack/react-query";
import { State } from "./feedback";
// 用户详情页（设计稿 BUserDetail）：头像行 + 一句结论 + 右侧用量环；标签页：概览（能用的节点、额度与期限）、链接、链式代理、流量、记录。
// 账户结论由中心统一提供；用量环在不限额度时画成未知。
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Avatar, Button, Card, TrafficRing, trafficReading, ObjectHeader, SectionHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
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

function conclusion(u: UserItem): [string, string] {
  const name = u.name || u.email.split("@")[0];
  return [`${name} · ${u.object_state?.label || "状态未知"}`, u.object_state?.reason || "中心尚未提供状态结论"];

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
          <TabsTab value="links">链接 {user.links}</TabsTab>
          <TabsTab value="chains">链式代理 {user.chains}</TabsTab>
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
        </TabsPanel>
        <TabsPanel value="links"><LinkPanel base={`/admin/users/${user.id}`} planEnds={user.expires_at} showCredentials inlineQr={inlineQr} /></TabsPanel>
        <TabsPanel value="chains"><ChainPanel base={`/admin/users/${user.id}`} admin /></TabsPanel>
        <TabsPanel value="traffic"><HistoryCharts path={`/admin/users/${user.id}/history`} /><UsageRanking path={`/admin/users/${user.id}/usage`} /></TabsPanel>
        <TabsPanel value="log"><p className="m-0 text-body text-[var(--weir-ink-2)]">这个用户的操作记录在 <Link to={`/ops/audit?q=user:${user.id}`}>操作审计</Link>。</p></TabsPanel>
      </Tabs>
      <UserForm value={editing ? user : null} nodes={nodes.data ?? []} close={() => setEditing(false)} saved={(updated) => { client.setQueryData<UserItem[]>(["users"], old => old?.map(u => u.id === updated.id ? { ...u, ...updated } : u)); void client.invalidateQueries({ queryKey: ["users"] }); setEditing(false); }} />
      {granting.value === String(user.id) && <CatalogGrants user={user} close={granting.close} />}
    </div>
  );
}
