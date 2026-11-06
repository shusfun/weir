// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { useState, useMemo, useCallback, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import {
  Badge, Button, Card, CardContent, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input,
  Notice, PageHeader, Readout, SkeletonCards, StatusLamp, SwitchField, useToast, useConfirm, type LampTone,
} from "@weir/ui";
import { Pencil, Plus, Route, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh } from "../components/dsh";
import { ObjectLink, dataObject, useDialogParam } from "../components/object-link";
import { exitKindName, probeText } from "../components/chain-exit";
import { NodeAccess } from "../components/node-access";
import { GateCard } from "../components/gate-card";
import { NodeDetail } from "../components/node-detail";
import { GateEdit } from "../components/gate-edit";
const emptyNodes:NodeItem[]=[];
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
  const detailParam = useDialogParam("gate", gates.data, !gates.isFetching);
  const nodeParam = useDialogParam("node", nodes.data, !nodes.isFetching);
  const detail = detailParam.current;
  const openGate=useCallback((g:Gate)=>setParams(p=>{p.set("gate",String(g.id));return p;}),[setParams]);
  const openNode=useCallback((n:NodeItem)=>setParams(p=>{p.set("node",String(n.id));return p;}),[setParams]);
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
  const header = (
    <PageHeader
      title="VPS 与节点"
      description="每个节点都会进入用户的订阅。VPS 用 SSH 一次装好，之后建节点、改配置都在这里完成。"
      actions={
        <>
          <Button variant="outline" disabled={!gates.data?.some((g) => g.status === "active")} onClick={() => setWizard("any")}><Plus />新建节点</Button>
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

      {edit && <GateEdit gate={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void refresh(); }} />}
      <NodeWizard
        open={wizard !== null}
        gateId={typeof wizard === "number" ? wizard : undefined}
        close={() => setWizard(null)}
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
        <State loading={gates.isPending || nodes.isPending} error={gates.error ?? nodes.error} onRetry={refresh} skeleton={<SkeletonCards count={2} lines={2} />} />
        {dialogs}
      </div>
    );
  return (
    <div className="stack">
      {header}
      <GettingStarted gates={gates.data} nodes={nodes.data} addGate={() => setInvite(true)} addNode={() => setWizard("any")} />
      {gates.data.map(g=><GateCard key={g.id} gate={g} nodes={grouped.get(g.id)??emptyNodes} onDetail={openGate} onEdit={setEdit} onNode={openNode} onAdd={setWizard}/>)}
      {dialogs}
    </div>
  );
}
