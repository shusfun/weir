import { Badge, Button, HealthRing, InsetRows, MetricStrip, Notice, PageHeader, SectionHeader, TextLink } from "@weir/ui";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { api } from "../../api";
import { State } from "../../components/feedback";
import { ObjectLink } from "../../components/object-link";
import type { Cert, Gate } from "../../lib/core";
import { date } from "../../lib/models";
import { overviewDemo } from "./health-overview-demo";
import { SetRow } from "./ops-overview";

const operationNames = {
  "worker.maintenance": "后台任务维护",
  "worker.tasks.list": "后台任务列表",
  "worker.tasks.statistics": "后台任务统计",
  "traffic.domains.country-data": "GeoIP 网段分发",
};
const operations = Object.keys(operationNames) as (keyof typeof operationNames)[];
const hotspotAnchor = "runtime-hotspots";
const operationAnchor = (operation: keyof typeof operationNames) => hotspotAnchor + "-" + operation;
const hotspotTarget = (anchor = hotspotAnchor) => "?tab=system#" + anchor;
type Health = "ok" | "bad" | "warn" | "unknown";
const sevenDays = 7 * 24 * 60 * 60 * 1000;
const maxAge = 90_000;
const refresh = { refetchInterval: 30_000, refetchIntervalInBackground: false, refetchOnWindowFocus: true } as const;

export type OverviewGate = Pick<Gate, "id" | "name" | "status" | "online" | "state" | "last_seen_at" | "config_revision" | "applied_revision" | "last_error">;
export type OverviewCert = Pick<Cert, "id" | "name" | "domain" | "not_after">;
type Hotspot = { operation: keyof typeof operationNames; calls: number; slow: number; failures: number; p95_ms: number | null; max_ms: number | null; last_anomaly_at: string | null; trace_id?: string };
export type Hotspots = { coverage_start: string; sampled_at: string; items: Hotspot[] };
type Reading<Data> = { data?: Data; loading?: boolean; error?: Error; retry?: () => void };

const noteworthy = (item: Hotspot) => item.slow > 0 || item.failures >= 3;
const milliseconds = (value: number | null) => value == null ? "未知" : value + " ms";
const timestamp = (value?: string) => value && !value.startsWith("0001-") ? Date.parse(value) : NaN;

function useReading<Data>(query: UseQueryResult<Data>, observedAt = query.dataUpdatedAt): Reading<Data> {
  const [, update] = useState(0);
  const staleAt = observedAt + maxAge;
  useEffect(() => {
    if (!(staleAt > Date.now()) || query.error || query.data === undefined) return;
    const timer = window.setTimeout(() => update(version => version + 1), staleAt - Date.now());
    return () => window.clearTimeout(timer);
  }, [staleAt, query.data, query.error]);
  const retry = () => { void query.refetch(); };
  if (query.error) return { error: query.error, retry };
  if (query.isPending) return { loading: true };
  if (!Number.isFinite(staleAt) || staleAt <= Date.now()) return { error: new Error("观测数据已过期或缺少采样时间，请重新加载。"), retry };
  return { data: query.data };
}

function useHotspots() {
  const query = useQuery({ ...refresh, queryKey: ["ops-hotspots"], queryFn: ({ signal }) => api("/admin/ops/hotspots", "GET", undefined, { signal }) as Promise<Hotspots> });
  return useReading(query, timestamp(query.data?.sampled_at));
}

function LiveOverview({ onDemo }: { onDemo: () => void }) {
  const gates = useQuery({ ...refresh, queryKey: ["observe-gates"], queryFn: async ({ signal }) => (await api("/admin/gates", "GET", undefined, { signal })).items as Gate[] });
  const certs = useQuery({ ...refresh, queryKey: ["certs"], queryFn: async ({ signal }) => (await api("/admin/certs", "GET", undefined, { signal })).items as Cert[] });
  return <OverviewContent gates={useReading(gates)} certs={useReading(certs)} hotspots={useHotspots()} onDemo={onDemo} />;
}

