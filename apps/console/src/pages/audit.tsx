// 操作记录：账户、节点、订阅与设置的变更，可按结果筛选并查看单条详情。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  SearchInput,
  Badge,
  PageHeader,
  Pager,
  Button,
  SkeletonList,
  Card,
  CardContent,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@weir/ui";
import { api } from "../api";
import { date, type Items } from "../lib/models";
import { State } from "../components/feedback";
import { ObjectLink, dataObject } from "../components/object-link";
import { parseTarget } from "../lib/objects";
import { field, useConsoleContext } from "../lib/console-context";

type Audit = {
  id: number;
  actor: number;
  action: string;
  target: string;
  result: string;
  ip: string;
  created_at: string;
};
const resultLabels: Record<string, string> = {
  all: "全部结果",
  ok: "成功",
  denied: "拒绝",
  failed: "失败",
  queued: "已排队",
};
const resultTones: Record<string, "ok" | "bad" | "warn"> = { ok: "ok", denied: "bad", failed: "bad", queued: "warn" };
// 地址栏：?q= 是筛选（如从对象跳过来看它的记录：?q=node:12），?audit=编号 打开这条记录的详情。
export function AuditPage() {
  const [url, setUrl] = useSearchParams();
  const [search, setSearch] = useState(url.get("q") ?? "");
  const [result, setResult] = useState("all");
  const [page, setPage] = useState(1);
  const params = new URLSearchParams({ page: String(page), page_size: "20", q: search });
  if (result !== "all") params.set("result", result);
  const query = useQuery({
    queryKey: ["audit-directory", params.toString()],
    queryFn: () => api("/admin/audit?" + params) as Promise<Items<Audit>>,
  });
  const detailId = Number(url.get("audit")) || 0;
  const one = useQuery({
    queryKey: ["audit-one", detailId],
    enabled: detailId > 0 && !query.data?.items.some((x) => x.id === detailId),
    queryFn: async () => ((await api("/admin/audit?id=" + detailId)) as Items<Audit>).items[0] ?? null,
  });
  const detail = detailId ? query.data?.items.find((x) => x.id === detailId) ?? one.data ?? null : null;
  const openDetail = (a: Audit) => setUrl((p) => { p.set("audit", String(a.id)); return p; });
  const closeDetail = () => setUrl((p) => { p.delete("audit"); return p; }, { replace: true });
  useConsoleContext({
    page: "操作审计",
    list: { filters: { 搜索: search, 结果: resultLabels[result] ?? result }, selected: query.data?.total ?? 0, items: (query.data?.items ?? []).slice(0, 20).map((a) => ({ type: "audit", name: a.action, id: a.id })) },
  });
  useConsoleContext(detail ? { object: { type: "audit", name: detail.action, id: detail.id }, dialog: { title: "审计记录 #" + detail.id }, form: [field("target", "目标", detail.target), field("result", "结果", detail.result)] } : null);
  return (
    <div className="stack audit-directory">
      <PageHeader
        title="操作记录"
        description="追踪账户、节点、订阅与设置的变更；节点是否生效以 VPS 回报的配置版本为准。"
        actions={
          <Button variant="outline" disabled={query.isFetching} onClick={() => query.refetch()}>
            刷新记录
          </Button>
        }
      />
      <Card className="table-card">
        <div className="table-toolbar">
          <SearchInput
            aria-label="搜索审计记录"
            placeholder="搜索操作名称、目标或任务编号…"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(1);
            }}
          />
          <Select
            value={result}
            onValueChange={(v) => {
              setResult(String(v));
              setPage(1);
            }}
          >
            <SelectTrigger className="min-w-[130px]" aria-label="筛选审计结果">
              <SelectValue>{resultLabels[result]}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {Object.entries(resultLabels).map(([key, label]) => (
                <SelectItem key={key} value={key}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <CardContent>
          {query.isPending ? (
            <State loading skeleton={<SkeletonList rows={6} lines={3} rowClassName="list-row audit-row" />} />
          ) : query.error ? (
            <State error={query.error} onRetry={() => query.refetch()} />
          ) : !query.data?.items.length ? (
            <State empty="没有符合条件的操作记录" />
          ) : (
            query.data.items.map((item) => (
              <div className="list-row audit-row" key={item.id} {...dataObject({ type: "audit", id: item.id })}>
                <div>
                  <strong>{item.action}</strong>
                  <small>
                    {date(item.created_at)} · {item.actor ? <ObjectLink to={{ type: "user", id: item.actor, name: `账户 #${item.actor}` }} /> : "系统 / 未登录"}
                  </small>
                  <small>目标：<AuditTarget target={item.target} /></small>
                </div>
                <div className="actions">
                  <Badge variant="soft" tone={resultTones[item.result] ?? "neutral"}>
                    {resultLabels[item.result] ?? item.result}
                  </Badge>
                  <Button size="sm" variant="outline" onClick={() => openDetail(item)}>
                    查看记录 #{item.id}
                  </Button>
                </div>
              </div>
            ))
          )}
          <Pager
            variant="inline"
            page={page}
            pageSize={20}
            total={query.data?.total}
            busy={query.isFetching}
            onPage={setPage}
          />
        </CardContent>
      </Card>
      <Dialog open={!!detailId && (!!detail || one.isFetched)} onOpenChange={(v) => !v && closeDetail()}>
        <DialogContent>
          <DialogTitle>审计记录 #{detail?.id ?? detailId}</DialogTitle>
          <DialogDescription>此记录描述操作请求的结果；异步任务是否生效须查看执行回执。</DialogDescription>
          {!detail && <State empty="没有这条记录" detail="编号不存在，或已超出保留期。" />}
          {detail && (
            <div className="stack">
              <div className="detail-grid">
                <div>
                  <span>操作</span>
                  <strong>{detail.action}</strong>
                </div>
                <div>
                  <span>结果</span>
                  <strong>{resultLabels[detail.result] ?? detail.result}</strong>
                </div>
                <div>
                  <span>操作者</span>
                  <strong>{detail.actor ? <ObjectLink to={{ type: "user", id: detail.actor, name: `账户 #${detail.actor}` }} /> : "系统 / 未登录"}</strong>
                </div>
                <div>
                  <span>来源 IP</span>
                  <strong>{detail.ip || "未知"}</strong>
                </div>
                <div>
                  <span>目标</span>
                  <strong className="break-all"><AuditTarget target={detail.target} /></strong>
                </div>
                <div>
                  <span>时间</span>
                  <strong>{date(detail.created_at)}</strong>
                </div>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

// AuditTarget：目标是「类型:编号」时可以点过去；对象已删除时打开后会如实提示。
function AuditTarget({ target }: { target: string }) {
  if (!target) return <>未指定</>;
  const ref = parseTarget(target);
  return ref ? <ObjectLink to={ref}>{target}</ObjectLink> : <>{target}</>;
}
