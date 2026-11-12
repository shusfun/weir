import type { ReactNode } from "react";
import { Badge, Button, Collapsible, InsetRow, InsetRows, ModuleCard, Readout, SectionHeader, useLiveSwitch } from "@weir/ui";
import { api } from "../../../api";
import { State } from "../../../components/feedback";
import { ObjectLink, dataObject } from "../../../components/object-link";
import type { DiagnosticSelection } from "../../../components/diagnostic-export";
import { date } from "../../../lib/models";
import { useQuery } from "../../../lib/query";

type Host = { observed_at: string; cpu_percent?: number; mem_total?: number; mem_used?: number; disk_total?: number; disk_used?: number };
type Health = { state: string; code?: string; observed_at?: string; version?: string; model?: string; digest?: string };
type Capabilities = {
  protocol: number; version: string; arch: string; os?: string; tasks: Record<string, number>;
  region?: string; asn?: number; bandwidth_bytes_per_second?: number;
  sources?: Record<string, boolean>; components?: Record<string, { version: string; models?: Record<string, string> }>;
};
type GateSnapshot = {
  id: number; name: string; state: string; last_seen_at: string | null; sources_observed_at: string | null;
  capabilities: Capabilities | null; host: Host | null; active: number | null; concurrency_limit: number | null;
  kernels: Record<string, Health>; laya: Health;
};
type SystemSnapshot = {
  observed_at: string;
  center: { environment: { weir_version: string; os: string; arch: string }; host: Host | null };
  gates: GateSnapshot[];
  dsh: { health: Health; plugin_verified: boolean; background_enabled: boolean };
  laya: { health: Health; mode?: string; gate_id?: number; configured_model?: string; configuration_revision?: string; trace_id?: string; inference: string };
  worker_policy_revision: string;
};

const states: Record<string, string> = { online: "在线", offline: "离线", revoked: "已撤销", running: "进程运行中", stopped: "已停止", available: "模型可访问", disabled: "未启用", failed: "运行失败", unknown: "未知" };
const reasons: Record<string, string> = {
  process_observation_missing: "缺少进程采样", process_observation_stale: "进程采样已过期", process_observation_failed: "进程采样失败",
  component_observation_stale: "组件采样已过期", component_observation_missing: "尚无组件采样", runtime_status_unavailable: "容器状态读取失败",
  runtime_not_running: "未确认容器运行", model_status_unavailable: "模型状态读取失败", model_digest_mismatch: "模型摘要不符合锁定档案",
  configuration_unavailable: "无法读取配置", model_observation_missing: "等待模型采样", see_gate_observation: "查看指定 VPS 的 Laya 状态",
  runtime_credentials_unavailable: "运行凭证不可用", supervisor_observation_missing: "尚无管理进程心跳", supervisor_observation_stale: "管理进程心跳已过期",
  desired_state_not_ready: "尚未达到期望运行状态", runtime_failed: "官方运行时未能启动",
};

function HealthValue({ value }: { value: Health }) {
  return <span className="stack">
    <Badge variant="status" tone={value.state === "failed" ? "bad" : value.state === "unknown" ? "warn" : "neutral"}>{states[value.state] ?? "未知"}</Badge>
    {value.code && <small>{reasons[value.code] ?? "状态未确认"}</small>}
    <small>采集于 {value.observed_at ? date(value.observed_at) : "未检测"} · 版本 {value.version || "未知"}</small>
    {value.model && <small className="break-all">{value.model} · 摘要 {value.digest || "未知"}</small>}
  </span>;
}

function ResourceReadings({ host }: { host: Host | null }) {
  const mib = (value?: number) => value == null ? null : Math.round(value / 1048576);
  return <div className="grid gap-3 sm:grid-cols-3">
    <Readout label="CPU" value={host?.cpu_percent == null ? null : host.cpu_percent.toFixed(1)} unit="%" />
    <Readout label="内存已用" value={mib(host?.mem_used)} unit="MiB" caption={host?.mem_total == null ? "总量未知" : `共 ${mib(host.mem_total)} MiB`} />
    <Readout label="磁盘已用" value={mib(host?.disk_used)} unit="MiB" caption={host?.disk_total == null ? "总量未知" : `共 ${mib(host.disk_total)} MiB`} />
  </div>;
}

