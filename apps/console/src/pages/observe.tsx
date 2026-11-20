// 运行观测：按「有什么问题」组织。每个分区先列失败、待处理的条目，正常条目在后；
// 原始结果码映射成中文，trace、版本号、模型等内部字段和外部数据原文收进每条的「详情」。
import { useSources } from "../lib/subscription-sources";
import { useState, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { Badge, Button, Checkbox, CodeBlock, Collapsible, Tabs, TabsList, TabsPanel, TabsTab, Timeline, TimelineItem, TraceSheet, useToast } from "@weir/ui";
import { X } from "lucide-react";
import { OpsOverview } from "../features/observability/ops-overview";
import { HealthOverview, HotspotDetails } from "../features/observability/health-overview";
import { api } from "../api";
import { useQuery, queryClient } from "../lib/query";
import { State } from "../components/feedback";
import { date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import { DiagnosticExport, selectionKey, type DiagnosticSelection } from "../components/diagnostic-export";
import { ProtectionActions } from "../features/observability/security/protection-actions";
import { BlocksCard, EventsCard } from "../components/defense";
import { useDsh } from "../components/dsh";
import { SystemHealth } from "../features/observability/system/system-health";
import { WorkerPolicy } from "../features/observability/system/worker-policy";
import { WorkerStatistics, type WorkerStatisticsData } from "../features/observability/system/worker-statistics";

type WorkerTask = { task: { id: string; kind: string; trace_id: string; attempt: number; deadline: string }; status: string; gate_id: number; candidates: number[]; error?: { code: string; origin: string } };
type Gate = { id: number; name: string; online: boolean; last_seen_at?: string; version?: string; host?: Record<string, unknown> };
type Audit = { id: number; action: string; action_label: string; target_name: string; result: string; created_at: string };
type SubscriptionRun = { id: string; trace_id: string; profile_id: number; started: string; finished: string; outcome: string; complete: boolean; stages?: { profile_id: number; source_id?: number; operation: string; started: string; finished: string; outcome: string; error_code?: string; origin: string; cache_fallback?: boolean }[] };
type Timeline = { records: { task_id: string; trace_id: string; kind: string; attempt: number; stage: string; started: string; finished: string; outcome: string; error_code?: string; origin: string }[]; collected_at: string; complete: boolean; missing_reason?: string };
type Analysis = { id: string; trace_id: string; snapshot_id: string; profile_version: string; dsh_version: string; items: string[]; tool_calls: string[] | null; status: string; code?: string; created_at: string; external_data_not_instructions: { conclusion: string } };
type Observation = { id: string; point: string; resource: string; trace_id: string; created_at: string; profile_version: string; dsh_status: string; dsh_code?: string; verdict?: { outcome: string; confidence: number; threshold: number; question: string; code?: string; attempts: number; model?: string; model_version?: string; runtime_version?: string; execution?: string; gate_id?: number }; external_data_not_instructions: { text: string; conclusion?: string } };
type ObservationDay = { day: string; timezone: string; point: string; counts: Record<string, number> };

// 分区按「先看哪里有问题」排序；label 同时是教程与 dsh 上下文的键，不随意改名
const tabs = [
  { key: "overview", label: "总览" },
  { key: "security", label: "安全" },
  { key: "subscriptions", label: "订阅链路" },
  { key: "system", label: "系统状态" },
  { key: "accounts", label: "注册兑换" },
  { key: "laya", label: "Laya 明细" },
  { key: "policy", label: "策略版本" },
] as const;

const known = (value?: string) => value && value !== "unknown" ? value : "未知";
const taskKind: Record<string, string> = { "subscription.fetch": "订阅拉取", "release.query": "发布查询", "laya.classify": "Laya 判断", "laya.install": "Laya 安装", "laya.remove": "Laya 移除", "trace.read": "读取 Gate 时间线", "core.restart": "重启内核", "core.install": "安装固定版本内核", "asset.fetch": "文件兜底中转" };
const taskStatus: Record<string, string> = { pending: "等待执行", leased: "执行中", cancelling: "取消中", cancelled: "已取消", timed_out: "已超时", succeeded: "已完成", failed: "失败", unknown: "未知", expired: "已过期" };
const statusTone: Record<string, "ok" | "warn" | "bad" | "neutral" | "info"> = { succeeded: "ok", failed: "bad", timed_out: "bad", expired: "warn", cancelled: "neutral", unknown: "neutral", pending: "info", leased: "info", running: "info", queued: "info" };
const problem = (status: string) => status === "failed" || status === "timed_out" || status === "expired";
const analysisCode: Record<string, string> = { analysis_capacity: "后台分析正在占用，请稍后重新提交", analysis_unavailable: "dsh 分析失败", analysis_profile_changed: "分析配置已改变，请重新预览后提交", analysis_version_unverified: "dsh 版本未验证，后台分析暂停", analysis_dsh_not_running: "dsh 未运行或未配置", analysis_timeout: "分析超时，最终结果未知", analysis_incomplete: "dsh 未返回完整结果", completion_not_observed: "未取得完成回执，可能在中心重启时中断", analysis_cancelled: "已取消分析" };
const pointLabel: Record<string, string> = { security: "安全", "subscription.health": "订阅源健康", "node.test": "节点实测", "gate.kernel": "VPS 与内核", "kernel.release": "内核更新说明", "content.spam": "内容审核" };
const decisionLabel: Record<string, string> = { needed: "需要分析", not_needed: "无需分析", unknown: "未知" };
const dshStatusLabel: Record<string, string> = { ...taskStatus, running: "分析中", queued: "等待分析", not_requested: "未调用", not_applicable: "不调用 dsh", paused: "已暂停" };
const auditResult: Record<string, [string, "ok" | "bad" | "warn" | "neutral"]> = { ok: ["成功", "ok"], denied: ["被拒绝", "bad"], failed: ["失败", "bad"], queued: ["已排队", "warn"] };
const originLabel: Record<string, string> = { center: "中心", gate: "VPS", upstream: "上游", client: "客户端", dsh: "dsh", laya: "Laya" };
const stageNames: Record<string, string> = { "subscription.source": "拉取与解析订阅源", "subscription.combine": "处理与组合", "subscription.cache_hit": "命中输出缓存", "subscription.resolve": "解析成员依赖", "subscription.render": "渲染订阅格式", "subscription.response": "写入响应正文" };
const ms = (from: string, to: string) => `${Math.max(0, new Date(to).getTime() - new Date(from).getTime())} ms`;

// 每条记录的内部字段放这里：默认收起，展开后用等宽原文
function Details({ lines, children }: { lines: (string | false | undefined)[]; children?: ReactNode }) {
  const text = lines.filter(Boolean).join("\n");
  return <Collapsible title="详情">
    <div className="stack">
      {children}
      {text && <CodeBlock className="whitespace-pre-wrap break-all text-meta">{text}</CodeBlock>}
    </div>
  </Collapsible>;
}

function StatusBadge({ status, labels = taskStatus }: { status: string; labels?: Record<string, string> }) {
  return <Badge variant="status" tone={statusTone[status] ?? "neutral"}>{labels[status] ?? "未知"}</Badge>;
}

function SubscriptionRunStages({ id, profileName }: { id: string; profileName: (id: number) => string }) {
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: ["observe-subscription-run", id], enabled: open, queryFn: () => api(`/admin/subscriptions/runs/${id}`) as Promise<SubscriptionRun> });
  return <Collapsible title="处理阶段" open={open} onOpenChange={setOpen}>
    {open && (q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <ol className="stack">{q.data.stages?.map((stage, index) => <li key={index} className="stack">
      <div className="flex flex-wrap items-center gap-2"><strong>{stageNames[stage.operation] ?? "其他阶段"}</strong><StatusBadge status={stage.outcome} /><small>{ms(stage.started, stage.finished)}</small>{stage.cache_fallback && <Badge variant="soft" tone="warn">使用保护性缓存</Badge>}</div>
      <small className="break-all">{stage.profile_id ? profileName(stage.profile_id) : "未指定订阅组"} · 出错位置 {originLabel[stage.origin] ?? "未知"}{stage.error_code ? ` · 错误码 ${stage.error_code}` : ""}</small>
    </li>)}</ol>)}
  </Collapsible>;
}

