import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, DialogContent, DialogDescription, DialogTitle, useConfirm } from "@weir/ui";
import { Sparkles } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { nodeStatus, type Gate, type NodeItem } from "../lib/core";
import { openDsh } from "./dsh";
import { nodeTitle } from "./node-summary";
import { NodeDetailPage } from "./node-detail-page";
import { NodeEdit } from "./node-edit";

// NodeDetail：一个节点的全部细节与操作：能不能用、要放行的端口、出口、启用、实测、AI 调整、删除。
export function NodeDetail({ node: n, gate: g, nodes, close, changed, layout = "dialog" }: { node: NodeItem; gate?: Gate; nodes: NodeItem[]; close: () => void; changed: () => void; layout?: "dialog" | "page" }) {
  const confirm = useConfirm();
  const { run, isBusy } = useRowAction<string>();
  const [, , why] = nodeStatus(n, g);
  useConsoleContext({ object: { type: "node", name: n.name, id: n.id }, dialog: { title: "节点详情" } });
  const client = useQueryClient();
  const [editing, setEditing] = useState(false);
  const patch = async (enabled: boolean) => { const updated = await api(`/admin/nodes/${n.id}`, "PATCH", {enabled}) as NodeItem; client.setQueryData<NodeItem[]>(["nodes"],old=>old?.map(x=>x.id===n.id?updated:x)); void client.invalidateQueries({queryKey:["gates"]}); };
  const remove = async () => { await api(`/admin/nodes/${n.id}`, "DELETE"); client.setQueryData<NodeItem[]>(["nodes"],old=>old?.filter(x=>x.id!==n.id)); void client.invalidateQueries({queryKey:["gates"]}); };
  const content = <NodeDetailPage node={n} gate={g} busy={isBusy()} tabKey={layout === "page" ? "tab" : "detail_tab"} onEdit={() => setEditing(true)}
    onToggle={(v) => void run("enable", () => patch(v), v ? "节点已启用" : "节点已停用")}
    onDelete={async () => { if (await confirm({ title: `删除节点「${n.name}」？`, description: "使用它的订阅会立即失效。", confirmText: "删除", tone: "danger" }) && await run("delete", remove, "节点已删除")) close(); }} />;
  return <>
    {editing && <NodeEdit node={n} nodes={nodes} close={() => setEditing(false)} />}
    {layout === "page" ? content : <Dialog open onOpenChange={o => !o && close()}><DialogContent size="xl">
      <DialogTitle>{nodeTitle(n)}</DialogTitle>
      <DialogDescription>{why}</DialogDescription>
      {content}
      <Button variant="outline" onClick={() => { close(); openDsh({ notice: `dsh 已知道你在看节点「${n.name}」，直接说要怎么调整即可。`, nodeId: n.id, nodeName: n.name }); }}><Sparkles />AI 调整</Button>
    </DialogContent></Dialog>}
  </>;
}
