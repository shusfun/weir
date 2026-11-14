import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input } from "@weir/ui";
import { State } from "./feedback";
import { api } from "../api";
import type { ExitSpec, NodeItem } from "../lib/core";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { useQuery } from "../lib/query";
import { useFormGuard } from "../lib/dirty";
import type { Preset } from "../lib/core";
import { AdvancedFields } from "./node-wizard";
import { ExitSelector } from "./exit-selector";
import { exitErrorFields } from "./chain-exit";
import type { PendingExit } from "./pending-exits";

export function NodeEdit({node,nodes,close}:{node:NodeItem;nodes:NodeItem[];close:()=>void}) {
  const client = useQueryClient();
  const [name,setName]=useState(node.name), [port,setPort]=useState(String(node.port));
  const [exitId,setExitId]=useState<number|undefined>(node.exit_id);
  const [exit,setExit]=useState<ExitSpec>({kind:"freedom"});
  const [settings,setSettings]=useState<Record<string,string|number|boolean>>({});
  const [target,setTarget]=useState(String(node.settings?.reality_server_names ?? "").split(",")[0]);
  const presets=useQuery({queryKey:["presets",node.gate_id],queryFn:async()=>(await api("/admin/presets?gate_id="+node.gate_id)).items as Preset[]});
  const fields=presets.data?.find(p=>p.id===node.preset)?.fields ?? [];
  const save=useFormAction(async track=>api(`/admin/nodes/${node.id}`,"PATCH",{name:name.trim(),port:Number(port),settings,...(node.security==="reality"?{reality_target:target.trim()}:{}),...(exitId?{exit_id:exitId}:{exit})},track),{
    fields:{invalid_reality_target:"target",reality_probe_failed:"target",invalid_name:"name",invalid_port:"port",port_in_use:"port",exit_not_allowed:"exit_id",...exitErrorFields(exit.kind)},
    onSuccess:(updated:NodeItem | {pending:PendingExit})=>{if (!("pending" in updated)) client.setQueryData<NodeItem[]>(["nodes"],old=>old?.map(n=>n.id===updated.id?updated:n));void client.invalidateQueries({queryKey:["gates"]});void client.invalidateQueries({queryKey:["topology"]});void client.invalidateQueries({queryKey:["pending-exits"]});guard.commit();close();},
  });
  const guard=useFormGuard({name,port,exitId,exit,settings,target},close,save.busy);
  useConsoleContext({object:{type:"node",id:node.id,name:node.name},dialog:{title:"编辑节点"}});
  return <Dialog open onOpenChange={v=>!v&&void guard.close()}><DialogContent size="lg"><DialogTitle>编辑节点</DialogTitle><DialogDescription>协议：{node.protocol}（只读）</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}>
    <Field label="名称" name="name" required><Input required maxLength={100} value={name} onChange={e=>setName(e.target.value)}/></Field>
    <Field label="端口" name="port" required><Input required type="number" min={1} max={65535} value={port} onChange={e=>setPort(e.target.value)}/></Field>
    {node.security==="reality"&&<Field label="伪装目标" name="target" required hint="填写域名，由 VPS 探测后应用。"><Input required value={target} onChange={e=>setTarget(e.target.value)}/></Field>}
    {presets.isError && <State error={presets.error} onRetry={()=>void presets.refetch()}/>}
    <AdvancedFields fields={fields.filter(f=>!f.key.includes("private")&&!f.key.includes("password"))} value={{...Object.fromEntries(Object.entries(node.settings??{}).filter(([,v])=>typeof v==="string"||typeof v==="number"||typeof v==="boolean")) as Record<string,string|number|boolean>,...settings}} onChange={v=>setSettings(Object.fromEntries(Object.entries(v).filter(([k,x])=>x!==node.settings?.[k])))}/>
    <ExitSelector admin gateId={node.gate_id} exitId={exitId} onExitId={setExitId} value={exit} onChange={setExit} sourcePath="/admin/chain-sources" nodes={nodes.filter(n=>n.id!==node.id && n.gate_id!==node.gate_id)} nodeHint="先在另一台 VPS 创建节点"/>
    <DialogFooter error={save.error}><Button type="button" variant="outline" onClick={()=>void guard.close()} disabled={save.busy}>取消</Button><Button type="submit" disabled={presets.isPending||presets.isError} busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter>
  </Form></DialogContent></Dialog>;
}