export function ObservePage() {
  const [params, setParams] = useSearchParams();
  const toast = useToast();
  const tab = tabs.some((item) => item.key === params.get("tab")) ? params.get("tab")! : "overview";
  const [selected, setSelected] = useState<DiagnosticSelection[]>([]);
  // 已选条目的显示名：跨分区勾选时列出名称，不只给计数
  const [names, setNames] = useState<Record<string, string>>({});
  const [timelineId, setTimelineId] = useState("");
  const [timelineTaskId, setTimelineTaskId] = useState("");
  const [timeline, setTimeline] = useState<Timeline | null>(null);
  const tasks = useQuery({ queryKey: ["observe-tasks"], enabled: tab === "system", refetchIntervalInBackground: false, queryFn: async () => (await api("/admin/workers/tasks")).items as WorkerTask[], refetchInterval: 10000 });
  const statistics = useQuery({ queryKey: ["observe-task-statistics"], enabled: tab === "system", refetchIntervalInBackground: false, queryFn: () => api("/admin/workers/statistics?range=24h") as Promise<WorkerStatisticsData>, refetchInterval: 15000 });
  const gates = useQuery({ queryKey: ["observe-gates"], enabled: tab === "system" || tab === "laya", refetchIntervalInBackground: false, queryFn: async () => (await api("/admin/gates")).items as Gate[], refetchInterval: 15000 });
  const dsh = useDsh(tab === "security");
  const audits = useQuery({ queryKey: ["observe-account-audits"], enabled: tab === "accounts", queryFn: async () => (await api("/admin/audit?page=1&page_size=50&scope=accounts")).items as Audit[] });
  const sources = useSources(tab === "subscriptions");
  const runs = useQuery({ queryKey: ["observe-subscription-runs"], enabled: tab === "subscriptions", queryFn: async () => (await api("/admin/subscriptions/runs")).items as SubscriptionRun[], refetchInterval: 15000 });
  const profiles = useQuery({ queryKey: ["subscription-profiles"], enabled: tab === "subscriptions", queryFn: async () => (await api("/admin/subscriptions/profiles")).items as { id: number; name: string }[] });
  const profile = useQuery({ queryKey: ["observe-ai-profile"], enabled: tab === "policy", queryFn: () => api("/admin/ai/profile") as Promise<{ revision: string; effective: { observations?: Record<string, { enabled: boolean; confidence: number }> } }> });
  const accountPolicy = useQuery({ queryKey: ["observe-account-policy"], enabled: tab === "policy", queryFn: () => api("/admin/settings/account-policy") as Promise<{ revision: string; sources: Record<string, string> }> });
  const communication = useQuery({ queryKey: ["observe-communication"], enabled: tab === "system", refetchIntervalInBackground: false, queryFn: async () => await api("/admin/workers/communication?range=24h") as { items: { gate_id: number; route: string; requests: number; received_body_bytes: number; sent_body_bytes: number; errors: number }[]; complete: boolean }, refetchInterval: 60000 });
  const analyses = useQuery({ queryKey: ["observe-analyses"], enabled: tab === "laya", refetchIntervalInBackground: false, queryFn: async () => (await api("/admin/workers/analyses")).items as Analysis[], refetchInterval: 5000 });
  const observationFilter = params.get("outcome") === "not_needed" ? "not_needed" : "";
  const observations = useQuery({ queryKey: ["observe-observations", observationFilter], enabled: tab === "laya", refetchIntervalInBackground: false, queryFn: async () => (await api(`/admin/workers/observations${observationFilter ? "?outcome=not_needed" : ""}`)).items as Observation[], refetchInterval: 10000 });
  const observationDays = useQuery({ queryKey: ["observe-observation-days"], enabled: tab === "laya", refetchIntervalInBackground: false, queryFn: async () => (await api("/admin/workers/observations/daily")).items as ObservationDay[], refetchInterval: 60000 });
  useConsoleContext({ page: "运行观测", tab: tabs.find((item) => item.key === tab)?.label, list: { filters: { 分区: tabs.find((item) => item.key === tab)?.label ?? "总览", 已选: selected.map((item) => names[selectionKey(item)] ?? item.id).join("、") }, total: selected.length } });

  const gateName = (id?: number) => !id ? "未分配 VPS" : gates.data?.find((g) => g.id === id)?.name ?? (gates.isPending ? "正在读取 VPS" : "已删除的 VPS");
  const profileName = (id: number) => profiles.data?.find((p) => p.id === id)?.name ?? (profiles.isPending ? "正在读取订阅组" : "已删除的订阅组");

  async function readTimeline(task: WorkerTask) {
    setTimelineId(task.task.id);
    setTimelineTaskId(task.task.id);
    setTimeline(null);
    try {
      const result = await api(`/admin/workers/tasks/${task.task.id}/timeline`, "POST") as Timeline;
      setTimeline(result);
    } catch (error) {
      toast(error instanceof Error ? error.message : "读取 Gate 时间线失败", { tone: "bad" });
    } finally { setTimelineId(""); }
  }

  const loadError = (query: { isPending: boolean; error: Error | null; refetch: () => unknown }) => query.isPending || query.error ? <State loading={query.isPending} error={query.error} onRetry={() => { void query.refetch(); }} /> : null;
  // 有问题的排前面，同类按时间倒序保持接口原顺序
  const problemsFirst = <T,>(items: T[], bad: (item: T) => boolean) => [...items.filter(bad), ...items.filter((item) => !bad(item))];
  const recentTasks = problemsFirst(tasks.data ?? [], (task) => problem(task.status) || !!task.error);
  const failedTasks = recentTasks.filter((task) => problem(task.status) || !!task.error).length;
  const unselect = (key: string) => setSelected((previous) => previous.filter((entry) => selectionKey(entry) !== key));
  const pick = (kind: DiagnosticSelection["kind"], id: string | number, name: string) => {
    const item = { kind, id: String(id) };
    const key = selectionKey(item);
    const checked = selected.some((value) => selectionKey(value) === key);
    return <span className="inline-flex min-h-[var(--weir-tap-touch)] min-w-[var(--weir-tap-touch)] items-center justify-center"><Checkbox aria-label={`选择${name}`} checked={checked} onCheckedChange={(value) => {
      if (value) { setNames((previous) => ({ ...previous, [key]: name })); setSelected((previous) => [...previous.filter((entry) => selectionKey(entry) !== key), item]); }
      else unselect(key);
    }} /></span>;
  };

  return <div className="stack">
    {selected.length > 0 && <section className="stack" aria-label="已选诊断条目">
      <div className="flex flex-wrap items-center justify-between gap-3"><span>已选 {selected.length} 项，最多 20 项，可跨分区勾选</span><div className="flex flex-wrap gap-2"><Button variant="ghost" onClick={() => setSelected([])}>清空</Button><DiagnosticExport items={selected} /></div></div>
      <ul className="flex flex-wrap gap-2">{selected.map((item) => { const key = selectionKey(item); return <li key={key}><Badge variant="outline" className="gap-1">{names[key] ?? item.id}<Button size="icon-xs" variant="ghost" aria-label={`移除${names[key] ?? item.id}`} onClick={() => unselect(key)}><X /></Button></Badge></li>; })}</ul>
    </section>}
    <Tabs value={tab} onValueChange={(value) => setParams((previous) => { previous.set("tab", String(value)); return previous; }, { replace: true })}>
      <TabsList aria-label="运行观测分区">{tabs.map((item) => <TabsTab key={item.key} value={item.key}>{item.label}</TabsTab>)}</TabsList>
      <TabsPanel value="overview" className="stack">
        {tab === "overview" && <HealthOverview />}
      </TabsPanel>
      <TabsPanel value="security" className="stack">
        <EventsCard dshReady={!!dsh.data?.ready} pick={(id, name) => pick("event", id, name)} />
        <ProtectionActions pick={(id, name) => pick("protection-action", id, name)} />
        <BlocksCard />
      </TabsPanel>
      <TabsPanel value="subscriptions" className="stack">
        <section className="stack"><h2>订阅源</h2>
          {loadError(sources) ?? <>{(sources.data?.items ?? []).length === 0 ? <State empty="还没有订阅源" /> : problemsFirst(sources.data?.items ?? [], (source) => !!source.last_error).map((source) => <div key={source.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2">{pick("source", source.id, `订阅源 ${source.name}`)}<span className="min-w-0 flex-1"><strong>{source.name}</strong><small className="block">最近取得数据 {source.fetched_at ? date(source.fetched_at) : "还没有成功拉取过"}</small>{source.last_error && <small className="block break-words text-[var(--weir-bad-ink)]">{source.last_error}</small>}</span>{source.last_error ? <Badge variant="status" tone="bad">拉取失败</Badge> : !source.enabled ? <Badge variant="status" tone="neutral">已停用</Badge> : null}</div>)}</>}
        </section>
        <section className="stack"><h2>最近处理</h2>
          {loadError(runs) ?? ((runs.data ?? []).length === 0 ? <State empty="还没有订阅处理记录" detail="生成或预览订阅组后记录各阶段；不保留订阅正文和节点凭据。" /> : problemsFirst(runs.data!, (run) => problem(run.outcome) || !run.complete).map((run) => { const name = run.profile_id ? profileName(run.profile_id) : "订阅源刷新"; return <div key={run.id} className="stack border-b border-[var(--weir-line)] py-2">
            <div className="flex flex-wrap items-center gap-3">{pick("subscription-run", run.id, `订阅处理 · ${name}`)}<strong>{name}</strong><StatusBadge status={run.outcome} />{!run.complete && <Badge variant="soft" tone="warn">阶段记录不完整</Badge>}<small>{date(run.started)} · 用时 {ms(run.started, run.finished)}</small></div>
            <SubscriptionRunStages id={run.id} profileName={profileName} />
            <Details lines={[`关联编号 ${run.trace_id}`, `记录编号 ${run.id}`]} />
          </div>; }))}
        </section>
      </TabsPanel>
      <TabsPanel value="system" className="stack">
        {tab === "system" && <HotspotDetails />}
        <SystemHealth active={tab === "system"} pick={pick} />
        {loadError(gates) ?? loadError(tasks) ?? loadError(communication) ?? <>
          <section className="stack"><h2>后台任务{failedTasks > 0 ? `：${failedTasks} 个失败` : ""}</h2>{recentTasks.length === 0 ? <State empty="还没有任务记录" /> : recentTasks.slice(0, 30).map((task) => { const kind = taskKind[task.task.kind] ?? "其他任务"; return <div key={task.task.id} className="stack border-b border-[var(--weir-line)] py-2">
            <div className="flex flex-wrap items-center justify-between gap-3">{pick("task", task.task.id, `${kind} · ${gateName(task.gate_id)}`)}<span className="min-w-0 flex-1"><strong>{kind}</strong><small className="block">{gateName(task.gate_id)}{task.task.attempt > 1 ? ` · 第 ${task.task.attempt} 次尝试` : ""}{timeline?.records && task.task.id === timelineTaskId ? ` · ${timeline.records.length} 个本地环节` : ""}</small>{task.error && <small className="block text-[var(--weir-bad-ink)]">在{originLabel[task.error.origin] ?? "未知位置"}失败</small>}</span><StatusBadge status={task.status} /><Button size="sm" variant="outline" disabled={!task.gate_id || task.task.kind === "trace.read" || Boolean(timelineId)} busy={timelineId === task.task.id} onClick={() => void readTimeline(task)}>读取时间线</Button></div>
            <Details lines={[`关联编号 ${task.task.trace_id}`, `任务编号 ${task.task.id}`, `任务类型 ${task.task.kind}`, task.error && `错误码 ${task.error.code}`, `截止 ${date(task.task.deadline)}`]} />
          </div>; })}</section>
          <TraceSheet open={!!timeline} onOpenChange={(open) => { if (!open) setTimeline(null); }} description={timeline ? `采集于 ${date(timeline.collected_at)} · ${timeline.complete ? "完整" : `不完整：${timeline.missing_reason ?? "原因未知"}`}` : undefined}>
            {timeline && (timeline.records.length === 0 ? <State empty="Gate 未保留匹配的环节记录" /> : <Timeline label="Gate 环节">{timeline.records.map((record, index) => <TimelineItem key={`${record.task_id}-${index}`} time={record.finished} tone={record.outcome === "failed" ? "fail" : record.outcome === "unknown" ? "unknown" : "neutral"} title={`${record.stage} · ${taskStatus[record.outcome] ?? "未知"}`} description={`${originLabel[record.origin] ?? "未知位置"}${record.error_code ? ` · 错误码 ${record.error_code}` : ""}`} tags={ms(record.started, record.finished)} />)}</Timeline>)}
          </TraceSheet>
          <section className="stack"><h2>VPS 与中心的通信（24 小时）</h2>{(communication.data?.items ?? []).length === 0 ? <State empty={communication.data?.complete === false ? "统计不完整，部分 VPS 未上报" : "还没有通信统计"} /> : problemsFirst(communication.data!.items, (item) => item.errors > 0).map((item) => <div key={`${item.gate_id}-${item.route}`} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2"><span className="min-w-0">{gateName(item.gate_id)}<small className="block break-all">{item.route} · {item.requests} 次 · 收 {item.received_body_bytes} B · 发 {item.sent_body_bytes} B</small></span>{item.errors > 0 && <Badge variant="status" tone="bad">{item.errors} 次失败</Badge>}</div>)}</section>
        </>}
        <WorkerStatistics data={statistics.data} loading={statistics.isPending} error={statistics.error} onRetry={() => { void statistics.refetch(); }} labels={taskKind} />
        <WorkerPolicy />
      </TabsPanel>
      <TabsPanel value="accounts" className="stack">
        {loadError(audits) ?? <>{(audits.data ?? []).length === 0 ? <State empty="没有账户相关的操作记录" /> : problemsFirst(audits.data ?? [], (item) => (auditResult[item.result.split(" ")[0]]?.[1] ?? "bad") === "bad").map((item) => { const [text, tone] = auditResult[item.result.split(" ")[0]] ?? ["其他结果", "neutral"]; return <div key={item.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2">{pick("audit", item.id, `${item.action_label} · ${item.target_name || "未指定对象"}`)}<span className="min-w-0 flex-1"><strong>{item.action_label}</strong><small className="block">{item.target_name || "未指定对象"} · {date(item.created_at)}</small></span><Badge variant="status" tone={tone}>{text}</Badge></div>; })}</>}
      </TabsPanel>
      <TabsPanel value="laya" className="stack">
        {tab === "laya" && <OpsOverview />}
        <section className="stack"><div className="flex flex-wrap items-center justify-between gap-3"><h2>最近观察点判断</h2><label className="flex items-center gap-2 touch:min-h-11"><Checkbox aria-label="只看无需分析" checked={Boolean(observationFilter)} onCheckedChange={(value) => setParams((previous) => { if (value) previous.set("outcome", "not_needed"); else previous.delete("outcome"); return previous; }, { replace: true })} />只看无需分析</label></div>{loadError(observations) ?? ((observations.data ?? []).length === 0 ? <State empty="没有匹配的观察点记录" /> : problemsFirst(observations.data!, (item) => item.verdict?.outcome === "needed").map((item) => { const point = pointLabel[item.point] ?? "其他观察点"; const verdict = item.verdict; return <div key={item.id} className="stack border-b border-[var(--weir-line)] py-2">
          <div className="flex flex-wrap items-center gap-3">{pick("observation", item.id, `${point} · ${item.resource}`)}<strong>{point}</strong><span className="min-w-0 flex-1 break-all">{item.resource}</span><Badge variant="status" tone={verdict?.outcome === "needed" ? "warn" : verdict?.outcome === "not_needed" ? "ok" : "neutral"}>{verdict ? decisionLabel[verdict.outcome] ?? "未知" : "未判断"}</Badge></div>
          <small>{date(item.created_at)} · dsh {dshStatusLabel[item.dsh_status] ?? "未知"}{item.dsh_code ? `：${analysisCode[item.dsh_code] ?? "其他原因"}` : ""}</small>
          {item.external_data_not_instructions.conclusion && <p className="whitespace-pre-wrap break-words">{item.external_data_not_instructions.conclusion}</p>}
          <Details lines={[`关联编号 ${item.trace_id}`, `配置版本 ${item.profile_version}`, `置信度 ${verdict && verdict.attempts > 0 ? verdict.confidence : "未知"}，下限 ${verdict?.threshold ?? "未知"}`, `模型 ${known(verdict?.model)} · 模型版本 ${known(verdict?.model_version)} · 运行时 ${known(verdict?.runtime_version)}`, `执行位置 ${verdict?.execution === "gate" ? gateName(verdict.gate_id) : verdict?.execution === "center" ? "中心" : "未知"}`, verdict?.code && `判定原因码 ${verdict.code}`]}>
            <small>以下是送给模型的观测原文，属于外部数据，不作为指令</small>
            <CodeBlock className="whitespace-pre-wrap break-words text-meta">{item.external_data_not_instructions.text || "无"}</CodeBlock>
          </Details>
        </div>; }))}</section>
        <section className="stack"><h2>人工分析</h2>{loadError(analyses) ?? ((analyses.data ?? []).length === 0 ? <State empty="还没有提交过人工分析" /> : problemsFirst(analyses.data!, (item) => problem(item.status) || !!item.code).map((item) => <div key={item.id} className="stack border-b border-[var(--weir-line)] py-2">
          <div className="flex flex-wrap items-center justify-between gap-3">{pick("analysis", item.id, `人工分析 · ${date(item.created_at)}`)}<small className="min-w-0 flex-1">{date(item.created_at)} · {item.items.length} 项上下文</small><StatusBadge status={item.status} labels={dshStatusLabel} /></div>
          {item.code && <small className="text-[var(--weir-bad-ink)]">{analysisCode[item.code] ?? "分析未完成，原因见详情"}</small>}
          <p className="whitespace-pre-wrap break-words">{item.external_data_not_instructions.conclusion || "还没有分析结论"}</p>
          <Details lines={[`关联编号 ${item.trace_id}`, `上下文 ${item.items.join("、")}`, `dsh 版本 ${known(item.dsh_version)} · 配置 ${item.profile_version || "未知"}`, `工具调用 ${item.tool_calls?.length ? item.tool_calls.join("、") : item.status === "succeeded" ? "未调用" : "未知"}（模型内部过程不可观测）`, item.code && `原因码 ${item.code}`]} />
        </div>))}</section>
        <section className="stack" data-tour="observation-days"><h2>观察点每日汇总</h2><small>按判定完成时的站点时区归档；内容审核的「需要分析」表示建议审核内容。</small>{loadError(observationDays) ?? ((observationDays.data ?? []).length === 0 ? <State empty="还没有已完成的观察点判断" /> : observationDays.data!.map((item) => <div key={`${item.timezone}-${item.day}-${item.point}`} className="flex flex-wrap justify-between gap-3 border-b border-[var(--weir-line)] py-2"><span>{item.day} · {pointLabel[item.point] ?? "其他观察点"}</span><small>需要分析 {item.counts.needed ?? "未知"} · 无需分析 {item.counts.not_needed ?? "未知"} · 未判断 {item.counts.unknown ?? "未知"}</small></div>))}</section>
      </TabsPanel>
      <TabsPanel value="policy" className="stack">
        <label className="row-wrap">{pick("protection-policy", "current", "自动安全护栏")}自动安全护栏</label>
        <label className="row-wrap">{pick("worker-policy", "current", "VPS 后台任务预算")}VPS 后台任务预算</label>
        {loadError(accountPolicy) ?? <div className="flex flex-wrap items-center justify-between gap-3">{pick("account-policy", "current", "账户验证码与发信策略")}<strong className="flex-1">账户验证码与发信策略</strong><Details lines={[`版本 ${accountPolicy.data?.revision ?? "未知"}`]} /></div>}
        {loadError(profile) ?? <><div className="flex flex-wrap items-center justify-between gap-3">{pick("profile", "current", "AI 生效配置")}<strong className="flex-1">AI 生效配置</strong><Details lines={[`版本 ${profile.data?.revision ?? "未知"}`]} /></div><div className="divide-y divide-[var(--weir-line)]">{Object.entries(profile.data?.effective.observations ?? {}).map(([name, config]) => <div key={name} className="flex flex-wrap items-center justify-between gap-3 py-2"><span>{pointLabel[name] ?? name}<small className="block">置信度下限 {config.confidence}</small></span>{!config.enabled && <Badge variant="status" tone="neutral">已停用</Badge>}</div>)}</div><Button variant="outline" onClick={() => { void queryClient.invalidateQueries({ queryKey: ["observe-ai-profile"] }); }}>重新读取</Button></>}
      </TabsPanel>
    </Tabs>
  </div>;
}
