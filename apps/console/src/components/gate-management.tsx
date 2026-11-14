import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ArrowUpRight, Pencil, Plus, Trash2 } from "lucide-react";
import { Button, GlassFolder, Notice, SectionHeader, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { nodeStatus, protocolNames, type Gate, type NodeItem } from "../lib/core";
import { useConsoleContext } from "../lib/console-context";
import { NodeEdit } from "./node-edit";
import { NodeWizard } from "./node-wizard";
import { GateEdit } from "./gate-edit";
import { ActionsWait } from "./wait";

// 列表展开与详情共用操作，新增始终预选当前 VPS。
export function GateManagement({ gate, nodes, details = false }: { gate: Gate; nodes: NodeItem[]; details?: boolean }) {
  const [wizard, setWizard] = useState(false);
  const [editing, setEditing] = useState<NodeItem | null>(null);
  const [editGate, setEditGate] = useState(false);
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
    <SectionHeader title="节点" actions={<>
      {details && <Button size="icon" variant="ghost" title="VPS 详情" aria-label={`${gate.name} 详情`} render={<Link to={`/gates/${gate.id}`} />}><ArrowUpRight /></Button>}
      <Button size="icon" variant="ghost" title="编辑 VPS" aria-label={`编辑 ${gate.name}`} disabled={gate.status !== "active"} onClick={() => setEditGate(true)}><Pencil /></Button>
      <Button size="icon" title="新增节点" aria-label={`在 ${gate.name} 新增节点`} disabled={gate.status !== "active"} onClick={() => setWizard(true)}><Plus /></Button>
    </>} />
    {!gate.online && <Notice tone="warn">VPS 离线。配置可以保存，待重新连接后应用；实测暂不可用。</Notice>}
    {mine.length === 0 ? <small>这台 VPS 还没有节点，点击加号创建。</small> : <div className="weir-node-folders">
      {mine.map(n => <div className="stack" key={n.id}>
        <GlassFolder name={n.name} files={[]} observations={false} caption={`${protocolNames[n.protocol] ?? n.protocol} · ${n.port}`} status={{ tone: nodeStatus(n, gate)[1] === "bad" ? "bad" : nodeStatus(n, gate)[1] === "warn" ? "warn" : nodeStatus(n, gate)[1] === "ok" ? "ok" : "neutral", text: nodeStatus(n, gate)[0] }} onClick={() => setEditing(n)} className="weir-folder-compact" />
        <div className="actions">
          <Button size="icon-sm" variant="ghost" title="节点详情" aria-label={`${n.name} 详情`} render={<Link to={`/nodes/${n.id}`} />}><ArrowUpRight /></Button>
          <Button size="icon-sm" variant="ghost" title="编辑节点" aria-label={`编辑 ${n.name}`} disabled={isBusy()} onClick={() => setEditing(n)}><Pencil /></Button>
          <Button size="icon-sm" variant="ghost" title="删除节点" aria-label={`删除 ${n.name}`} disabled={isBusy()} busy={isBusy(n.id)} onClick={() => void remove(n)}><Trash2 /></Button>
          <ActionsWait wait={waitFor(n.id)} />
        </div>
      </div>)}
    </div>}
    {editing && <NodeEdit node={editing} nodes={nodes} close={() => setEditing(null)} />}
    {editGate && <GateEdit gate={gate} close={() => setEditGate(false)} saved={() => { setEditGate(false); void refresh(); }} />}
    <NodeWizard open={wizard} gateId={gate.id} gates={[gate]} nodes={nodes} close={() => setWizard(false)} done={() => void refresh()} />
  </div>;
}