export function SystemHealth({ active, pick }: { active: boolean; pick: (kind: DiagnosticSelection["kind"], id: string | number, name: string) => ReactNode }) {
  const [live, setLive] = useLiveSwitch("observe-system-health");
  const query = useQuery({
    queryKey: ["observe-system-health"], enabled: active && live,
    queryFn: ({ signal }) => api("/admin/workers/system", "GET", undefined, { signal }) as Promise<SystemSnapshot>,
    refetchInterval: active && live ? 30000 : false,
  });
  const data = query.data;
  return <ModuleCard title="中心与 VPS 健康" description="资源与进程采样独立于心跳。模型可访问只证明运行时返回了模型摘要，实际分类结果请查看观察记录。" live={live} onLiveChange={setLive} actions={<Button size="sm" variant="outline" busy={query.isFetching} disabled={!active || !live} onClick={() => { void query.refetch(); }}>刷新状态</Button>}>
    {query.error || (query.isPending && active && live) ? <State loading={query.isPending} error={query.error} onRetry={() => { void query.refetch(); }} /> : !data ? <State empty="尚未检测系统状态" /> : <InsetRows data-tour="system-health">
      <InsetRow>
        <SectionHeader size="sm" title="中心" actions={pick("center", "current", "中心运行环境")} description={`${data.center.environment.os} / ${data.center.environment.arch} · Weir ${data.center.environment.weir_version} · 资源采集于 ${data.center.host ? date(data.center.host.observed_at) : "未检测"}`} />
        <ResourceReadings host={data.center.host} />
      </InsetRow>
      <InsetRow>
        <SectionHeader size="sm" title="dsh" actions={pick("dsh", "current", "dsh 状态")} />
        <HealthValue value={data.dsh.health} />
        <small>插件兼容范围：{data.dsh.plugin_verified ? "已验证" : "未确认或超出范围"} · 后台分析：{data.dsh.background_enabled ? "可用" : "暂停"}</small>
      </InsetRow>
      <InsetRow>
        <SectionHeader size="sm" title="Laya" actions={pick("laya", "current", "Laya 状态")} description={data.laya.mode === "gate" ? `部署于 VPS ${data.laya.gate_id ?? "未知"}` : "中心连接的官方运行时"} />
        <HealthValue value={data.laya.health} />
        <small>模型：{data.laya.configured_model || "未知"} · 此探测不执行推理</small>
        {data.laya.trace_id && <small className="break-all">探测 trace {data.laya.trace_id}</small>}
      </InsetRow>
      {data.gates.length === 0 ? <InsetRow><State empty="没有 VPS" /></InsetRow> : data.gates.map((gate) => <InsetRow key={gate.id} {...dataObject({ type: "gate", id: gate.id })}>
        <SectionHeader size="sm" title={<ObjectLink to={{ type: "gate", id: gate.id, name: gate.name }} />} actions={<>{pick("gate", gate.id, `VPS ${gate.name}`)}<Badge variant="status">{states[gate.state] ?? "未知"}</Badge></>} description={`最近心跳 ${gate.last_seen_at ? date(gate.last_seen_at) : "未检测"} · 协议 ${gate.capabilities?.protocol ?? "未知"} · ${gate.capabilities?.os || "系统未知"} / ${gate.capabilities?.arch || "架构未知"}`} />
        <ResourceReadings host={gate.host} />
        <div className="grid gap-3 sm:grid-cols-3"><Readout label="后台任务占用" value={gate.active} caption={`并发上限 ${gate.concurrency_limit ?? "未知"}`} />{Object.entries(gate.kernels).map(([name, health]) => <div key={name} className="stack"><strong>{name}</strong><HealthValue value={health} /></div>)}</div>
        <Collapsible title="能力与组件详情">
          <InsetRows>
            <InsetRow><strong>Laya</strong><HealthValue value={gate.laya} /></InsetRow>
            <InsetRow><small>地区 {gate.capabilities?.region || "未知"} · ASN {gate.capabilities?.asn ?? "未知"} · 带宽档位 {gate.capabilities?.bandwidth_bytes_per_second ?? "未知"} B/s</small></InsetRow>
            <InsetRow><strong>来源可达性</strong>{!Object.keys(gate.capabilities?.sources ?? {}).length ? <p>未知</p> : Object.entries(gate.capabilities!.sources!).map(([name, reachable]) => <p key={name}>{name}：{reachable ? "探测可访问" : "探测未通过"}</p>)}<small>采集于 {gate.sources_observed_at ? date(gate.sources_observed_at) : "未检测"}。每五分钟独立探测；可访问不保证下载成功，调度拒绝超过十分钟的探测。</small></InsetRow>
            <InsetRow><strong>任务版本</strong>{!gate.capabilities ? <p>未知</p> : Object.entries(gate.capabilities.tasks).map(([kind, version]) => <p key={kind} className="break-all">{kind} · v{version}</p>)}</InsetRow>
          </InsetRows>
        </Collapsible>
      </InsetRow>)}
      <InsetRow><small>汇总于 {date(data.observed_at)}。进程采样超过两分钟显示未知；VPS 心跳超过九十秒视为离线。旧协议设备缺少能力文档时仍显示未知。</small></InsetRow>
    </InsetRows>}
  </ModuleCard>;
}
