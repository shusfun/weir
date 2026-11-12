import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ActionGroup, Badge, Button, Checkbox, Collapsible, SectionHeader, Tabs, TabsList, TabsPanel, TabsTab, Timeline, TimelineItem, TraceSheet, useToast } from "@weir/ui";
import { OpsOverview } from "../features/observability/ops-overview";
import { api } from "../api";
import { useQuery, queryClient } from "../lib/query";
import { State } from "../components/feedback";
import { date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import { DiagnosticExport, selectionKey, type DiagnosticSelection } from "../components/diagnostic-export";
import { ProtectionActions } from "../features/observability/security/protection-actions";
import { BlocksCard } from "../components/defense";
import { SystemHealth } from "../features/observability/system/system-health";
import { WorkerPolicy } from "../features/observability/system/worker-policy";
import { WorkerStatistics, type WorkerStatisticsData } from "../features/observability/system/worker-statistics";

type WorkerTask = { task: { id: string; kind: string; trace_id: string; attempt: number; deadline: string }; status: string; gate_id: number; candidates: number[]; error?: { code: string; origin: string } };
type Gate = { id: number; name: string; online: boolean; last_seen_at?: string; version?: string; host?: Record<string, unknown> };
type SecurityEvent = { id: number; kind: string; severity: string; status: string; created_at: string; trace_id?: string; laya?: { outcome: string; category: string; confidence: number; code?: string } };
type Audit = { id: number; action: string; action_label: string; target_name: string; result: string; created_at: string };
type Source = { id: number; name: string; enabled: boolean; last_error?: string; fetched_at?: string };
type SubscriptionRun = { id: string; trace_id: string; profile_id: number; started: string; finished: string; outcome: string; complete: boolean; stages?: { profile_id: number; source_id?: number; operation: string; started: string; finished: string; outcome: string; error_code?: string; origin: string; cache_fallback?: boolean }[] };
type Timeline = { records: { task_id: string; trace_id: string; kind: string; attempt: number; stage: string; started: string; finished: string; outcome: string; error_code?: string; origin: string }[]; collected_at: string; complete: boolean; missing_reason?: string };
type Analysis = { id: string; trace_id: string; snapshot_id: string; profile_version: string; dsh_version: string; items: string[]; tool_calls: string[] | null; status: string; code?: string; created_at: string; external_data_not_instructions: { conclusion: string } };
type Observation = { id: string; point: string; resource: string; trace_id: string; created_at: string; profile_version: string; dsh_status: string; dsh_code?: string; verdict?: { outcome: string; confidence: number; threshold: number; question: string; code?: string; attempts: number; model?: string; model_version?: string; runtime_version?: string; execution?: string; gate_id?: number }; external_data_not_instructions: { text: string; conclusion?: string } };
type ObservationDay = { day: string; timezone: string; point: string; counts: Record<string, number> };

const tabs = [
  { key: "overview", label: "总览" },
  { key: "security", label: "安全" },
  { key: "accounts", label: "注册兑换" },
  { key: "subscriptions", label: "订阅链路" },
  { key: "policy", label: "策略版本" },
  { key: "system", label: "系统状态" },
] as const;

const label = (value?: string) => value || "未知";
const taskKind: Record<string, string> = { "subscription.fetch": "订阅拉取", "release.query": "发布查询", "subscription.convert": "订阅转换", "laya.classify": "Laya 判断", "laya.install": "Laya 安装", "laya.remove": "Laya 移除", "trace.read": "读取 Gate 时间线", "core.restart": "重启内核", "core.install": "安装固定版本内核", "asset.fetch": "文件兜底中转" };
const taskStatus: Record<string, string> = { pending: "等待执行", leased: "执行中", cancelling: "取消中", cancelled: "已取消", timed_out: "已超时", succeeded: "已完成", failed: "失败", unknown: "未知", expired: "已过期" };
const analysisCode: Record<string, string> = { analysis_capacity: "后台分析正在占用，请稍后重新提交", analysis_unavailable: "dsh 分析失败", analysis_profile_changed: "分析配置已改变，请重新预览后提交", analysis_version_unverified: "dsh 版本未验证，后台分析暂停", analysis_dsh_not_running: "dsh 未运行或未配置", analysis_timeout: "分析超时，最终结果未知", analysis_incomplete: "dsh 未返回完整结果", completion_not_observed: "未取得完成回执，可能在中心重启时中断", analysis_cancelled: "已取消分析" };
const pointLabel: Record<string, string> = { security: "安全", "subscription.health": "订阅源健康", "node.test": "节点实测", "gate.kernel": "VPS 与内核", "kernel.release": "内核更新说明", "content.spam": "内容审核" };
const decisionLabel: Record<string, string> = { needed: "需要分析", not_needed: "无需分析", unknown: "未知" };
const dshStatusLabel: Record<string, string> = { ...taskStatus, running: "分析中", queued: "等待分析", not_requested: "未调用", not_applicable: "不调用 dsh", paused: "已暂停" };

function SubscriptionRunDetails({ id }: { id: string }) {
  const [open,setOpen] = useState(false);
  const q = useQuery({ queryKey:["observe-subscription-run",id], enabled:open, queryFn:()=>api(`/admin/subscriptions/runs/${id}`) as Promise<SubscriptionRun> });
  const names: Record<string,string> = { "subscription.source":"拉取与解析订阅源", "subscription.combine":"处理与组合", "subscription.cache_hit":"命中输出缓存", "subscription.resolve":"解析成员依赖", "subscription.render":"渲染订阅格式", "subscription.response":"写入响应正文" };
  return <Collapsible title="查看处理阶段" open={open} onOpenChange={setOpen}>
    {open && (q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()} /> : <ol className="stack">{q.data.stages?.map((stage,index)=><li key={index} className="stack"><div className="flex flex-wrap gap-2"><strong>{names[stage.operation] ?? stage.operation}</strong><Badge variant="status">{taskStatus[stage.outcome] ?? "未知"}</Badge><small>{new Date(stage.finished).getTime()-new Date(stage.started).getTime()} ms</small></div><small className="break-all">订阅组 {stage.profile_id || "未指定"}{stage.source_id ? ` · 订阅源 ${stage.source_id}` : ""} · {stage.origin}{stage.error_code ? ` · ${stage.error_code}` : ""}{stage.cache_fallback ? " · 使用保护性缓存" : ""}</small></li>)}</ol>)}
  </Collapsible>;
}