export function HealthOverview() {
  const [demo, setDemo] = useState(false);
  if (import.meta.env.DEV && demo) {
    const example = overviewDemo(Date.now());
    return <div className="stack">
      <Notice tone="info">
        <div className="actions justify-between">
          <strong className="text-[var(--weir-ink)]">演示数据 · 不代表实际运行状态</strong>
          <Button size="sm" variant="outline" onClick={() => setDemo(false)}>返回实时数据</Button>
        </div>
      </Notice>
      <OverviewContent demo gates={{ data: example.gates }} certs={{ data: example.certs }} hotspots={{ data: example.hotspots }} />
    </div>;
  }
  return <LiveOverview onDemo={() => setDemo(true)} />;
}

function gateIssue(gate: OverviewGate) {
  if (gate.state === "failed") return { rank: 0, label: "配置应用失败", tone: "bad" } as const;
  if (!Number.isFinite(timestamp(gate.last_seen_at))) return { rank: 3, label: "未知", tone: "neutral" } as const;
  if (!gate.online) return { rank: 1, label: "离线", tone: "bad" } as const;
  if (gate.state === "pending" || gate.config_revision !== gate.applied_revision) return { rank: 2, label: "配置未应用", tone: "warn" } as const;
}

function OverviewContent({ gates, certs, hotspots, demo = false, onDemo }: { gates: Reading<OverviewGate[]>; certs: Reading<OverviewCert[]>; hotspots: Reading<Hotspots>; demo?: boolean; onDemo?: () => void }) {
  const now = Date.now();
  const managed = gates.data?.filter(gate => gate.status === "active");
  const online = managed?.filter(gate => Number.isFinite(timestamp(gate.last_seen_at)) && gate.online).length;
  const unknownGates = managed?.filter(gate => !Number.isFinite(timestamp(gate.last_seen_at))).length;
  const gateIssues = managed?.flatMap(gate => {
    const issue = gateIssue(gate);
    return issue ? [{ gate, issue }] : [];
  }).sort((first, second) => first.issue.rank - second.issue.rank);
  const certExpiry = certs.data?.map(cert => ({ cert, expiry: timestamp(cert.not_after) }));
  const certIssues = certExpiry?.filter(({ expiry }) => !Number.isFinite(expiry) || expiry <= now + sevenDays)
    .sort((first, second) => (Number.isFinite(first.expiry) ? first.expiry : Infinity) - (Number.isFinite(second.expiry) ? second.expiry : Infinity));
  const unknownCerts = certExpiry?.filter(({ expiry }) => !Number.isFinite(expiry)).length;
  const expiring = certIssues?.filter(({ expiry }) => Number.isFinite(expiry)).length;
  const samples = hotspots.data?.items.filter(item => operations.includes(item.operation) && item.calls > 0);
  const ratio = (count: number | undefined, total: number, unknown = 0) => `${count == null || (count === 0 && unknown > 0) ? "未知" : count} / ${total}`;
  const alarms = samples?.filter(noteworthy).length ?? 0;
  const gateHealth: Health = gateIssues?.some(({ issue }) => issue.tone === "bad") ? "bad" : gateIssues?.some(({ issue }) => issue.tone === "warn") ? "warn" : !managed?.length || unknownGates ? "unknown" : "ok";
  const certHealth: Health = certIssues?.some(({ expiry }) => expiry <= now) ? "bad" : expiring ? "warn" : !certExpiry?.length || unknownCerts ? "unknown" : "ok";
  const hotspotHealth: Health = alarms ? "warn" : samples?.length === operations.length && samples.every(item => item.calls >= 5) ? "ok" : "unknown";
  const categories = [
    { label: "VPS", health: gateHealth, detail: managed?.length === 0 ? "未纳管" : undefined },
    { label: "证书", health: certHealth, detail: certExpiry?.length === 0 ? "无库存" : undefined },
    { label: "热点", health: hotspotHealth, detail: samples?.length ? "覆盖不全或样本不足" : samples ? "未采样" : undefined },
  ];
  const alerts = categories.filter(({ health }) => health === "bad" || health === "warn").length;
  const healthy = categories.filter(({ health }) => health === "ok").length;
  const observing = [gates, certs, hotspots].some(reading => reading.loading);
  const conclusion = alerts ? alerts + " 类运行告警" : healthy === categories.length ? "暂无运行异常" : observing ? "正在读取运行状态" : "观测尚未覆盖全部";
  const coverage = [unknownGates ? unknownGates + " 台 VPS 心跳未知" : "", unknownCerts ? unknownCerts + " 张证书有效期未知" : "", samples && samples.length < operations.length ? (operations.length - samples.length) + " 项热点未采样" : ""].filter(Boolean).join(" · ");

  return <div className="weir-observe-dashboard">
    <div className="stack min-w-0" data-tour="observe-readings">
      <PageHeader eyebrow="运行概况" title={conclusion} description={coverage || undefined} className="weir-workbench-heading" actions={import.meta.env.DEV && onDemo ? <Button size="sm" variant="outline" onClick={onDemo}>演示数据</Button> : undefined} />
      <MetricStrip className="weir-observe-metrics" items={[
        { label: "VPS 在线 / 纳管", value: managed ? ratio(online, managed.length, unknownGates) : null, caption: unknownGates ? `${unknownGates} 台心跳未知` : undefined },
        { label: "到期风险 / 证书", value: certExpiry ? ratio(expiring, certExpiry.length, unknownCerts) : null, caption: unknownCerts ? `${unknownCerts} 张有效期未知` : "含已过期" },
        { label: "异常操作 / 已采样", value: samples ? ratio(samples.length ? samples.filter(noteworthy).length : undefined, samples.length) : null, caption: samples && samples.length < operations.length ? `${samples.length} / ${operations.length} 项已采样` : "近 15 分钟" },
      ]} />
    </div>

    <aside className="weir-observe-health" aria-label="观测分类状态">
      <div aria-hidden="true"><HealthRing segments={categories.map(({ health }) => health)} title={healthy + " / " + categories.length} caption="已确认正常" size={203} /></div>
      <InsetRows as="ul" className="weir-observe-legend">
        {categories.map(({ label, health, detail }) => <li key={label} className="flex flex-wrap items-center justify-between gap-[var(--weir-space-2)] py-[var(--weir-space-1)]"><span>{label}</span><Badge variant="status" tone={health === "unknown" ? "neutral" : health}>{health === "ok" ? "正常" : health === "unknown" ? detail ?? "未知" : "有告警"}</Badge></li>)}
      </InsetRows>
    </aside>

    <div className="weir-observe-inventory stack min-w-0">
      <section className="stack-tight">
        <SectionHeader title="VPS 状态" flush actions={!demo && <TextLink render={<Link to="/gates" />}>管理 VPS</TextLink>} />
        {!managed || !gateIssues ? <ReadingState reading={gates} /> : !managed.length ? <State empty="尚未纳管 VPS" detail="前往「管理 VPS」添加设备，收到心跳后即可看到在线状态。" />
          : gateIssues.length ? <>
            <small>{managed.length - gateIssues.length} 台运行正常 · {gateIssues.length} 台有异常或待观测</small>
            <InsetRows>{gateIssues.map(({ gate, issue }) => <SetRow key={gate.id}
              title={demo ? gate.name : <ObjectLink to={{ type: "gate", id: gate.id }}>{gate.name}</ObjectLink>}
              description={<span className="break-words">{gate.state === "failed" && <>请查看配置回执 · </>}{gate.config_revision !== gate.applied_revision && <>已应用 r{gate.applied_revision}，目标 r{gate.config_revision} · </>}{Number.isFinite(timestamp(gate.last_seen_at)) ? `最近心跳 ${date(gate.last_seen_at)}` : "尚未收到心跳"}</span>}
              end={<Badge variant="status" tone={issue.tone}>{issue.label}</Badge>} />)}
              {demo && managed.filter(gate => !gateIssue(gate)).map(gate => <SetRow key={gate.id} title={gate.name} description="配置已应用" end={<Badge variant="status" tone="neutral">在线</Badge>} />)}
            </InsetRows>
          </> : <State empty={`${managed.length} 台 VPS 运行正常`} detail="设备均在线，配置已应用。" />}
      </section>

      <section className="stack-tight">
        <SectionHeader title="证书到期" flush actions={!demo && <TextLink render={<Link to="/gates?certs=1" />}>管理证书</TextLink>} />
        {!certExpiry || !certIssues ? <ReadingState reading={certs} /> : !certExpiry.length ? <State empty="尚无证书" detail="添加证书后，这里会提醒即将到期或已过期的证书。" />
          : certIssues.length ? <>
            <small>{certExpiry.length - certIssues.length} 张有效期超过 7 天 · {expiring} 张到期风险{unknownCerts ? ` · ${unknownCerts} 张有效期未知` : ""}</small>
            <InsetRows>{certIssues.map(({ cert, expiry }) => <SetRow key={cert.id}
              title={demo ? cert.name || cert.domain : <ObjectLink to={{ type: "cert", id: cert.id }}>{cert.name || cert.domain}</ObjectLink>}
              description={Number.isFinite(expiry) ? `到期时间 ${date(cert.not_after)}` : "到期时间未知"}
              end={!Number.isFinite(expiry) ? <Badge variant="status" tone="neutral">未知</Badge> : expiry <= now ? <Badge variant="status" tone="bad">已过期</Badge> : <Badge variant="status" tone="warn">7 天内到期</Badge>} />)}
              {demo && certExpiry.filter(({ expiry }) => expiry > now + sevenDays).map(({ cert }) => <SetRow key={cert.id} title={cert.name} description={`到期时间 ${date(cert.not_after)}`} end={<Badge variant="status" tone="neutral">有效</Badge>} />)}
            </InsetRows>
          </> : <State empty={`${certExpiry.length} 张证书暂无到期风险`} detail="所有证书有效期均超过 7 天。" />}
      </section>
    </div>

    <section className="weir-observe-hotspots stack-tight min-w-0">
      <SectionHeader title="运行热点" flush actions={!demo && <TextLink render={<Link to={hotspotTarget()} />}>查看运行明细</TextLink>} />
      <HotspotReading reading={hotspots} link={!demo} />
    </section>
  </div>;
}

