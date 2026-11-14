import { GateManagement } from "./gate-management";
import { PendingExits } from "./pending-exits";
import { GateLayaHosting } from "../features/ai/laya-gate-hosting";

function KernelTimeline({ events }: { events?: KernelEvent[] }) {
  if (!events) return <small>断线记录未知</small>;
  if (!events.length) return <small>还没有内核启停记录</small>;
  return <Timeline label="断线时间线">{events.map((e, i) => {
    const tone = e.kind === "crash" || e.kind === "stop" ? "fail" : e.kind === "hot_users" ? "hot" : e.kind === "start" || e.kind === "recover" ? "restart" : "unknown";
    const pid = e.pid == null ? "进程号未知" : `PID ${e.pid}`;
    const drops = e.disconnects == null ? "断连数未知" : `断开 ${e.disconnects}`;
    return <TimelineItem key={`${e.at}-${i}`} time={e.at} tone={tone} title={`${e.kernel || "内核"} ${e.kind || "未知"}`} description={`${pid} · ${drops}${e.reason ? ` · ${e.reason}` : ""}`} trace={e.trace_id} />;
  })}</Timeline>;
}
// VPS 详情：主机观测（缺失显示「未知」）、本机其他程序占用的端口、内核版本锁定、内核诊断 / 重启、节点出口关系。
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useState } from "react";
import { useQuery } from "../lib/query";
import { Badge, Button, Card, Field, GlassFolder, HealthRing, InsightBanner, Notice, ObjectHeader, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Sheet, SheetContent, Tabs, TabsList, TabsPanel, TabsTab, Timeline, TimelineItem, useConfirm } from "@weir/ui";
import { GateHealthList, healthItems } from "./gate-health";
import { confidenceText, reopenDrafts, useDrafts } from "./draft-confirm";
import { protocolNames, relativeTime } from "../lib/core";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { GateUpdateLine, gateLags } from "./gate-version";
import { ActionsWait } from "./wait";
import { State } from "./feedback";
import { bytes, date } from "../lib/models";
import type { Gate, KernelEvent, NodeItem } from "../lib/core";
import { KernelDiagnostics, type KernelInfo } from "../pages/kernels";
import { GateCerts } from "./node-certs";
import { ProcessUsage, freshHost } from "./resource-usage";
import { NodeSummary } from "./node-summary";
import { HistoryCharts } from "./history-charts";
import { GateTopology } from "./gate-topology";

