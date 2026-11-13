import { useState } from "react";
import { Link } from "react-router-dom";
import { Badge, Button, Notice, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { State } from "./feedback";
import { useConsoleContext } from "../lib/console-context";
import { HostOperation, type RootOperation } from "./host-operation";

type LocalGate = { state: string; reason: string; gate_id: number; can_enable: boolean; operation?: RootOperation };
const labels: Record<string,string> = { enabled:"已启用", disabled:"未启用", pending:"等待本机执行端上线", conflict:"身份冲突", unknown:"未知", unavailable:"此环境不支持" };

export function LocalGateControl() {
 const q=useQuery({queryKey:["local-gate"],queryFn:()=>api("/admin/center/local-gate") as Promise<LocalGate>,refetchInterval:5000});
 const confirm=useConfirm();const [busy,setBusy]=useState(false);const [error,setError]=useState("");
 useConsoleContext({ object: { type: "gate", id: q.data?.gate_id || undefined, name: "中心本机执行端" } });
 async function enable() {
  setBusy(true);setError("");
  try {
   const preview=await api("/admin/center/local-gate/preview","POST");
   if (!preview.id) {void q.refetch();return;}
   if (!await confirm({title:"启用中心本机节点能力？",description:"中心将短暂重启，扩展为「中心＋本机执行端」，仍可管理远程 VPS。中心配置、订阅令牌和已有设备身份会保留。",confirmText:"确认启用"})) return;
   await api(`/admin/center/local-gate/${preview.id}/confirm`,"POST");void q.refetch();
  }catch(e){setError(e instanceof Error?e.message:"启用未完成");}finally{setBusy(false);}
 }
 const d=q.data;
 if (!d) return <State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/>;
 const running=d.operation && !["done","failed"].includes(d.operation.state);
 return <div className="stack">
  {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/> : <>
   <div className="flex flex-wrap items-center gap-2"><span>{d.state==="enabled"?"中心＋本机执行端":"中心本机执行端"}</span><Badge variant="status" tone={d.state==="enabled"?"ok":"neutral"}>{labels[d.state]??"未知"}</Badge></div>
   <small>中心仍可统一管理远程 VPS。</small>
   {d.reason && <small>{d.reason}</small>}
   <div className="actions">
    {d.can_enable && <Button size="sm" variant="outline" disabled={busy || !!running} busy={busy} onClick={()=>void enable()}>{d.state==="pending"?"检查并恢复本机执行端":"启用本机节点能力"}</Button>}
    {d.gate_id>0 && <><Button size="sm" variant="outline" render={<Link to={`/gates/${d.gate_id}`}/>}>本机 VPS 与组件</Button><Button size="sm" disabled={d.state!=="enabled"} render={<Link to={`/gates?new=${d.gate_id}`}/>}>创建本机节点</Button></>}
    <Button size="sm" variant="ghost" render={<Link to="/system?tab=ai"/>}>部署 Laya</Button>
   </div>
   {d.operation && <HostOperation operation={d.operation}/>}
  </>}
  {error && <Notice tone="bad">{error}</Notice>}
 </div>;
}
