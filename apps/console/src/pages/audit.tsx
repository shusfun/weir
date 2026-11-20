// 操作记录：账户、节点、订阅与设置的变更，可按结果筛选并查看单条详情。
import { useState } from "react";
import { useQuery } from "../lib/query";
import { useSearchParams } from "react-router-dom";
import {
  SearchInput,
  Badge,
  PageHeader,
  Pager,
  Segmented,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Button,
  SkeletonList,
  Card,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  Collapsible,
  CodeBlock,
} from "@weir/ui";
import { api } from "../api";
import { date, type Items } from "../lib/models";
import { State } from "../components/feedback";
import { ObjectLink, dataObject } from "../components/object-link";
import { parseTarget } from "../lib/objects";
import { field, useConsoleContext } from "../lib/console-context";
import { describeResult, originOf, originText, resultLabels } from "../lib/audit-view";

type Audit = {
  id: number;
  actor: number;
  action: string;
  action_label: string;
  actor_name: string;
  target: string;
  target_name: string;
  result: string;
  ip: string;
  created_at: string;
  trace_id?: string;
};
const resultTones: Record<string, "ok" | "bad" | "warn"> = { ok: "ok", denied: "bad", failed: "bad", queued: "warn" };
// 地址栏：?q= 是筛选（如从对象跳过来看它的记录：?q=node:12），?audit=编号 打开这条记录的详情。
export function AuditPage() {
  const [url, setUrl] = useSearchParams();
  const [search, setSearch] = useState(url.get("q") ?? "");
  const [result, setResult] = useState("all");
  const [range, setRange] = useState<"1" | "7" | "30">("7");
  const [kind, setKind] = useState<"human" | "all" | "failed" | "restart">("human");
  const [page, setPage] = useState(1);
  const params = new URLSearchParams({ page: String(page), page_size: "20", q: search });
  if (result !== "all") params.set("result", result);
  params.set("kind", kind);
  const heat = useQuery({ queryKey: ["audit-heat", range], queryFn: () => {
    const today = new Date(); today.setHours(0, 0, 0, 0);
    return api(`/admin/audit/summary?days=${range}&today_from=${encodeURIComponent(today.toISOString())}`) as Promise<AuditSummary>;
  } });
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
    list: { filters: { 范围: kind, 搜索: search, 结果: resultLabels[result] ?? result }, total: query.data?.total ?? 0, items: (query.data?.items ?? []).slice(0, 20).map((a) => ({ type: "audit", name: a.action, id: a.id })) },
  });
  useConsoleContext(detail ? { object: { type: "audit", name: detail.action, id: detail.id }, dialog: { title: "审计记录 #" + detail.id }, form: [field("target", "目标", detail.target), field("result", "结果", detail.result)] } : null);
  return (
    <div className="stack audit-directory">
      <PageHeader
        size="data"
        title="操作审计"
        description={heat.data ? (() => {
          const { human, automatic, failed, restarts } = heat.data.today;
          return <span className="weir-sum"><span>今天 <b>{human}</b> 次人的操作</span>{failed > 0 && <span className="text-[var(--weir-bad-ink)]">{failed} 次失败或被拦下</span>}{restarts > 0 && <span>{restarts} 次重启了内核</span>}<span>{automatic} 条自动任务</span></span>;
        })() : "正在读取审计记录"}
        actions={
          <Button variant="outline" disabled={query.isFetching} onClick={() => { void query.refetch(); void heat.refetch(); }}>
            刷新记录
          </Button>
        }
      />
      <Card className="px-[var(--weir-gap-21)] py-[var(--weir-gap-17)]">
        <div className="mb-[11px] flex flex-wrap items-center justify-between gap-3">
          <span className="text-meta text-[var(--weir-ink-2)]">最近 {range === "1" ? "24 小时" : `${range} 天`}，每格 {range === "30" ? "4 小时" : "1 小时"}；有涟漪的是重启了内核</span>
          <Segmented aria-label="热力范围" value={range} onValueChange={setRange} options={[["1", "今天"], ["7", "7 天"], ["30", "30 天"]] as const} />
        </div>
        {heat.isPending || heat.error ? <State loading={heat.isPending} error={heat.error} onRetry={() => heat.refetch()} /> : <AuditHeat summary={heat.data} days={Number(range)} />}
      </Card>
      <div className="flex flex-wrap items-center gap-[var(--weir-gap-9)]">
        <Segmented aria-label="审计范围" value={kind} onValueChange={(v) => { setKind(v); setPage(1); }} options={[["human", "人的操作"], ["all", "含自动任务"], ["failed", "失败与拦下"], ["restart", "重启了内核"]] as const} />
        <span className="flex-1" />
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
      <Card className="table-card overflow-hidden p-0">
          {query.isPending ? (
            <State loading skeleton={<SkeletonList rows={6} lines={3} rowClassName="list-row audit-row" />} />
          ) : query.error ? (
            <State error={query.error} onRetry={() => query.refetch()} />
          ) : !query.data?.items.length ? (
            <State empty="没有符合条件的操作记录" />
          ) : (
            <Table>
              <TableHeader><TableRow><TableHead className="w-[91px]">时间</TableHead><TableHead>操作者</TableHead><TableHead>操作</TableHead><TableHead>对象</TableHead><TableHead>结果</TableHead><TableHead>对内核的影响</TableHead></TableRow></TableHeader>
              <TableBody>
                {query.data.items.map((item) => {
                  const code = resultCode(item.result);
                  return (
                    <TableRow key={item.id} {...dataObject({ type: "audit", id: item.id })} onClick={() => openDetail(item)}>
                      <TableCell className="font-[family-name:var(--weir-mono)]">{new Date(item.created_at).toLocaleTimeString("zh-CN", { hour12: false })}</TableCell>
                      <TableCell><AuditOrigin item={item} /></TableCell>
                      <TableCell>{item.action_label}</TableCell>
                      <TableCell>{parseTarget(item.target) ? <AuditTarget item={item} /> : <span className="text-[var(--weir-ink-3)]">{item.target_name || "—"}</span>}</TableCell>
                      <TableCell>{code === "ok" ? <span className="text-meta text-[var(--weir-ink-3)]">成功</span> : <Badge variant="soft" tone={resultTones[code] ?? "neutral"}>{describeResult(item.result).label}</Badge>}</TableCell>
                      <TableCell>{restarted(item) ? <Badge variant="soft" tone="warn">重启了内核</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">—</span>}</TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
          <Pager
            variant="inline"
            page={page}
            pageSize={20}
            total={query.data?.total}
            busy={query.isFetching}
            onPage={setPage}
          />
      </Card>
      <Dialog open={!!detailId && (!!detail || one.isFetched)} onOpenChange={(v) => !v && closeDetail()}>
        <DialogContent>
          <DialogTitle>审计记录 #{detail?.id ?? detailId}</DialogTitle>
          <DialogDescription>此记录描述操作请求的结果；异步任务是否生效须查看执行回执。</DialogDescription>
          {!detail && (one.error ? <State error={one.error} onRetry={() => one.refetch()} /> : <State empty="没有这条记录" detail="编号不存在，或已超出保留期。" />)}
          {detail && (
            <div className="stack">
              <div className="detail-grid">
                <div>
                  <span>操作</span>
                  <strong>{detail.action_label}</strong>
                </div>
                <div>
                  <span>结果</span>
                  <strong>{[describeResult(detail.result).label, ...describeResult(detail.result).extras].join(" · ")}</strong>
                </div>
                <div>
                  <span>发起方</span>
                  <strong>{originText[originOf(detail).kind]}</strong>
                </div>
                <div>
                  <span>{originOf(detail).kind === "dsh-confirmed" ? "确认人" : "操作者"}</span>
                  <strong>{detail.actor ? <ObjectLink to={{ type: "user", id: detail.actor, name: detail.actor_name }} /> : "无"}</strong>
                </div>
                <div>
                  <span>来源 IP</span>
                  <strong>{detail.ip || "未知"}</strong>
                </div>
                <div>
                  <span>目标</span>
                  <strong className="break-all">{detail.target ? <AuditTarget item={detail} /> : "未指定"}</strong>
                </div>
                <div>
                  <span>时间</span>
                  <strong>{date(detail.created_at)}</strong>
                </div>
              </div>
              <Collapsible title="原始记录">
                <CodeBlock className="whitespace-pre-wrap break-all text-meta">{`操作代码 ${detail.action}\n结果原文 ${detail.result || "未记录"}\n目标代码 ${detail.target || "未指定"}\n关联编号 ${detail.trace_id || "旧记录未保存"}`}</CodeBlock>
              </Collapsible>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

// AuditOrigin：区分 dsh 起草（经管理员确认 / 自动执行）、管理员本人与系统任务
function AuditOrigin({ item }: { item: Audit }) {
  const origin = originOf(item);
  if (origin.kind === "dsh-confirmed" || origin.kind === "dsh-auto") return <Badge variant="soft" tone={origin.kind === "dsh-auto" ? "warn" : "info"}>{origin.label}</Badge>;
  return origin.kind === "system" ? <span className="text-[var(--weir-ink-3)]">系统</span> : <>{origin.label}</>;
}

// AuditTarget：显示对象名称；目标是「类型:编号」时可以点过去，对象已删除时打开后会如实提示。
function AuditTarget({ item }: { item: Audit }) {
  const ref = parseTarget(item.target);
  return ref ? <ObjectLink to={ref}>{item.target_name}</ObjectLink> : <>{item.target_name}</>;
}

// 只有审计里明确写了重启内核的操作才算「对内核有影响」；其余写 —，不猜。
function restarted(a: Audit) {
  return a.action === "gate.kernel.restart" || a.action.endsWith(".restart") || /restart|重启/.test(a.result);
}

type AuditSummary = { from: string; to: string; today: { human: number; automatic: number; failed: number; restarts: number }; buckets: { at: string; total: number; restarts: number }[] };

function AuditHeat({ summary, days }: { summary: AuditSummary; days: number }) {
  const max = Math.max(1, ...summary.buckets.map(c => c.total));
  const total = summary.buckets.reduce((sum, c) => sum + c.total, 0);
  const restarts = summary.buckets.reduce((sum, c) => sum + c.restarts, 0);
  const busiest = summary.buckets.reduce<AuditSummary["buckets"][number] | null>((top, c) => c.total > (top?.total ?? 0) ? c : top, null);
  return <><p className="weir-sum m-0 mb-[var(--weir-gap-7)] text-meta"><span>共 <b>{total}</b> 次操作</span>{busiest && <span>最多在 {date(busiest.at)}：<b>{busiest.total}</b> 次</span>}<span>重启内核 <b>{restarts}</b> 次</span></p><div className="weir-heat" style={{ gridTemplateColumns: `repeat(${summary.buckets.length}, minmax(0, 1fr))` }} role="img" aria-label={`最近 ${days} 天的完整操作分布`}>
    {summary.buckets.map(c => <i key={c.at} title={`${date(c.at)} · ${c.total} 次操作 · ${c.restarts} 次重启`} data-restart={c.restarts > 0 || undefined} style={c.restarts > 0 ? undefined : { opacity: c.total ? 0.37 + c.total / max * 0.63 : undefined }} data-on={c.total ? "" : undefined} />)}
  </div><div className="mt-[var(--weir-gap-7)] flex justify-between text-fine text-[var(--weir-ink-3)]"><span>{date(summary.from)}</span><span>{date(summary.to)}</span></div></>;
}

// 结果可能带附加信息（如「ok chains=0」）：标签看第一个词，完整内容在详情里。
function resultCode(result: string) {
  return result.split(" ")[0];
}
