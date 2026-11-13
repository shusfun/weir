import { useQueryClient } from "@tanstack/react-query";
import { State } from "./feedback";
// 用户详情页（设计稿 BUserDetail）：头像行 + 一句结论 + 右侧用量环；标签页：概览（能用的节点、额度与期限）、链接、链式代理、流量、记录。
// 结论只按真实数据：停用、到期、用量 ≥ 90% 才写「要处理」，否则写正常；用量环在不限额度时画成未知。
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Avatar, Button, Card, TrafficRing, trafficReading, ObjectHeader, SectionHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { useQuery } from "../lib/query";
import { api } from "../api";
import { bytes, date } from "../lib/models";
import { ChainPanel, LinkPanel } from "./link-panel";
import { HistoryCharts } from "./history-charts";
import { UserForm } from "./user-form";
import { protocolNames, type NodeItem, type UserItem } from "../lib/core";

function conclusion(u: UserItem): [string, string] {
  const name = u.name || u.email.split("@")[0];
  if (!u.enabled) return [`${name} 已停用`, "停用后拉不到节点，已导入的客户端也不能用。"];
  const left = u.expires_at ? Math.ceil((new Date(u.expires_at).getTime() - Date.now()) / 86400e3) : null;
  if (u.expires_at && new Date(u.expires_at).getTime() <= Date.now()) return [`${name} 已经到期`, `到期于 ${date(u.expires_at!)}，续期后恢复。`];
  if (left != null && left < 7) return [`${name} ${left} 天后到期`, `到期后拉不到节点；续期在右上角。`];
  if (!u.unlimited && u.quota > 0 && u.used >= u.quota) return [`${name} 的流量已用完`, `本月已用 ${Math.round(u.used / u.quota * 100)}%，需要补充额度${u.monthly_reset ? "或等待每月重置" : ""}。`];
  if (!u.unlimited && u.quota > 0 && u.used / u.quota >= 0.9) return [`${name} 的流量\n快用完了`, `本月已用 ${Math.round((u.used / u.quota) * 100)}%。用完后会自动停用，并断开现有的连接${u.monthly_reset ? "；每月重置" : ""}。`];
  if (!Number.isFinite(u.used) || (!u.unlimited && !Number.isFinite(u.quota))) return [`${name} 的用量未知`, "尚未取得有效流量数据。"];
  if (!u.unlimited && u.quota === 0) return [`${name} 没有可用额度`, "当前额度为零，请核对套餐设置。"];
  return [`${name} 用得正常`, u.unlimited ? `不限额度，本月已用 ${bytes(u.used)}。` : `本月流量 ${trafficReading({ used: u.used, quota: u.quota }).text}。`];
}

export function UserDetailView({ user, users }: { user: UserItem; users: UserItem[] }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [editing, setEditing] = useState(false);
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const tab = params.get("tab") || "overview";
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
          <Button variant="ghost" disabled={idx <= 0} onClick={() => navigate(`/users/${users[idx - 1].id}`)}>上一个</Button>
          <Button variant="ghost" disabled={idx < 0 || idx >= users.length - 1} onClick={() => navigate(`/users/${users[idx + 1].id}`)}>下一个</Button>
          <Button variant="outline" onClick={() => setEditing(true)}>编辑</Button>
        </>}
        tone={!user.enabled ? "off" : title.includes("正常") ? "ok" : "warn"}
        status={<span className="flex items-center gap-[9px]"><Avatar name={user.name || user.email} /><span>{user.role === "admin" ? "管理员" : "用户"} · {user.email}<br />{user.created_at ? `${date(user.created_at)} 创建` : "创建时间未知"}</span></span>}
        title={<span className="whitespace-pre-line">{title}</span>}
        banner={<p className="m-0 max-w-[521px] text-lead text-[var(--weir-ink-2)]">{lead}</p>}
        aside={<div className="weir-user-traffic-aside"><TrafficRing used={user.used} quota={user.quota} unlimited={user.unlimited} /></div>}
      />
      <Tabs value={tab} onValueChange={(v) => setParams((p) => { p.set("tab", String(v)); return p; }, { replace: true })}>
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
              <SectionHeader title="能用的节点" actions={<Button size="sm" variant="ghost" onClick={() => setEditing(true)}>调整授权</Button>} />
              {nodes.error ? <State error={nodes.error} onRetry={() => void nodes.refetch()} /> : mine.length === 0 ? <p className="m-0 text-meta text-[var(--weir-ink-3)]">{nodes.isPending ? "正在读取节点" : "还没有分配节点"}</p> : (
                <div className="grid gap-[13px] sm:grid-cols-3">
                  {mine.map((n) => (
                    <Link key={n.id} to={`/nodes/${n.id}`} className="weir-mini-card">
                      <small>{n.gate_name ?? "VPS 未知"}</small>
                      <b className="block truncate">{protocolNames[n.protocol] ?? n.name}</b>
                      <small className="font-[family-name:var(--weir-mono)]">端口 {n.port || "未知"}</small>
                    </Link>
                  ))}
                </div>
              )}
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
        <TabsPanel value="links"><LinkPanel base={`/admin/users/${user.id}`} planEnds={user.expires_at} showCredentials /></TabsPanel>
        <TabsPanel value="chains"><ChainPanel base={`/admin/users/${user.id}`} admin /></TabsPanel>
        <TabsPanel value="traffic"><HistoryCharts path={`/admin/users/${user.id}/history`} /></TabsPanel>
        <TabsPanel value="log"><p className="m-0 text-body text-[var(--weir-ink-2)]">这个用户的操作记录在 <Link to={`/ops/audit?q=user:${user.id}`}>操作审计</Link>。</p></TabsPanel>
      </Tabs>
      <UserForm value={editing ? user : null} nodes={nodes.data ?? []} close={() => setEditing(false)} saved={(updated) => { client.setQueryData<UserItem[]>(["users"], old => old?.map(u => u.id === updated.id ? { ...u, ...updated } : u)); void client.invalidateQueries({ queryKey: ["users"] }); setEditing(false); }} />
    </div>
  );
}
