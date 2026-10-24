import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { SearchInput, Badge, PageHeader, Pager, Button, SkeletonList, Card, CardContent, Select, SelectTrigger, SelectValue, SelectContent, SelectItem, Dialog, DialogContent, DialogTitle, DialogDescription } from "@migate/ui";
import { api } from "../api";
import { date, type Items } from "../lib/models";
import { State } from "../components/feedback";

type Audit = { id: number; actor: number; action: string; target: string; result: string; ip: string; created_at: string };
const resultLabels: Record<string, string> = { all: "全部结果", ok: "成功", denied: "拒绝", failed: "失败", queued: "已排队" };
const resultTones: Record<string, "ok" | "bad" | "warn"> = { ok: "ok", denied: "bad", failed: "bad", queued: "warn" };
export function AuditPage() {
  const [search, S] = useState("");
  const [result, F] = useState("all");
  const [page, P] = useState(1);
  const [detail, D] = useState<Audit | null>(null);
  const params = new URLSearchParams({ page: String(page), page_size: "20", q: search });
  if (result !== "all") params.set("result", result);
  const query = useQuery({ queryKey: ["audit-directory", params.toString()], queryFn: () => api("/admin/audit?" + params) as Promise<Items<Audit>> });
  return <div className="stack audit-directory">
    <PageHeader title="操作记录" description="追踪账户、节点、订阅与设置的变更；节点是否生效以 VPS 回报的配置版本为准。" actions={<Button variant="outline" disabled={query.isFetching} onClick={() => query.refetch()}>刷新记录</Button>} />
    <Card className="table-card"><div className="table-toolbar">
        <SearchInput aria-label="搜索审计记录" placeholder="搜索操作名称、目标或任务编号…" value={search} onChange={e => { S(e.target.value); P(1); }} />
        <Select value={result} onValueChange={v => { F(String(v)); P(1); }}><SelectTrigger className="min-w-[130px]" aria-label="筛选审计结果"><SelectValue>{resultLabels[result]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(resultLabels).map(([key, label]) => <SelectItem key={key} value={key}>{label}</SelectItem>)}</SelectContent></Select>
      </div><CardContent>
      {query.isPending ? <State loading skeleton={<SkeletonList rows={6} lines={3} rowClassName="list-row audit-row" />} /> : query.error ? <State error={query.error} onRetry={() => query.refetch()} /> : !query.data?.items.length ? <State empty="没有符合条件的操作记录" /> : query.data.items.map(item => <div className="list-row audit-row" key={item.id}>
        <div><strong>{item.action}</strong><small>{date(item.created_at)} · {item.actor ? `账户 #${item.actor}` : "系统 / 未登录"}</small><small>目标：{item.target || "未指定"}</small></div>
        <div className="actions"><Badge variant="soft" tone={resultTones[item.result] ?? "neutral"}>{resultLabels[item.result] ?? item.result}</Badge><Button size="sm" variant="outline" onClick={() => D(item)}>查看记录 #{item.id}</Button></div>
      </div>)}
      <Pager variant="inline" page={page} pageSize={20} total={query.data?.total} busy={query.isFetching} onPage={P} />
    </CardContent></Card>
    <Dialog open={!!detail} onOpenChange={v => !v && D(null)}><DialogContent><DialogTitle>审计记录 #{detail?.id}</DialogTitle><DialogDescription>此记录描述操作请求的结果；异步任务是否生效须查看执行回执。</DialogDescription>{detail && <div className="stack"><div className="detail-grid">
      <div><span>操作</span><strong>{detail.action}</strong></div><div><span>结果</span><strong>{resultLabels[detail.result] ?? detail.result}</strong></div>
      <div><span>操作者</span><strong>{detail.actor ? <Link to="/users">账户 #{detail.actor}</Link> : "系统 / 未登录"}</strong></div><div><span>来源 IP</span><strong>{detail.ip || "未知"}</strong></div>
      <div><span>目标</span><strong className="break-all">{detail.target || "未指定"}</strong></div><div><span>时间</span><strong>{date(detail.created_at)}</strong></div>
    </div></div>}</DialogContent></Dialog>
  </div>;
}