import { ObjectLink } from "./object-link";
import { exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { useConsoleContext } from "../lib/console-context";

interface Port {
  port: number;
  proto: string;
  process?: string;
}

const unknown = "未知";
export const pct = (v: unknown) => (typeof v === "number" ? v.toFixed(1) + "%" : unknown);
export const size = (v: unknown) => (typeof v === "number" ? bytes(v) : unknown);
const uptime = (v: unknown) => (typeof v === "number" ? `${Math.floor(v / 86400)} 天 ${Math.floor((v % 86400) / 3600)} 小时` : unknown);

export function GateDetail({ gate, nodes, close, layout = "sheet" }: { gate: Gate & { listening_ports?: Port[]; kernel_pins?: Record<string, string>; xray_stats_port?: number; singbox_stats_port?: number }; nodes: NodeItem[]; close: () => void; layout?: "sheet" | "page" }) {
  const [localTab, setLocalTab] = useState("overview");
  const [params, setParams] = useSearchParams();
  const tab = layout === "page" ? params.get("tab") || "overview" : localTab;
  const setTab = (value: string) => {
    if (layout === "page") setParams((p) => { p.set("tab", value); return p; }, { replace: true });
    else setLocalTab(value);
  };
  useConsoleContext({ dialog: { title: "VPS 详情", step: ({ overview: "概览", nodes: "节点与出口", kernels: "内核", certs: "证书", health: "体检", timeline: "断线时间线" } as Record<string, string>)[tab] } });
  const drafts = useDrafts();
  const confirm = useConfirm();
  const kernels = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, KernelInfo> });
  const [pins, setPins] = useState<Record<string, string>>(gate.kernel_pins ?? {});
  const navigate = useNavigate();
  const [diag, setDiag] = useState<{ kernel: string; result: Record<string, unknown> } | null>(null);
  const h = freshHost(gate.host,gate.online) ?? {};
  // 本项目自己的端口：节点端口与两个内核的统计端口（只监听回环）。
  const own = new Set([...nodes.filter((n) => n.gate_id === gate.id).map((n) => n.port), gate.xray_stats_port ?? 0, gate.singbox_stats_port ?? 0]);
  const foreign = (gate.listening_ports ?? []).filter((p) => !own.has(p.port));
  const { run, isBusy, waitFor } = useRowAction<string>();
  const mine = nodes.filter((n) => n.gate_id === gate.id);
  const exitName = (n: NodeItem) => {
    if (n.exit.kind === "freedom") return "直连";
    if (n.exit.kind === "source") return "订阅源节点";
    if (n.exit.kind !== "node") return `${exitKindName[n.exit.kind]} ${n.exit.address ?? ""}${n.exit.port ? ":" + n.exit.port : ""}`;
    const x = nodes.find((y) => y.id === n.exit.node_id);
    return <>经 <ObjectLink to={{ type: "node", id: n.exit.node_id ?? undefined, name: x?.name ?? "节点" }} />{x?.gate_name ? `（${x.gate_name}）` : ""}</>;
  };
  const fw = gate.firewall;
  const gateDraft = (drafts.data ?? []).find((d) => d.gate_id === gate.id);
  const body = (
    <>
        <div className={layout === "page" ? "stack" : "stack p-4"}>
          <GateUpdateLine gate={gate} />
          <PendingExits all gateId={gate.id} />
          <GateLayaHosting gateId={gate.id} host={gate.public_host} />

          <Tabs value={tab} onValueChange={v => setTab(String(v))}>
            <TabsList aria-label="VPS 详情">
              <TabsTab value="overview">概览</TabsTab>
              <TabsTab value="nodes">节点与出口 {mine.length}</TabsTab>
              <TabsTab value="kernels">内核</TabsTab>
              <TabsTab value="certs">证书</TabsTab>
              <TabsTab value="health">体检</TabsTab>
              <TabsTab value="timeline">断线时间线</TabsTab>
            </TabsList>
            <TabsPanel value="health">
              <div className="grid items-start gap-[29px] lg:grid-cols-[301px_minmax(0,1fr)]">
                <Card className="flex flex-col items-center p-[21px] text-center">
                  <HealthRing segments={healthItems(h.health).map((i) => (i.status === "bad" ? "bad" : i.status === "warn" ? "warn" : i.status === "unknown" && i.label === "未知" ? "unknown" : "ok"))} title={`${healthItems(h.health).length} 项`} caption={(() => { const n = healthItems(h.health).filter((i) => i.status === "bad" || i.status === "warn").length; return n ? `${n} 项需要处理` : "没有需要处理的项"; })()} />
                  <p className="mt-[9px] mb-0 text-meta text-[var(--weir-ink-3)]">每次心跳随主机观测上报，不改系统、不重启内核</p>
                </Card>
                <Card className="overflow-hidden p-0"><GateHealthList health={h.health} /></Card>
              </div>
            </TabsPanel>
            <TabsPanel value="timeline" className="stack">
              <KernelTimeline events={gate.kernel_events} />
            </TabsPanel>
            <TabsPanel value="overview" className="stack">
          <SectionHeader title="主机" description="由 VPS 读取本机 /proc 与磁盘信息上报；网卡总量可能包含虚拟网卡，不作为用户计费依据。" />
          <dl className="grid grid-cols-2 gap-2 text-body">
            <dt>CPU</dt><dd>{pct(h.cpu_percent)}</dd>
            <dt>负载</dt><dd>{typeof h.load1 === "number" ? h.load1.toFixed(2) : unknown}</dd>
            <dt>内存</dt><dd>{size(h.mem_used)} / {size(h.mem_total)}</dd>
            <dt>磁盘</dt><dd>{size(h.disk_used)} / {size(h.disk_total)}</dd>
            <dt>网卡累计</dt><dd>↓ {size(h.net_rx_bytes)} · ↑ {size(h.net_tx_bytes)}</dd>
            <dt>运行时间</dt><dd>{uptime(h.uptime_secs)}</dd>
          </dl>
          <SectionHeader title="流量与主机曲线" description="最近 1 / 6 / 24 小时；VPS 离线或读取失败的时段显示为未知，不补零。" />
          <HistoryCharts path={`/admin/gates/${gate.id}/history`} host />
          <SectionHeader title="本机防火墙" description="只读：节点端口没有放行时在节点上提示，Weir 不改防火墙规则；云厂商的安全组读不到。" />
          <small>
            {!fw ? unknown : fw.error ? `${fw.tool}：读取失败（${fw.error}）` : fw.tool === "freedom" ? "没有检测到防火墙" : !fw.active ? `${fw.tool} 未启用（全部放行）` : `${fw.tool} 已启用，放行 ${fw.rules?.length ? fw.rules.join("、") : "无端口"}`}
          </small>
          <SectionHeader title="其他程序占用的端口" description="建节点时自动避开这些端口。" />
          {foreign.length === 0 ? <small>{gate.listening_ports ? "没有" : unknown}</small> : (
            <div className="flex flex-wrap gap-1.5">{foreign.map((p) => <Badge key={p.proto + p.port} variant="soft" tone="neutral">{p.port}/{p.proto}{p.process ? ` ${p.process}` : ""}</Badge>)}</div>
          )}
            </TabsPanel>
            <TabsPanel value="kernels" className="stack">
          <SectionHeader title="内核" description="可为这台 VPS 锁定版本；不锁定时使用默认版本。" />
          {kernels.isPending || kernels.error ? <State loading={kernels.isPending} error={kernels.error} onRetry={() => void kernels.refetch()} /> : (["xray", "sing-box"] as const).map((k) => {
            const cached = (kernels.data[k]?.versions ?? []).filter((v) => v.arches[gate.arch] && v.driver).map((v) => v.version);
            return (
              <div key={k} className="stack rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
                <strong>{k} <small className="font-normal text-[var(--weir-ink-3)]">运行中 {gate.kernels_running?.[k] ? <ObjectLink to={{ type: "kernel", key: `${k}@${gate.kernels_running[k]}`, name: gate.kernels_running[k] }} /> : "无"}</small></strong>
                {cached.length === 0 && <small className="text-[var(--weir-ink-3)]">中心还没有适用 {gate.arch} 且有驱动的 {k} 版本，<ObjectLink to={{ type: "kernel", key: k, name: "去内核页下载" }} />。</small>}
                <ProcessUsage host={gate.host} online={gate.online} names={[k]} />
                <HistoryCharts path={`/admin/gates/${gate.id}/history`} traffic={false} processes={[k]} />
                <Field label={k + " 版本"}>
                  <Select value={pins[k] ?? ""} onValueChange={(v) => setPins({ ...pins, [k]: String(v) })}>
                    <SelectTrigger><SelectValue>{pins[k] || `跟随默认（${kernels.data[k]?.default || "保持当前运行版本"}）`}</SelectValue></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="">跟随默认</SelectItem>
                      {cached.map((v) => <SelectItem key={v} value={v}>{v}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </Field>
                <div className="actions">
                  <Button size="sm" variant="outline" disabled={isBusy() || !gate.online} busy={isBusy("d" + k)} busyText="正在诊断…" onClick={() => void run("d" + k, async () => setDiag({ kernel: k, result: (await api(`/admin/gates/${gate.id}/kernels/${k}/diagnostics`, "POST", {})).result }), "已读取诊断", { wait: { label: `正在读取 ${gate.name} 上 ${k} 的诊断`, max: 30 } })}>诊断</Button>
                  <Button size="sm" variant="outline" disabled={isBusy() || !gate.online} onClick={async () => (await confirm({ title: `重启 ${gate.name} 上的 ${k}？`, description: "连接会短暂中断。", confirmText: "重启" })) && run("r" + k, () => api(`/admin/gates/${gate.id}/kernels/${k}/restart`, "POST", {}), "已重启", { wait: { label: `正在重启 ${gate.name} 上的 ${k}`, max: 30 } })} busy={isBusy("r" + k)} busyText="正在重启…">重启</Button>
                  <ActionsWait wait={waitFor("d" + k) || waitFor("r" + k)} />
                </div>
                {diag?.kernel === k && <KernelDiagnostics result={diag.result} />}
              </div>
            );
          })}
          <div className="actions"><Button disabled={isBusy()} busy={isBusy("pins")} busyText="正在保存…" onClick={() => void run("pins", () => api("/admin/gates/" + gate.id, "PATCH", { kernel_pins: Object.fromEntries(Object.entries(pins).filter(([, v]) => v)) }), "版本设置已保存")}>保存版本设置</Button></div>
          {!gate.online && <Notice tone="warn">VPS 离线：诊断与重启不可用。</Notice>}
            </TabsPanel>
            <TabsPanel value="certs" className="stack">
          <GateCerts gate={gate} />
            </TabsPanel>
            <TabsPanel value="nodes" className="stack">
          <SectionHeader title="节点与出口" description="入口节点 → 出口。" actions={<Button size="sm" variant="outline" onClick={() => navigate(`/exits?gate=${gate.id}`)}>出口与分流</Button>} />
          <GateManagement gate={gate} nodes={nodes} />
          <SectionHeader title="关系图" description="用户 → 入口节点 → 路由（用户自建链式代理、分流规则）→ 出口（只读）；点击跳到对应用户、节点、VPS、订阅源或出口与分流。" />
          <GateTopology gateId={gate.id} />
            </TabsPanel>
          </Tabs>
        </div>
    </>
  );
  if (layout === "page") {
    return (
      <div className="stack">
        <ObjectHeader
          back={<Link to="/gates">VPS 与节点</Link>}
          crumb={gate.name}
          actions={<Button variant="outline" onClick={() => setTab("nodes")}>管理节点与 VPS</Button>}
          tone={gate.status !== "active" ? "off" : !gate.online ? "bad" : gate.state === "failed" ? "bad" : gate.state === "pending" ? "warn" : "ok"}
          status={`${gate.status !== "active" ? "已撤销" : gate.online ? "在线" : "离线"} · ${gate.last_seen_at ? relativeTime(gate.last_seen_at) + "心跳" : "心跳未知"} · Gate ${gate.version || "版本未知"}`}
          title={gate.name}
          meta={[gate.public_host || "等待上报地址", gate.arch || "架构未知"].join(" · ")}
          banner={gateDraft && <InsightBanner title={gateDraft.steps?.[0]?.title || gateDraft.explain || "有待确认的草稿"} confidence={confidenceText(gateDraft)} progress={`影响这台的 ${mine.length} 个节点 · 草稿已生成`} action={<Button onClick={reopenDrafts}>去确认</Button>} />}
          aside={<GlassFolder name={<span className="text-body font-normal text-[var(--weir-ink-2)]">{mine.length} 个节点 · {new Set(mine.map((n) => n.exit_id)).size} 个出口</span>} cpu={h.cpu_percent} headEnd={<span className="font-[family-name:var(--weir-mono)] text-meta">CPU {typeof h.cpu_percent === "number" ? `${Math.round(h.cpu_percent)}%` : "未知"} · 延迟未知</span>} files={mine.slice(0, 3).map((n) => ({ id: String(n.id), title: protocolNames[n.protocol] ?? n.name, meta: n.port ? String(n.port) : null }))} className="weir-folder-compact" />}
        />
        {body}
      </div>
    );
  }
  return (
    <Sheet open onOpenChange={(o) => !o && close()}>
      <SheetContent side="right" title={gate.name} description={`${gate.public_host || "等待上报地址"} · ${gate.arch} · Gate ${gate.version || "版本未知"}${gateLags(gate) ? `（未跟上中心 ${gate.center_version}）` : ""} · 最近心跳 ${date(gate.last_seen_at)}`} className="w-full sm:max-w-[640px]">
        {body}
      </SheetContent>
    </Sheet>
  );
}
