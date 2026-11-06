import { NodeSummary, ExitSummary, nodeTitle } from "./node-summary";
// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import {
  Badge, Button, Card, CardContent, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input,
  Notice, PageHeader, Readout, SkeletonCards, StatusLamp, SwitchField, useToast, useConfirm, type LampTone,
} from "@weir/ui";
import { Pencil, Plus, Route, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh } from "./dsh";
import { ObjectLink, dataObject, useDialogParam } from "./object-link";
import { exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { NodeTestControl } from "./node-test";

import { GateDetail, pct, size } from "./gate-detail";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "./wait";
import { GateUpdateLine, GateVersion } from "./gate-version";
import { State } from "./feedback";
import { InviteGate } from "./invite-gate";
import { CertManager } from "./cert-manager";
import { NodeWizard } from "./node-wizard";
import { date } from "../lib/models";
import { rateText } from "../lib/linkmap";
import { field, useConsoleContext } from "../lib/console-context";
import { nodeStatus, protocolNames, relativeTime, type Gate, type NodeItem } from "../lib/core";
import { GettingStarted } from "./getting-started";


import { useQueryClient } from "@tanstack/react-query";
import { NodeEdit } from "./node-edit";
import { protocolLabel } from "./gate-card";
// NodeDetail：一个节点的全部细节与操作：能不能用、要放行的端口、出口、启用、实测、AI 调整、删除。
export function NodeDetail({ node: n, gate: g, nodes, close, changed }: { node: NodeItem; gate?: Gate; nodes: NodeItem[]; close: () => void; changed: () => void }) {
  const confirm = useConfirm();
  const toast = useToast();
  const { run, isBusy, waitFor } = useRowAction<string>();
  const [s, tone, why] = nodeStatus(n, g);
  useConsoleContext({ object: { type: "node", name: n.name, id: n.id }, dialog: { title: "节点详情" } });
  const exit = n.exit.remark || n.exit.label;
  const client = useQueryClient();
  const [editing, setEditing] = useState(false);
  const patch = async (enabled: boolean) => { const updated = await api(`/admin/nodes/${n.id}`, "PATCH", {enabled}) as NodeItem; client.setQueryData<NodeItem[]>(["nodes"],old=>old?.map(x=>x.id===n.id?updated:x)); void client.invalidateQueries({queryKey:["gates"]}); };
  const remove = async () => { await api(`/admin/nodes/${n.id}`, "DELETE"); client.setQueryData<NodeItem[]>(["nodes"],old=>old?.filter(x=>x.id!==n.id)); void client.invalidateQueries({queryKey:["gates"]}); };
  return (
    <>
    {editing && <NodeEdit node={n} nodes={nodes} close={()=>setEditing(false)}/>}
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="md">
        <DialogTitle>{nodeTitle(n)}</DialogTitle>
        <DialogDescription><NodeSummary node={n} /></DialogDescription>
        <div className="stack">
          <p className="flex items-center gap-2 text-body"><StatusLamp tone={tone} label={s} /><strong>{s}</strong><span className="text-[var(--weir-ink-3)]">{why}</span></p>
          {n.readiness?.state === "credentials" && <Notice tone="warn">请到 <Link to="/users" onClick={close}>用户</Link> 分配这个节点并创建可用订阅；节点会在凭据生效后启动。</Notice>}
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Readout label="端口" value={String(n.port)} />
            <Readout label="内核" value={n.kernel || "未知"} />
            <Readout label="凭据" value={String(n.credentials)} caption="每条链接一份" />
            <Readout label="下载速率" value={n.rate?.minutes ? rateText(n.rate.p90_bps) : "未知"} caption={n.rate?.minutes ? `近 24 小时 ${n.rate.minutes} 分钟有流量` : "近 24 小时没有流量"} />
          </div>
          <NodeAccess ports={n.ports} reach={n.reach} />
          <NodeTestControl node={n} gate={g} detail />
          <div className="text-body">
            <span className="text-[var(--weir-ink-3)]">出口：</span><ExitSummary exit={n.exit} expanded />
            {n.exit.kind !== "freedom" && <small className="block text-[var(--weir-ink-3)]">{probeText(n.exit.probe)}</small>}
          </div>
          <SwitchField label="启用" description="停用后这个节点不在任何订阅里，VPS 上也不再监听。" checked={n.enabled} disabled={isBusy("enable")}
            onCheckedChange={(v) => void run("enable", () => patch(v), v ? "节点已启用" : "节点已停用")} />
          {g && !g.online && <Notice tone="warn">这台 VPS 离线，实测要等它重新连上。</Notice>}
        </div>
        <DialogFooter status={(waitFor("test") || waitFor("delete")) && <WaitText wait={waitFor("test") || waitFor("delete")} />}>
          <Button variant="destructive" disabled={isBusy()} busy={isBusy("delete")} busyText="正在删除…" onClick={async () => (await confirm({ title: `删除节点「${n.name}」？`, description: "使用它的订阅会立即失效。", confirmText: "删除", tone: "danger" })) && (await run("delete", remove, "节点已删除")) && close()}>
            <Trash2 />删除
          </Button>
          <Button variant="outline" onClick={()=>setEditing(true)}><Pencil />编辑</Button>
          <Button variant="outline" onClick={() => { close(); openDsh({ notice: `dsh 已知道你在看节点「${n.name}」，直接说要怎么调整即可。`, nodeId: n.id, nodeName: n.name }); }}><Sparkles />AI 调整</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    </>
  );
}