function ReadingState({ reading }: { reading: Reading<unknown> }) {
  return <State loading={reading.loading} error={reading.error} onRetry={reading.retry} empty="观测状态未知" />;
}

function HotspotReading({ reading, link = false, details = false }: { reading: Reading<Hotspots>; link?: boolean; details?: boolean }) {
  return <>
    {reading.data ? <small>近 15 分钟 · 覆盖起点 {Number.isFinite(timestamp(reading.data.coverage_start)) ? date(reading.data.coverage_start) : "未知"}</small> : <ReadingState reading={reading} />}
    <InsetRows>{operations.map(operation => {
      const item = reading.data?.items.find(row => row.operation === operation);
      const calls = item?.calls ?? 0;
      const status = !item || !calls ? reading.data ? "未采样" : "未知" : item.failures >= 3 ? item.slow ? "重复失败、慢调用" : "重复失败" : item.slow ? "慢调用" : calls < 5 ? "样本不足" : "未达告警阈值";
      return <SetRow key={operation} id={details ? operationAnchor(operation) : undefined}
        title={link ? <TextLink render={<Link to={hotspotTarget(operationAnchor(operation))} />}>{operationNames[operation]}</TextLink> : operationNames[operation]}
        description={item && calls ? <span className="break-words">{calls} 次调用 · {item.failures} 失败 · {item.slow} 慢调用 · P95 {milliseconds(item.p95_ms)}{details && <><br />最大耗时 {milliseconds(item.max_ms)} · 最近异常 {item.last_anomaly_at ? date(item.last_anomaly_at) : "无"}{item.trace_id && <><br />trace <code className="break-all">{item.trace_id}</code></>}</>}</span> : reading.data ? "窗口内没有样本" : undefined}
        end={<Badge variant="status" tone={item && noteworthy(item) ? "warn" : "neutral"}>{status}</Badge>} />;
    })}</InsetRows>
  </>;
}

export function HotspotDetails() {
  const hotspots = useHotspots();
  const { key, hash } = useLocation();
  useEffect(() => {
    const target = hash.slice(1);
    if (target !== hotspotAnchor && !operations.some(operation => target === operationAnchor(operation))) return;
    const row = document.getElementById(target);
    row?.scrollIntoView({ block: "start" });
    if (target !== hotspotAnchor) row?.focus({ preventScroll: true });
  }, [key, hash]);
  return <section id={hotspotAnchor} className="stack">
    <SectionHeader title="运行热点" flush />
    <HotspotReading reading={hotspots} details />
  </section>;
}
