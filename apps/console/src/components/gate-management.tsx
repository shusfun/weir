import { GateBrowserTest } from "./browser-test";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ArrowUpRight, Pencil, Plus, Trash2 } from "lucide-react";
import { ActionGroup, Badge, Button, Chip, Notice, SectionHeader, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { nodeStatus, protocolNames, type Gate, type NodeItem } from "../lib/core";
import { useConsoleContext } from "../lib/console-context";
import { NodeEdit } from "./node-edit";
import { NodeWizard } from "./node-wizard";
import { NodeDetail } from "./node-detail";
import { ObjectLink, useDialogParam } from "./object-link";
import { nodeChainRoute } from "./node-summary";
import { GateTraffic } from "./gate-traffic";
import { GateEdit } from "./gate-edit";
import { GateLifecycle } from "./gate-delete";
import { ActionsWait } from "./wait";

// 列表展开与详情共用操作，新增始终预选当前 VPS。compact 模式用于文件夹原位展开，把低频组件收进详情对应标签页。
export function GateManagement({ gate, nodes, details = false, compact = false }: { gate: Gate; nodes: NodeItem[]; details?: boolean; compact?: boolean }) {
  const [wizard, setWizard] = useState(false);
  const [editing, setEditing] = useState<NodeItem | null>(null);
  const [editGate, setEditGate] = useState(false);
  const detail = useDialogParam("node", nodes, true);
  const confirm = useConfirm();
  const client = useQueryClient();
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ["nodes"] }), client.invalidateQueries({ queryKey: ["gates"] }), client.invalidateQueries({ queryKey: ["topology", gate.id] })]);
  const { run, isBusy, waitFor } = useRowAction<number>({ refetch: refresh });
  const mine = nodes.filter(n => n.gate_id === gate.id);
  useConsoleContext({ object: { type: "gate", id: gate.id, name: gate.name } });
  const remove = async (n: NodeItem) => {
    if (await confirm({ title: `删除节点「${n.name}」？`, description: `使用此节点的 ${n.credentials} 份凭据将失效，订阅将移除此节点；其他节点不受影响。被用作出口的节点需先解除引用。`, confirmText: "删除节点", tone: "danger" })) {
      await run(n.id, track => api(`/admin/nodes/${n.id}`, "DELETE", undefined, track), "节点已删除");
    }
  };
  return <div className="stack" data-tour="gate-management">
    <SectionHeader
      flush
      title={`节点（${mine.length}）`}
      actions={
        <ActionGroup wrap>
          {!compact && <GateLifecycle gate={gate} />}
          {details && <Button size="sm" variant="outline" render={<Link to={`/gates?gate=${gate.id}`} />}>详情</Button>}
          <Button size="sm" variant="outline" disabled={gate.status !== "active"} onClick={() => setEditGate(true)}><Pencil />编辑 VPS</Button>
          <Button size="sm" disabled={gate.status !== "active"} onClick={() => setWizard(true)}><Plus />新建节点</Button>
        </ActionGroup>
      }
    />
    {!gate.online && <Notice tone="warn">VPS 离线。配置可以保存，待重新连接后应用；实测暂不可用。</Notice>}
    {mine.length === 0 ? <small className="text-[var(--weir-ink-3)]">这台 VPS 还没有节点，点右上角「新建节点」创建。</small> : <div className="divide-y divide-[var(--weir-line-soft)]">
      {mine.map(n => {
        const chain = nodeChainRoute(n, nodes, [gate]);
        return <div className="weir-set-row weir-node-management-row py-3" key={n.id}>
        <div className="weir-node-management-summary space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Chip tone={nodeStatus(n, gate)[1]} onClick={() => detail.open(n.id)}>{n.name}</Chip>
            <small className="font-mono text-[var(--weir-ink-2)]">{protocolNames[n.protocol] ?? n.protocol} · {n.port} · {nodeStatus(n, gate)[0]}</small>
            {chain ? <Badge variant="soft" tone="info">{chain.tag} ⇢ {chain.ref ? <ObjectLink to={chain.ref} /> : chain.target}</Badge> : <small className="text-[var(--weir-ink-3)]">直连</small>}
          </div>
          {nodeStatus(n, gate)[2] && <small className="block text-[var(--weir-ink-3)]">{nodeStatus(n, gate)[2]}</small>}
        </div>
        <ActionGroup wrap={false}>
          <Button size="sm" variant="ghost" onClick={() => detail.open(n.id)}>详情</Button>
          <Button size="icon-sm" variant="ghost" title="编辑节点" aria-label={`编辑 ${n.name}`} disabled={isBusy()} onClick={() => setEditing(n)}><Pencil /></Button>
          <Button size="icon-sm" variant="ghost" title="删除节点" aria-label={`删除 ${n.name}`} disabled={isBusy()} busy={isBusy(n.id)} onClick={() => void remove(n)}><Trash2 /></Button>
        </ActionGroup>
        {waitFor(n.id) && <div className="weir-node-management-wait"><ActionsWait wait={waitFor(n.id)} /></div>}
      </div>;
      })}
    </div>}
    {(!compact || details) && <GateTraffic gateID={gate.id} />}
    {!compact && <GateBrowserTest gateId={gate.id} />}
    {details && compact && (
      <ActionGroup>
        <Button size="sm" variant="outline" render={<Link to={`/gates?gate=${gate.id}&detail_tab=health`} />}>主机体检</Button>
        <Button size="sm" variant="outline" render={<Link to={`/gates?gate=${gate.id}&detail_tab=laya`} />}>Laya 推理托管</Button>
      </ActionGroup>
    )}
    {detail.current && <NodeDetail node={detail.current} gate={gate} nodes={nodes} close={detail.close} changed={() => void refresh()} />}
    {editing && <NodeEdit node={editing} nodes={nodes} close={() => setEditing(null)} />}
    {editGate && <GateEdit gate={gate} close={() => setEditGate(false)} saved={() => { setEditGate(false); void refresh(); }} />}
    <NodeWizard open={wizard} gateId={gate.id} gates={[gate]} nodes={nodes} close={() => setWizard(false)} done={() => void refresh()} />
  </div>;
}
