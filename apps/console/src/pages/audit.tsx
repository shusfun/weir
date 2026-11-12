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
  action_label: string;
  actor_name: string;
  target: string;
  target_name: string;
  result: string;
  ip: string;
  created_at: string;
  trace_id?: string;
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
  const [range, setRange] = useState<"1" | "7" | "30">("7");
  const [kind, setKind] = useState<"human" | "all" | "failed" | "restart">("human");
  const [page, setPage] = useState(1);
  const params = new URLSearchParams({ page: String(page), page_size: "20", q: search });
  if (result !== "all") params.set("result", result);
  if (kind === "failed") params.set("result", "denied");
  // 热力格：取最近的 500 条在本地按小时聚合；超出时如实标注「只统计最近 500 条」
  const heat = useQuery({ queryKey: ["audit-heat"], queryFn: () => api("/admin/audit?page=1&page_size=100") as Promise<Items<Audit>> });
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
    list: { filters: { 搜索: search, 结果: resultLabels[result] ?? result }, total: query.data?.total ?? 0, items: (query.data?.items ?? []).slice(0, 20).map((a) => ({ type: "audit", name: a.action, id: a.id })) },
  });
  useConsoleContext(detail ? { object: { type: "audit", name: detail.action, id: detail.id }, dialog: { title: "审计记录 #" + detail.id }, form: [field("target", "目标", detail.target), field("result", "结果", detail.result)] } : null);
  return (
    <div className="stack audit-directory">
      <PageHeader
        size="data"
        title="操作审计"
        description={heat.data ? (() => {
          const today = heat.data.items.filter((a) => new Date(a.created_at).toDateString() === new Date().toDateString());
          const human = today.filter((a) => a.actor > 0);
          const denied = today.filter((a) => resultCode(a.result) === "denied").length;
          const restarts = today.filter(restarted).length;
          return <span className="weir-sum"><span>今天 <b>{human.length}</b> 次人的操作</span>{denied > 0 && <span className="text-[var(--weir-bad-ink)]">{denied} 次被拦下</span>}{restarts > 0 && <span className="text-[var(--weir-warn-ink)]">{restarts} 次重启了内核</span>}<span>{today.length - human.length} 条自动任务已折叠</span></span>;
        })() : "正在读取审计记录"}
        actions={
          <Button variant="outline" disabled={query.isFetching} onClick={() => query.refetch()}>
            刷新记录
          </Button>
        }
      />
      <Card className="px-[21px] py-[17px]">
        <div className="mb-[11px] flex flex-wrap items-center justify-between gap-3">
          <span className="text-meta text-[var(--weir-ink-2)]">最近 {range === "1" ? "24 小时" : `${range} 天`} · 每一格是 {range === "30" ? "4 小时" : "1 小时"}，越亮操作越多；有涟漪的是重启了内核</span>
          <Segmented aria-label="热力范围" value={range} onValueChange={setRange} options={[["1", "今天"], ["7", "7 天"], ["30", "30 天"]] as const} />
        </div>
        {heat.isPending || heat.error ? <State loading={heat.isPending} error={heat.error} onRetry={() => heat.refetch()} /> : <AuditHeat items={heat.data.items} days={Number(range)} />}
      </Card>
      <div className="flex flex-wrap items-center gap-[9px]">
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
          ) : !query.data?.items.filter((a) => kindMatch(a, kind)).length ? (
            <State empty="没有符合条件的操作记录" />
          ) : (
            <Table>
              <TableHeader><TableRow><TableHead className="w-[91px]">时间</TableHead><TableHead>操作者</TableHead><TableHead>操作</TableHead><TableHead>对象</TableHead><TableHead>结果</TableHead><TableHead>对内核的影响</TableHead><TableHead>trace</TableHead></TableRow></TableHeader>
              <TableBody>
                {query.data.items.filter((a) => kindMatch(a, kind)).map((item) => {
                  const code = resultCode(item.result);
                  return (
                    <TableRow key={item.id} {...dataObject({ type: "audit", id: item.id })} onClick={() => openDetail(item)}>
                      <TableCell className="font-[family-name:var(--weir-mono)]">{new Date(item.created_at).toLocaleTimeString("zh-CN", { hour12: false })}</TableCell>
                      <TableCell>{item.action.startsWith("ai.") || item.action.startsWith("ops.") ? <Badge variant="soft" tone="info">{item.actor ? "dsh · 管理员确认" : "dsh"}</Badge> : item.actor ? item.actor_name || "未知" : <span className="text-[var(--weir-ink-3)]">系统</span>}</TableCell>
                      <TableCell>{item.action_label}</TableCell>
                      <TableCell>{parseTarget(item.target) ? <AuditTarget item={item} /> : <span className="text-[var(--weir-ink-3)]">{item.target_name || "—"}</span>}</TableCell>
                      <TableCell>{code === "ok" ? <span className="text-meta text-[var(--weir-ink-3)]">成功</span> : <Badge variant="soft" tone={resultTones[code] ?? "neutral"}>{resultLabels[code] ?? item.result}</Badge>}</TableCell>
                      <TableCell>{restarted(item) ? <Badge variant="soft" tone="warn">重启了内核</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">—</span>}</TableCell>
                      <TableCell className="font-[family-name:var(--weir-mono)] text-[var(--weir-accent-text)]">{item.trace_id ? `${item.trace_id.slice(0, 4)}…${item.trace_id.slice(-4)}` : <span className="text-[var(--weir-ink-3)]">未知</span>}</TableCell>
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
                  <strong>{detail.action_label} <code className="font-normal">{detail.action}</code></strong>
                </div>
                <div>
                  <span>结果</span>
                  <strong>{resultLabels[resultCode(detail.result)] ?? detail.result}{resultCode(detail.result) !== detail.result ? <code className="ml-1 font-normal">{detail.result}</code> : null}</strong>
                </div>
                <div>
                  <span>操作者</span>
                  <strong>{detail.actor ? <ObjectLink to={{ type: "user", id: detail.actor, name: detail.actor_name }} /> : "系统 / 未登录"}</strong>
                </div>
                <div>
                  <span>来源 IP</span>
                  <strong>{detail.ip || "未知"}</strong>
                </div>
                <div>
                  <span>目标</span>
                  <strong className="break-all">{detail.target ? <><AuditTarget item={detail} />{detail.target_name !== detail.target ? <code className="ml-1 font-normal">{detail.target}</code> : null}</> : "未指定"}</strong>
                </div>
                <div>
                  <span>时间</span>
                  <strong>{date(detail.created_at)}</strong>
                </div>
                <div>
                  <span>关联编号</span>
                  <code className="break-all">{detail.trace_id || "未知"}</code>
                </div>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
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

function kindMatch(a: Audit, kind: string) {
  if (kind === "human") return a.actor > 0 || a.action.startsWith("ai.");
  if (kind === "restart") return restarted(a);
  return true;
}

// 热力格（设计稿 CAudit）：按小时（30 天时按 4 小时）聚合，越亮越多；有重启的格子是黄色带光圈。
function AuditHeat({ items, days }: { items: Audit[]; days: number }) {
  const step = days === 30 ? 4 : 1;
  const cells = Math.min(84, Math.round((days * 24) / step / (days === 7 ? 2 : 1)));
  const span = (days * 24 * 3600e3) / cells;
  const end = Date.now();
  const counts = Array.from({ length: cells }, () => ({ n: 0, restart: false }));
  for (const a of items) {
    const t = new Date(a.created_at).getTime();
    const i = Math.floor((t - (end - days * 24 * 3600e3)) / span);
    if (i >= 0 && i < cells) {
      counts[i].n += 1;
      if (restarted(a)) counts[i].restart = true;
    }
  }
  const max = Math.max(1, ...counts.map((c) => c.n));
  const labels = [0, 1, 2].map((k) => new Date(end - days * 24 * 3600e3 + (k * days * 24 * 3600e3) / 3)).map((d) => `${d.getMonth() + 1}/${d.getDate()}`);
  return (
    <>
      <div className="weir-heat" style={{ gridTemplateColumns: `repeat(${cells}, minmax(0, 1fr))` }} role="img" aria-label={`最近 ${days} 天的操作分布`}>
        {counts.map((c, i) => <i key={i} data-restart={c.restart || undefined} style={{ opacity: c.n ? 0.37 + (c.n / max) * 0.63 : undefined }} data-on={c.n ? "" : undefined} />)}
      </div>
      <div className="mt-[7px] flex justify-between text-fine text-[var(--weir-ink-3)]">{labels.map((l) => <span key={l}>{l}</span>)}<span>今天</span></div>
      {items.length >= 100 && <div className="mt-[5px] text-fine text-[var(--weir-ink-3)]">只统计最近 100 条记录，更早的格子可能偏暗</div>}
    </>
  );
}

// 结果可能带附加信息（如「ok chains=0」）：标签看第一个词，完整内容在详情里。
function resultCode(result: string) {
  return result.split(" ")[0];
}
