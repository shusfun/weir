import { GateManagement } from "../components/gate-management";
import { GateLayaHosting } from "../features/ai/laya-gate-hosting";
// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { Fragment, useState, useMemo, useCallback, useRef } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useEffect } from "react";
import { useQuery } from "../lib/query";
import {
  Badge, Button, Card, CardContent, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FolderSlot, GlassFolder, Input,
  InsightBanner, MetricStrip, Notice, PageHeader, Readout, SectionHeader, SkeletonCards, StatusLamp, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast, useConfirm, type FolderStatus, type LampTone,
} from "@weir/ui";
import { Pencil, Plus, Route, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh } from "../components/dsh";
import { ObjectLink, dataObject, useDialogParam } from "../components/object-link";
import { exitKindName, probeText } from "../components/chain-exit";
import { NodeAccess } from "../components/node-access";
import { CenterResources } from "../components/resource-usage";
import { GateCard } from "../components/gate-card";
import { NodeDetail } from "../components/node-detail";
import { GateEdit } from "../components/gate-edit";
const emptyNodes:NodeItem[]=[];

// 文件夹右上角的状态标签：只在有事时出现，写最要紧的一件（稿子「DNS 被污染」「宿主机抢 CPU」）。
function gateFolderStatus(g: Gate, tone: LampTone): FolderStatus | null {
  if (g.status !== "active") return { tone: "neutral", text: "已撤销" };
  if (!g.online) return { tone: "bad", text: "离线" };
  if (g.state === "failed") return { tone: "bad", text: "配置应用失败" };
  if (g.host?.health?.dns === "polluted") return { tone: "bad", text: "DNS 被污染" };
  if ((g.host?.health?.cpu_steal ?? 0) >= 10) return { tone: "warn", text: "宿主机抢 CPU" };
  if (tone === "warn") return { tone: "warn", text: "待生效" };
  return null;
}
import { GateDetail, pct, size } from "../components/gate-detail";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "../components/wait";
import { GateUpdateLine, GateVersion } from "../components/gate-version";
import { State } from "../components/feedback";
import { InviteGate } from "../components/invite-gate";
import { CertManager } from "../components/cert-manager";
import { NodeWizard } from "../components/node-wizard";
import { date } from "../lib/models";
import { rateText } from "../lib/linkmap";
import { field, useConsoleContext } from "../lib/console-context";
import { nodeStatus, protocolNames, relativeTime, type Gate, type NodeItem } from "../lib/core";
import { GettingStarted } from "../components/getting-started";
import { GateNodeTable } from "../components/gate-node-table";
import { confidenceText, reopenDrafts, useDrafts } from "../components/draft-confirm";

