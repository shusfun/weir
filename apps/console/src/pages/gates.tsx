import { recordFolderMotion } from "../lib/folder-motion";
import { SyncLine } from "../components/sync-line";
import { GateManagement } from "../components/gate-management";
// VPS 与节点：上方切换「VPS」（玻璃文件夹 + 紧凑协议行）与「全部节点」（自有节点筛选表 + 跨来源统一目录）。
import { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import {
  Badge, Button, FolderGrid, FolderSlot, GlassFolder,
  InsightBanner, MetricStrip, PageHeader, SectionHeader, Segmented, SkeletonCards, useToast, type FolderStatus, type LampTone,
} from "@weir/ui";
import { Plus, Server } from "lucide-react";
import { useDialogParam } from "../components/object-link";
import { nodeChainRoute } from "../components/node-summary";
import { ExternalNodeCatalog } from "../components/node-catalog";
import { CenterResources } from "../components/resource-usage";
import { NodeDetail } from "../components/node-detail";
import { GateEdit } from "../components/gate-edit";
const emptyNodes: NodeItem[] = [];

// 文件夹右上角的状态标签：只在有事时出现，写最要紧的一件（稿子「DNS 被污染」「宿主机抢 CPU」）。
function gateFolderStatus(g: Gate, tone: LampTone): FolderStatus | null {
  const [label, stateTone] = objectStatus(g.object_state);
  if (stateTone === "ok") return null;
  return { tone: stateTone === "bad" ? "bad" : stateTone === "warn" ? "warn" : "neutral", text: label };
}
import { GateDetail } from "../components/gate-detail";
import { api } from "../api";
import { State } from "../components/feedback";
import { InviteGate } from "../components/invite-gate";
import { CertManager } from "../components/cert-manager";
import { NodeWizard } from "../components/node-wizard";
import { useConsoleContext } from "../lib/console-context";
import { objectStatus, nodeStatus, protocolNames, type Gate, type NodeItem } from "../lib/core";
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
  const focusParam = params.get("focus") ?? "";
  const view = params.get("view") === "nodes" || focusParam.startsWith("node:") ? "nodes" : "vps";
  const setView = (next: string) => setParams(p => { if (next === "nodes") p.set("view", "nodes"); else p.delete("view"); return p; }, { replace: true });
  useEffect(() => {
    const id = Number(params.get("new"));
    const editId = Number(params.get("edit"));
    if (id > 0 && gates.data?.some(g => g.id === id)) setWizard(id);
    if (editId > 0) { const target = gates.data?.find(g => g.id === editId); if (target) setEdit(target); }
  }, [params, gates.data]);
  const clearAction = () => setParams(p => { p.delete("new"); p.delete("edit"); return p; }, { replace: true });
  const detailParam = useDialogParam("gate", gates.data, gates.isSuccess);
  const nodeParam = useDialogParam("node", nodes.data, nodes.isSuccess);
  const detail = detailParam.current;
  const expanded = Number(params.get("expand"));
  const setExpanded = (id: string | null) => setParams(p => { if (id) p.set("expand", id); else p.delete("expand"); return p; });
  const openNode=useCallback((n:NodeItem)=>nodeParam.open(n),[nodeParam]);
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
  const activeGates = listed.filter((g) => g.status === "active");
  const onlineCount = activeGates.filter((g) => g.online).length;
  const attention = activeGates.filter((g) => !g.online || g.state === "failed" || g.state === "pending").length;
  const dayAgo = Date.now() - 86400_000;
  const restarts24h = activeGates.length > 0 && activeGates.every((g) => g.online && Array.isArray(g.kernel_events))
    ? activeGates.reduce((sum, g) => sum + (g.kernel_events ?? []).filter((e) => (e.kind === "crash" || e.kind === "recover" || e.kind === "start") && e.at && Date.parse(e.at) >= dayAgo).length, 0)
    : null;
  const header = (
    <PageHeader
      size="hero"
      title={listed.length === 0 ? "还没有 VPS" : <>{onlineCount} 台 VPS 在线{attention > 0 && <><br />{attention} 件事要处理</>}</>}
      description={<>{attention > 0 ? (failed[0] || pending[0] || offline[0]) : listed.length === 0 ? "用 SSH 一键接入 VPS 后，建节点、配证书与托管 Laya 都在这里完成。" : null} <SyncLine /></>}
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
      {drafts.data && drafts.data.length > 0 && <InsightBanner className="mt-[var(--weir-gap-25)]" title={drafts.data[0].steps?.[0]?.title || drafts.data[0].explain || "有待确认的草稿"} confidence={confidenceText(drafts.data[0])} progress={`${drafts.data[0].source === "analysis" ? "dsh 已起草" : "dsh 对话起草"} · 待你确认`} more={Math.max(0, drafts.data.length - 1)} action={<Button onClick={reopenDrafts}>去确认</Button>} />}
      <MetricStrip items={[
        { label: "在线 VPS", value: `${onlineCount} / ${activeGates.length}` },
        { label: "可用节点", value: `${nodes.data.filter((n) => nodeStatus(n, gates.data.find((g) => g.id === n.gate_id))[1] === "ok").length} / ${nodes.data.length}` },
        { label: "24 小时内核启停", value: restarts24h },
        { label: "待确认草稿", value: drafts.data ? drafts.data.length : null },
      ]} />
      <div className="mt-[var(--weir-gap-25)] flex flex-wrap items-center justify-between gap-3">
        <Segmented aria-label="节点视图" value={view} onValueChange={setView} options={[["vps", `VPS ${gates.data.length}`], ["nodes", `全部节点 ${nodes.data.length}`]]} />
        <span className="text-meta text-[var(--weir-ink-3)]">{view === "vps" ? "点开文件夹快速管理协议与流量额度；完整诊断与 Laya 在详情里打开" : "按异常或协议筛选自有节点，并查看跨来源统一目录"}</span>
      </div>
      {view === "vps" ? (
        <>
          <FolderGrid expandedId={expanded ? String(expanded) : null} onExpandedChange={setExpanded} onMotion={recordFolderMotion} items={gates.data.map((g, i) => {
            const folderNodes = grouped.get(g.id) ?? emptyNodes;
            const tone: LampTone = g.status !== "active" ? "off" : !g.online || g.state === "failed" ? "bad" : g.state === "pending" ? "warn" : "ok";
            const cpu = g.host?.cpu_percent;
            const steal = g.host?.health?.cpu_steal;
            const memPct = g.online && g.host?.mem_total && g.host.mem_used != null ? Math.round((g.host.mem_used / g.host.mem_total) * 100) : null;
            const folderStatus = gateFolderStatus(g, tone);
            const chainedRoutes = folderNodes.map((n) => nodeChainRoute(n, nodes.data, gates.data)).filter((c): c is NonNullable<typeof c> => !!c);
            const caption = [cpu == null ? "CPU 未知" : `CPU ${Math.round(cpu)}%`, steal != null && steal >= 5 ? `steal ${Math.round(steal)}%` : null, `${folderNodes.length} 个节点`, chainedRoutes.length > 0 ? `${chainedRoutes.length} 条链式` : null].filter(Boolean).join(" · ");
            return {
              id: String(g.id), label: g.name,
              folder: (open: boolean) => <GlassFolder expanded={open} controls={`folder-content-${g.id}`} hue={(i % 2) as 0 | 1} name={g.name} address={g.public_host || null} status={folderStatus} headEnd={!folderStatus && chainedRoutes.length > 0 ? <Badge variant="soft" tone="info">{chainedRoutes.length === 1 ? `⇢ ${chainedRoutes[0].target}` : `⇢ ${chainedRoutes.length} 条链式`}</Badge> : undefined} cpu={cpu} caption={caption} metric={memPct} metricUnit="%" metricLabel="内存" onClick={() => setExpanded(open ? null : String(g.id))} files={folderNodes.slice(0, 3).map((n) => {
                const chain = nodeChainRoute(n, nodes.data, gates.data);
                const portMeta = n.port ? String(n.port) + (n.network === "udp" ? "/udp" : "") : "";
                return {
                  id: String(n.id),
                  chained: !!chain,
                  kind: chain ? `${chain.tag} ⇢` : "节点",
                  title: protocolNames[n.protocol] ?? n.name,
                  meta: chain ? `→ ${chain.target}` : portMeta || null,
                  tone: nodeStatus(n, g)[1],
                };
              })} />,
              content: <GateManagement gate={g} nodes={nodes.data} details compact />,
            };
          })}>
            <FolderSlot title="添加 VPS" caption="用 SSH 一次装好" onClick={() => setInvite(true)} />
          </FolderGrid>
          <GettingStarted gates={gates.data} nodes={nodes.data} addGate={() => setInvite(true)} addNode={() => setWizard("any")} />
          <SectionHeader flush className="mt-[var(--weir-gap-33)]" title="中心" />
          <CenterResources />
        </>
      ) : (
        <>
          <GateNodeTable gates={gates.data} nodes={nodes.data} openNode={openNode} />
          <ExternalNodeCatalog />
        </>
      )}
      {dialogs}
    </div>
  );
}
