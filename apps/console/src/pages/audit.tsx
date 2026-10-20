import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Button, Card, CardContent, Input, Select, SelectTrigger, SelectValue, SelectContent, SelectItem, Dialog, DialogContent, DialogTitle, DialogDescription } from "@migate/ui";
import { api } from "../api";
import { date, type Items } from "../lib/models";
import { State } from "../components/feedback";

type Audit = { id: number; actor: number; action: string; target: string; result: string; ip: string; created_at: string };
const resultLabels: Record<string, string> = { all: "全部结果", ok: "成功", denied: "拒绝", failed: "失败", queued: "已排队" };
export function AuditPage() {
  const [search, S] = useState("");
  const [result, F] = useState("all");
  const [page, P] = useState(1);
  const [detail, D] = useState<Audit | null>(null);
  const params = new URLSearchParams({ page: String(page), page_size: "20", q: search });
  if (result !== "all") params.set("result", result);
  const query = useQuery({ queryKey: ["audit-directory", params.toString()], queryFn: () => api("/admin/audit?" + params) as Promise<Items<Audit>> });
  return <div className="stack audit-directory">
    <div className="page-intro"><div><h2>操作记录</h2><p>追踪账户与设备变更。任务执行状态请以设备回执为准。</p></div><Button variant="outline" disabled={query.isFetching} onClick={() => query.refetch()}>刷新记录</Button></div>
    <Card><CardContent>
      <div className="table-toolbar">
        <Input aria-label="搜索审计记录" placeholder="搜索操作名称、目标或任务编号…" value={search} onChange={e => { S(e.target.value); P(1); }} />
        <Select value={result} onValueChange={v => { F(String(v)); P(1); }}><SelectTrigger aria-label="筛选审计结果"><SelectValue>{resultLabels[result]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(resultLabels).map(([key, label]) => <SelectItem key={key} value={key}>{label}</SelectItem>)}</SelectContent></Select>
      </div>
      {query.isPending ? <State loading /> : query.error ? <State error={query.error} onRetry={() => query.refetch()} /> : !query.data?.items.length ? <State empty="没有符合条件的操作记录" /> : query.data.items.map(item => <div className="list-row audit-row" key={item.id}>
        <div><strong>{item.action}</strong><small>{date(item.created_at)} · {item.actor ? `账户 #${item.actor}` : "系统 / 未登录"}</small><small>目标：{item.target || "未指定"}</small></div>
        <div className="actions"><span className={"tag " + (item.result === "ok" ? "good" : "")}>{resultLabels[item.result] ?? item.result}</span><Button size="sm" variant="outline" onClick={() => D(item)}>查看记录 #{item.id}</Button></div>
      </div>)}
      <div className="section-heading"><small>共 {query.data?.total ?? "…"} 条 · 第 {page} 页</small><div className="actions"><Button variant="outline" disabled={page === 1 || query.isFetching} onClick={() => P(page - 1)}>上一页</Button><Button variant="outline" disabled={!query.data || page * 20 >= (query.data.total ?? 0) || query.isFetching} onClick={() => P(page + 1)}>下一页</Button></div></div>
    </CardContent></Card>
    <Dialog open={!!detail} onOpenChange={v => !v && D(null)}><DialogContent><DialogTitle>审计记录 #{detail?.id}</DialogTitle><DialogDescription>此记录描述操作请求的结果；异步任务是否生效须查看执行回执。</DialogDescription>{detail && <div className="stack"><div className="detail-grid">
      <div><span>操作</span><strong>{detail.action}</strong></div><div><span>结果</span><strong>{resultLabels[detail.result] ?? detail.result}</strong></div>
      <div><span>操作者</span><strong>{detail.actor ? <Link to={"/users/" + detail.actor}>查看账户 #{detail.actor}</Link> : "系统 / 未登录"}</strong></div><div><span>来源 IP</span><strong>{detail.ip || "未知"}</strong></div>
      <div><span>目标</span><strong className="break-all">{detail.target || "未指定"}</strong></div><div><span>时间</span><strong>{date(detail.created_at)}</strong></div>
    </div>{detail.action.startsWith("task.") && <Link to={"/tasks?q=" + encodeURIComponent(detail.target)}>查看关联任务</Link>}</div>}</DialogContent></Dialog>
  </div>;
}