export function GatesPage() {
  const toast = useToast();
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[],
    // 有 VPS 在自更新时刷新快一些，进度才连得上
    refetchInterval: (q) => ((q.state.data ?? []).some((g) => g.self_update?.state === "downloading" || g.self_update?.state === "replacing") ? 2000 : 10_000) });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[], refetchInterval: 10_000 });
  const [invite, setInvite] = useState(false);
  // wizard：建节点向导；数字是从某台 VPS 的「＋ 协议」打开的（这台 VPS 已选好）
  const [wizard, setWizard] = useState<number | "any" | null>(null);
  const [edit, setEdit] = useState<Gate | null>(null);

  const refresh = () => Promise.all([gates.refetch(), nodes.refetch()]);
  // 地址栏（对象链接、dsh 的界面导航、关系图）：?gate=编号 是这台 VPS 的详情；?node=编号 是节点详情；?certs=1 是证书窗口。
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  useEffect(() => {
    const gate = params.get("gate");
    const node = params.get("node");
    if (gate) navigate(`/gates/${gate}`, { replace: true });
    else if (node) navigate(`/nodes/${node}`, { replace: true });
  }, [params, navigate]);
  useEffect(() => {
    const id = Number(params.get("new"));
    const editId = Number(params.get("edit"));
    if (id > 0 && gates.data?.some(g => g.id === id)) setWizard(id);
    if (editId > 0) { const target = gates.data?.find(g => g.id === editId); if (target) setEdit(target); }
  }, [params, gates.data]);
  const clearAction = () => setParams(p => { p.delete("new"); p.delete("edit"); return p; }, { replace: true });
  const detailParam = useDialogParam("gate", gates.data, !gates.isFetching);
  const nodeParam = useDialogParam("node", nodes.data, !nodes.isFetching);
  const detail = detailParam.current;
  const expanded = Number(params.get("expand"));
  const openGate = (g: Gate) => setParams(p => { if (expanded === g.id) p.delete("expand"); else p.set("expand", String(g.id)); return p; });
  const openNode=useCallback((n:NodeItem)=>navigate(`/nodes/${n.id}`),[navigate]);
  const previousGroups=useRef(new Map<number,NodeItem[]>());
  const grouped=useMemo(()=>{
    const groups=new Map<number,NodeItem[]>();
    for(const n of nodes.data??[]){const list=groups.get(n.gate_id)??[];list.push(n);groups.set(n.gate_id,list);}
    for(const [id,list] of groups){const previous=previousGroups.current.get(id);if(previous?.length===list.length&&list.every((n,i)=>n===previous[i]))groups.set(id,previous);}
    previousGroups.current=groups;
    return groups;
  },[nodes.data]);
  const certs = params.get("certs") === "1";
  const setCerts = (on: boolean) => setParams((p) => { if (on) p.set("certs", "1"); else p.delete("certs"); return p; }, { replace: !on });
  const offline = (gates.data ?? []).filter((g) => g.status === "active" && !g.online).map((g) => `VPS「${g.name}」离线`);
  const pending = (gates.data ?? []).filter((g) => g.state === "pending").map((g) => `VPS「${g.name}」配置待生效`);
  const failed = (gates.data ?? []).filter((g) => g.state === "failed").map((g) => `VPS「${g.name}」配置应用失败：${g.last_error}`);
  useConsoleContext({
    page: "VPS 与节点",
    list: { total: gates.data?.length ?? 0, items: (gates.data ?? []).slice(0, 20).map((g) => ({ type: "gate", name: g.name, id: g.id })) },
    notices: [...offline, ...pending, ...failed],
  });
  useConsoleContext(detail ? { object: { type: "gate", name: detail.name, id: detail.id }, dialog: { title: "VPS 详情" } } : null);
  const drafts = useDrafts();
  const listed = gates.data ?? [];
  const onlineCount = listed.filter((g) => g.status === "active" && g.online).length;
  const attention = listed.filter((g) => g.status === "active" && (!g.online || g.state === "failed" || g.state === "pending")).length;
  const header = (
    <PageHeader
      size="hero"
      eyebrow="VPS 与节点"
      title={listed.length === 0 ? "还没有 VPS" : <>{onlineCount} 台 VPS 在线{attention > 0 && <><br />{attention} 件事要处理</>}</>}
      description={attention > 0 ? (failed[0] || pending[0] || offline[0]) : "每个节点都会进入用户的订阅。VPS 用 SSH 一次装好，之后建节点、改配置都在这里完成。"}
      actions={
        <>
          <Button data-tour="new-node" variant="outline" disabled={!gates.data?.some((g) => g.status === "active")} onClick={() => setWizard("any")}><Plus />新建节点</Button>
          <Button onClick={() => setInvite(true)}><Server />添加 VPS</Button>
        </>
      }
    />
  );
  const gateOf = (n: NodeItem) => gates.data?.find((g) => g.id === n.gate_id);
  const dialogs = (
    <>
      <InviteGate open={invite} close={() => setInvite(false)} done={() => void gates.refetch()} />
      <CertManager open={certs} close={() => setCerts(false)} />
      {detail && <GateDetail gate={detail} nodes={nodes.data ?? []} close={detailParam.close} />}
      {nodeParam.current && <NodeDetail node={nodeParam.current} gate={gateOf(nodeParam.current)} nodes={nodes.data ?? []} close={nodeParam.close} changed={() => void refresh()} />}

      {edit && <GateEdit gate={edit} close={() => { setEdit(null); clearAction(); }} saved={() => { setEdit(null); clearAction(); toast("已保存", { tone: "ok" }); void refresh(); }} />}
      <NodeWizard
        open={wizard !== null}
        gateId={typeof wizard === "number" ? wizard : undefined}
        close={() => { setWizard(null); clearAction(); }}
        done={() => { toast("节点已创建", { tone: "ok" }); void refresh(); }}
        gates={gates.data ?? []}
        nodes={nodes.data ?? []}
      />
    </>
  );
  if (gates.isPending || nodes.isPending || gates.error || nodes.error)
    return (
      <div className="stack">
        {header}
        <CenterResources />
        <State loading={gates.isPending || nodes.isPending} error={gates.error ?? nodes.error} onRetry={refresh} skeleton={<SkeletonCards count={2} lines={2} />} />
        {dialogs}
      </div>
    );
  return (
    <div className="stack" data-tour-online={gates.data.some(g => g.status === "active" && g.online)} data-tour-has-nodes={nodes.data.length > 0}>
      {header}
      {drafts.data && drafts.data.length > 0 && <InsightBanner className="mt-[25px]" title={drafts.data[0].steps?.[0]?.title || drafts.data[0].explain || "有待确认的草稿"} confidence={confidenceText(drafts.data[0])} progress={`${drafts.data[0].source === "analysis" ? "dsh 已起草" : "dsh 对话起草"} · 待你确认`} more={Math.max(0, drafts.data.length - 1)} action={<Button onClick={reopenDrafts}>去确认</Button>} />}
      <MetricStrip items={[
        { label: "可用节点", value: nodes.data ? `${nodes.data.filter((n) => nodeStatus(n, gates.data.find((g) => g.id === n.gate_id))[1] === "ok").length} / ${nodes.data.length}` : null },
        { label: "此刻总速率", value: null, unit: "B/s" },
        { label: "24 小时内核重启", value: null },
      ]} />
      <SectionHeader className="mt-[33px]" title="VPS" actions={<span className="text-meta text-[var(--weir-ink-3)]">点开文件夹管理节点与 Laya</span>} />
      <div className="grid gap-[21px] md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
        {gates.data.map((g, i) => {
          const folderNodes = grouped.get(g.id) ?? emptyNodes;
          const tone: LampTone = g.status !== "active" ? "off" : !g.online || g.state === "failed" ? "bad" : g.state === "pending" ? "warn" : "ok";
          const cpu = g.host?.cpu_percent;
          const steal = g.host?.health?.cpu_steal;
          const caption = [cpu == null ? "CPU 未知" : `CPU ${Math.round(cpu)}%`, steal != null && steal >= 5 ? `steal ${Math.round(steal)}%` : null, `${folderNodes.length} 个节点`].filter(Boolean).join(" · ");
          return <section key={g.id} className={expanded === g.id ? "weir-expanded-folder stack" : "stack"}><GlassFolder expanded={expanded === g.id} controls={`gate-content-${g.id}`} hue={(i % 2) as 0 | 1} name={g.name} address={g.public_host || null} status={gateFolderStatus(g, tone)} cpu={cpu} caption={caption} metric={null} metricUnit="ms" metricLabel="国内延迟" onClick={() => openGate(g)} files={folderNodes.slice(0, 3).map((n) => ({ id: String(n.id), title: protocolNames[n.protocol] ?? n.name, meta: n.port ? String(n.port) + (n.network === "udp" ? "/udp" : "") : null, tone: nodeStatus(n, g)[1] }))} />{expanded === g.id && <div id={`gate-content-${g.id}`} className="stack"><GateManagement gate={g} nodes={nodes.data} details /><GateLayaHosting gateId={g.id} host={g.public_host} /></div>}</section>;
        })}
        <FolderSlot title="添加 VPS" caption="用 SSH 一次装好" onClick={() => setInvite(true)} />
      </div>
      <GateNodeTable gates={gates.data} nodes={nodes.data} openNode={openNode} />
      <GettingStarted gates={gates.data} nodes={nodes.data} addGate={() => setInvite(true)} addNode={() => setWizard("any")} />
      <SectionHeader className="mt-[33px]" title="中心" />
      <CenterResources />
      {dialogs}
    </div>
  );
}
