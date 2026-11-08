import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input } from "@weir/ui";
import { api } from "../api";
import type { ExitSpec, NodeItem } from "../lib/core";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { ExitSelector } from "./exit-selector";
import { exitErrorFields } from "./chain-exit";
import type { PendingExit } from "./pending-exits";

export function NodeEdit({node,nodes,close}:{node:NodeItem;nodes:NodeItem[];close:()=>void}) {
  const client = useQueryClient();
  const [name,setName]=useState(node.name), [port,setPort]=useState(String(node.port));
  const [exitId,setExitId]=useState<number|undefined>(node.exit_id);
  const [exit,setExit]=useState<ExitSpec>({kind:"freedom"});
  const save=useFormAction(async track=>api(`/admin/nodes/${node.id}`,"PATCH",{name:name.trim(),port:Number(port),...(exitId?{exit_id:exitId}:{exit})},track),{
    fields:{invalid_name:"name",invalid_port:"port",port_in_use:"port",exit_not_allowed:"exit_id",...exitErrorFields(exit.kind)},
    onSuccess:(updated:NodeItem | {pending:PendingExit})=>{if (!("pending" in updated)) client.setQueryData<NodeItem[]>(["nodes"],old=>old?.map(n=>n.id===updated.id?updated:n));void client.invalidateQueries({queryKey:["gates"]});void client.invalidateQueries({queryKey:["pending-exits"]});close();},
  });
  useConsoleContext({object:{type:"node",id:node.id,name:node.name},dialog:{title:"编辑节点"}});
  return <Dialog open onOpenChange={v=>!v&&!save.busy&&close()}><DialogContent size="lg"><DialogTitle>编辑节点</DialogTitle><DialogDescription>协议：{node.protocol}（只读）</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}>
    <Field label="名称" name="name" required><Input required maxLength={100} value={name} onChange={e=>setName(e.target.value)}/></Field>
    <Field label="端口" name="port" required><Input required type="number" min={1} max={65535} value={port} onChange={e=>setPort(e.target.value)}/></Field>
    <ExitSelector admin gateId={node.gate_id} exitId={exitId} onExitId={setExitId} value={exit} onChange={setExit} sourcePath="/admin/chain-sources" nodes={nodes.filter(n=>n.id!==node.id && n.gate_id!==node.gate_id)} nodeHint="先在另一台 VPS 创建节点"/>
    <DialogFooter error={save.error}><Button type="button" variant="outline" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter>
  </Form></DialogContent></Dialog>;
}