export function ObservePage() {
  const [params, setParams] = useSearchParams();
  const toast = useToast();
  const tab = tabs.some((item) => item.key === params.get("tab")) ? params.get("tab")! : "overview";
  const [selected, setSelected] = useState<DiagnosticSelection[]>([]);
  const [timelineId, setTimelineId] = useState("");
  const [timelineTaskId, setTimelineTaskId] = useState("");
  const [timeline, setTimeline] = useState<Timeline | null>(null);
  const tasks = useQuery({ queryKey: ["observe-tasks"], queryFn: async () => (await api("/admin/workers/tasks")).items as WorkerTask[], refetchInterval: 10000 });
  const statistics = useQuery({ queryKey: ["observe-task-statistics"], queryFn: () => api("/admin/workers/statistics?range=24h") as Promise<WorkerStatisticsData>, refetchInterval: 15000 });
  const gates = useQuery({ queryKey: ["observe-gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[], refetchInterval: 15000 });
  const events = useQuery({ queryKey: ["observe-events"], queryFn: async () => (await api("/admin/defense/events")).events as SecurityEvent[], refetchInterval: 15000 });
  const audits = useQuery({ queryKey: ["observe-account-audits"], queryFn: async () => (await api("/admin/audit?page=1&page_size=50&scope=accounts")).items as Audit[] });
  const sources = useQuery({ queryKey: ["observe-subscription-sources"], queryFn: async () => (await api("/admin/subscriptions/sources")).items as Source[] });
  const runs = useQuery({ queryKey: ["observe-subscription-runs"], enabled: tab === "subscriptions", queryFn: async () => (await api("/admin/subscriptions/runs")).items as SubscriptionRun[], refetchInterval: 15000 });
  const profile = useQuery({ queryKey: ["observe-ai-profile"], queryFn: () => api("/admin/ai/profile") as Promise<{ revision: string; effective: { observations?: Record<string, { enabled: boolean; confidence: number }> } }> });
  const accountPolicy = useQuery({ queryKey: ["observe-account-policy"], queryFn: () => api("/admin/settings/account-policy") as Promise<{ revision: string; sources: Record<string, string> }> });
  const communication = useQuery({ queryKey: ["observe-communication"], queryFn: async () => await api("/admin/workers/communication?range=24h") as { items: { gate_id: number; route: string; requests: number; received_body_bytes: number; sent_body_bytes: number; errors: number }[]; complete: boolean }, refetchInterval: 60000 });
  const analyses = useQuery({ queryKey: ["observe-analyses"], queryFn: async () => (await api("/admin/workers/analyses")).items as Analysis[], refetchInterval: 5000 });
  const observationFilter = params.get("outcome") === "not_needed" ? "not_needed" : "";
  const observations = useQuery({ queryKey: ["observe-observations", observationFilter], queryFn: async () => (await api(`/admin/workers/observations${observationFilter ? "?outcome=not_needed" : ""}`)).items as Observation[], refetchInterval: 10000 });
  const observationDays = useQuery({ queryKey: ["observe-observation-days"], queryFn: async () => (await api("/admin/workers/observations/daily")).items as ObservationDay[], refetchInterval: 60000 });
  useConsoleContext({ page: "运行观测", tab: tabs.find((item) => item.key === tab)?.label, list: { filters: { 分区: tabs.find((item) => item.key === tab)?.label ?? "总览" }, total: selected.length } });

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
  const recentTasks = tasks.data ?? [];
  const taskStates = statistics.data?.task_states;
  const activeTasks = taskStates ? ["leased", "pending", "cancelling"].reduce((sum, state) => sum + (taskStates[state] ?? 0), 0) : null;
  const taskFailures = statistics.data ? statistics.data.items.reduce((sum, item) => sum + item.failed + item.unknown, 0) : null;
  const gateItems = gates.data ?? [];
  const pick = (kind: DiagnosticSelection["kind"], id: string | number, name: string) => {
    const item = { kind, id: String(id) };
    const checked = selected.some((value) => selectionKey(value) === selectionKey(item));
    return <span className="inline-flex min-h-[var(--weir-tap-touch)] min-w-[var(--weir-tap-touch)] items-center justify-center"><Checkbox aria-label={`选择${name}`} checked={checked} onCheckedChange={(value) => setSelected((previous) => value ? [...previous.filter((entry) => selectionKey(entry) !== selectionKey(item)), item] : previous.filter((entry) => selectionKey(entry) !== selectionKey(item)))} /></span>;
  };

  return <div className="stack">
    <OpsOverview />
    <SectionHeader className="mt-[41px]" title="明细" actions={<span className="text-meta text-[var(--weir-ink-3)]">在线 VPS {gates.data ? `${gateItems.filter((gate) => gate.online).length} / ${gateItems.length}` : "未知"} · 进行中的任务 {activeTasks ?? "未知"} · 24 小时失败或未知 {taskFailures ?? "未知"}</span>} />
    <div className="flex flex-wrap items-center justify-between gap-3"><span>已选择 {selected.length} 项（最多 20 项），可跨分区选择。</span><div className="flex flex-wrap gap-2"><Button variant="ghost" disabled={!selected.length} onClick={() => setSelected([])}>清空选择</Button><DiagnosticExport items={selected} /></div></div>
    <Tabs value={tab} onValueChange={(value) => setParams((previous) => { previous.set("tab", String(value)); return previous; }, { replace: true })}>
      <TabsList aria-label="运行观测分区">{tabs.map((item) => <TabsTab key={item.key} value={item.key}>{item.label}</TabsTab>)}</TabsList>
      <TabsPanel value="overview" className="stack">
        {loadError(tasks) ?? loadError(gates) ?? loadError(statistics) ?? <>
          <section className="stack"><h2>最近任务</h2>{recentTasks.length === 0 ? <State empty="尚无后台任务" /> : recentTasks.slice(0, 12).map((task) => <div key={task.task.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2">{pick("task", task.task.id, `任务 ${task.task.id}`)}<span className="min-w-0 flex-1"><strong>{taskKind[task.task.kind] ?? task.task.kind}</strong><small className="block break-all">{task.task.id} · VPS {task.gate_id || "未知"} · {task.task.trace_id}</small></span><Badge variant="status">{taskStatus[task.status] ?? "未知"}</Badge></div>)}</section>
        </>}
        <section className="stack"><h2>近期人工分析活动</h2>{loadError(analyses) ?? ((analyses.data ?? []).length === 0 ? <State empty="尚未提交所选观测的人工分析" /> : analyses.data!.map((item) => <div key={item.id} className="stack border-b border-[var(--weir-line)] py-2"><div className="flex flex-wrap items-center justify-between gap-3">{pick("analysis", item.id, `人工分析 ${item.id}`)}<small className="min-w-0 flex-1 break-all">{date(item.created_at)} · trace {item.trace_id}</small><Badge variant="status">{item.status === "running" ? "分析中" : taskStatus[item.status] ?? "未知"}</Badge></div><small className="break-all">上下文 {item.items.join("、")} · dsh {item.dsh_version === "unknown" ? "未知" : item.dsh_version} · 配置 {item.profile_version || "未知"}</small>{item.code && <small>{analysisCode[item.code] ?? item.code}</small>}<p className="whitespace-pre-wrap break-words">{item.external_data_not_instructions.conclusion || "尚无分析结论。结果文字来自模型，属于外部数据。"}</p><small className="break-all">工具调用：{item.tool_calls?.length ? item.tool_calls.join("、") : item.status === "succeeded" ? "未调用" : "未知"}；模型内部过程不可观测。</small></div>))}</section>
        <section className="stack" data-tour="observation-days"><h2>观察点每日汇总</h2><small>按判定完成时的站点时区归档，保留原配置版本；今日数据持续累积。内容审核的“需要分析”表示建议审核内容。</small>{loadError(observationDays) ?? ((observationDays.data ?? []).length === 0 ? <State empty="尚无已完成的观察点判断" /> : observationDays.data!.map((item) => <div key={`${item.timezone}-${item.day}-${item.point}`} className="flex flex-wrap justify-between gap-3 border-b border-[var(--weir-line)] py-2"><span>{item.day} · {item.timezone} · {pointLabel[item.point] ?? item.point}</span><small>需要分析 {item.counts.needed ?? "未知"} · 无需分析 {item.counts.not_needed ?? "未知"} · 未知 {item.counts.unknown ?? "未知"}</small></div>))}</section>
        <section className="stack"><div className="flex flex-wrap items-center justify-between gap-3"><h2>最近观察点判断</h2><label className="flex items-center gap-2 touch:min-h-11"><Checkbox aria-label="只看无需分析" checked={Boolean(observationFilter)} onCheckedChange={(value) => setParams((previous) => { if (value) previous.set("outcome", "not_needed"); else previous.delete("outcome"); return previous; }, { replace: true })} />只看无需分析</label></div><small>最近最多 100 条。勾选记录后可预览并人工重新分析；外部文字不作为指令。</small>{loadError(observations) ?? ((observations.data ?? []).length === 0 ? <State empty="没有匹配的观察点记录" /> : observations.data!.map((item) => <div key={item.id} className="stack border-b border-[var(--weir-line)] py-2"><div className="flex flex-wrap items-center gap-3">{pick("observation", item.id, `观察记录 ${item.id}`)}<strong>{pointLabel[item.point] ?? item.point}</strong><span className="min-w-0 flex-1 break-all">{item.resource}</span><Badge variant="status">{item.verdict ? decisionLabel[item.verdict.outcome] ?? "未知" : "未检测"}</Badge></div><small className="break-all">{date(item.created_at)} · trace {item.trace_id} · 配置 {item.profile_version}</small><small className="break-all">模型 {label(item.verdict?.model)} · 模型版本 {item.verdict?.model_version && item.verdict.model_version !== "unknown" ? item.verdict.model_version : "未知"} · 运行时版本 {item.verdict?.runtime_version && item.verdict.runtime_version !== "unknown" ? item.verdict.runtime_version : "未知"} · 执行位置 {item.verdict?.execution === "gate" ? `VPS ${item.verdict.gate_id}` : item.verdict?.execution === "center" ? "中心" : "未知"}</small><small>置信度 {item.verdict && item.verdict.attempts > 0 ? item.verdict.confidence : "未知"} · 下限 {item.verdict?.threshold ?? "未知"} · dsh {dshStatusLabel[item.dsh_status] ?? "未知"} {item.dsh_code ? analysisCode[item.dsh_code] ?? item.dsh_code : ""}</small>{item.verdict?.code && <small>判定原因：{item.verdict.code}</small>}<p className="whitespace-pre-wrap break-words">{item.external_data_not_instructions.text}</p>{item.external_data_not_instructions.conclusion && <p className="whitespace-pre-wrap break-words">{item.external_data_not_instructions.conclusion}</p>}</div>))}</section>
      </TabsPanel>
      <TabsPanel value="security" className="stack">
        <ProtectionActions pick={(id,name)=>pick("protection-action",id,name)} />
        <BlocksCard />
        {loadError(events) ?? <>
          <p>勾选安全事件后，使用上方的预览检查上下文，再交给 dsh。</p>
          {(events.data ?? []).length === 0 ? <State empty="尚无安全事件" /> : (events.data ?? []).map((event) => <label key={event.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2">{pick("event", event.id, `安全事件 ${event.id}`)}<span className="min-w-0 flex-1"><strong>#{event.id} · {event.kind}</strong><small className="block">{date(event.created_at)} · {event.status} · Laya {event.laya?.outcome ?? "未知"} · 置信度 {event.laya?.confidence ?? "未知"}</small><small className="block break-all">trace {event.trace_id ?? "未知"}</small></span></label>)}
        </>}
      </TabsPanel>
      <TabsPanel value="accounts" className="stack">
        {loadError(audits) ?? <>{(audits.data ?? []).length === 0 ? <State empty="没有匹配的账户审计记录" /> : (audits.data ?? []).map((item) => <div key={item.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2">{pick("audit", item.id, `账户审计 ${item.id}`)}<span className="min-w-0 flex-1"><strong>{item.action_label}</strong><small className="block">{item.target_name || "未知"} · {date(item.created_at)}</small></span><Badge variant="status">{item.result || "未知"}</Badge></div>)}</>}
      </TabsPanel>
      <TabsPanel value="subscriptions" className="stack">
        <h2>最近处理时间线</h2>
        {loadError(runs) ?? ((runs.data ?? []).length === 0 ? <State empty="尚无订阅处理时间线" detail="生成或预览订阅组后记录阶段；不保留订阅正文和节点凭据。" /> : runs.data!.map((run) => <div key={run.id} className="stack border-b border-[var(--weir-line)] py-2"><div className="flex flex-wrap items-center gap-3">{pick("subscription-run",run.id,`订阅处理 ${run.id}`)}<strong>{run.profile_id ? `订阅组 #${run.profile_id}` : "订阅源刷新"}</strong><Badge variant="status">{taskStatus[run.outcome] ?? "未知"}</Badge><small>{date(run.started)} · {Math.max(0,new Date(run.finished).getTime()-new Date(run.started).getTime())} ms</small></div><small className="break-all">trace {run.trace_id}{!run.complete ? " · 阶段记录不完整" : ""}</small><SubscriptionRunDetails id={run.id} /></div>))}
        {loadError(sources) ?? <>{(sources.data ?? []).length === 0 ? <State empty="尚无订阅源" /> : (sources.data ?? []).map((source) => <div key={source.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2">{pick("source", source.id, `订阅源 ${source.name}`)}<span className="min-w-0 flex-1"><strong>{source.name}</strong><small className="block">最近取得数据 {source.fetched_at ? date(source.fetched_at) : "未检测"}</small>{source.last_error && <small className="block break-words text-[var(--weir-danger)]">{source.last_error}</small>}</span><Badge variant="status">{source.enabled ? "启用" : "停用"}</Badge></div>)}</>}
      </TabsPanel>
      <TabsPanel value="policy" className="stack">
        <label className="row-wrap">{pick("protection-policy","current","自动安全护栏")}自动安全护栏</label>
        <label className="row-wrap">{pick("worker-policy","current","VPS 后台任务预算")}VPS 后台任务预算</label>
        {loadError(accountPolicy) ?? <div className="flex flex-wrap items-center justify-between gap-3">{pick("account-policy", "current", "账户验证码与发信策略")}<strong>账户验证码与发信策略</strong><span className="min-w-0 break-all font-mono">{accountPolicy.data?.revision ?? "未知"}</span></div>}
        {loadError(profile) ?? <><div className="flex flex-wrap items-center justify-between gap-3">{pick("profile", "current", "AI 生效配置")}<strong>生效配置版本</strong><span className="min-w-0 break-all font-mono">{profile.data?.revision ?? "未知"}</span></div><div className="divide-y divide-[var(--weir-line)]">{Object.entries(profile.data?.effective.observations ?? {}).map(([name, config]) => <div key={name} className="flex flex-wrap items-center justify-between gap-3 py-2"><span>{name}<small className="block">置信度下限 {config.confidence}</small></span><Badge variant="status">{config.enabled ? "启用" : "停用"}</Badge></div>)}</div><Button variant="outline" onClick={() => { void queryClient.invalidateQueries({ queryKey: ["observe-ai-profile"] }); }}>刷新配置版本</Button></>}
      </TabsPanel>
      <TabsPanel value="system" className="stack">
        <SystemHealth active={tab === "system"} pick={pick} />
        <WorkerPolicy />
        <WorkerStatistics data={statistics.data} loading={statistics.isPending} error={statistics.error} onRetry={() => { void statistics.refetch(); }} labels={taskKind} />
        {loadError(gates) ?? loadError(tasks) ?? loadError(communication) ?? <>

          <section className="stack"><h2>任务活动</h2>{recentTasks.length === 0 ? <State empty="尚无任务记录" /> : recentTasks.slice(0, 30).map((task) => <div key={task.task.id} className="stack border-b border-[var(--weir-line)] py-2"><div className="flex flex-wrap items-center justify-between gap-3">{pick("task", task.task.id, `任务 ${task.task.id}`)}<span className="min-w-0 flex-1"><strong>{taskKind[task.task.kind] ?? task.task.kind}</strong><small className="block break-all">{task.task.trace_id} · VPS {task.gate_id || "未知"}</small>{task.error && <small className="block">{task.error.code} · {task.error.origin}</small>}{timeline?.records && task.task.id === timelineTaskId && <small className="block">{timeline.records.length} 个本地环节</small>}</span><Button size="sm" variant="outline" disabled={!task.gate_id || task.task.kind === "trace.read" || Boolean(timelineId)} busy={timelineId === task.task.id} onClick={() => void readTimeline(task)}>读取时间线</Button></div></div>)}</section>
          <TraceSheet open={!!timeline} onOpenChange={(open) => { if (!open) setTimeline(null); }} description={timeline ? `采集于 ${date(timeline.collected_at)} · ${timeline.complete ? "完整" : `不完整：${timeline.missing_reason ?? "未知原因"}`}` : undefined}>
            {timeline && (timeline.records.length === 0 ? <State empty="Gate 未保留匹配的环节记录" /> : <Timeline label="Gate 环节">{timeline.records.map((record, index) => <TimelineItem key={`${record.task_id}-${index}`} time={record.finished} tone={record.outcome === "failed" ? "fail" : record.outcome === "unknown" ? "unknown" : "neutral"} title={`${record.stage} · ${record.outcome}`} description={`${record.origin} · ${record.error_code ?? "无错误"}`} tags={`${Math.max(0, new Date(record.finished).getTime() - new Date(record.started).getTime())} ms`} />)}</Timeline>)}
          </TraceSheet>
          <section className="stack"><h2>HTTP 通信（24 小时）</h2>{(communication.data?.items ?? []).length === 0 ? <State empty={communication.data?.complete === false ? "数据不完整" : "尚无通信统计"} /> : communication.data!.items.map((item) => <div key={`${item.gate_id}-${item.route}`} className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--weir-line)] py-2"><span>VPS {item.gate_id} · {item.route}<small className="block">{item.requests} 次 · 收 {item.received_body_bytes} B · 发 {item.sent_body_bytes} B · 失败 {item.errors}</small></span></div>)}</section>
        </>}
      </TabsPanel>
    </Tabs>
  </div>;
}
