import { useState } from "react";
import { Link } from "react-router-dom";
import { Badge, Button, Collapsible, Dialog, DialogContent, DialogDescription, DialogTitle, Notice, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import type { Job } from "../../lib/jobs";
import { State } from "../../components/feedback";
import { JobByID } from "../../components/job";
import { SSHInstall } from "../../components/invite-gate";
import { hasUnsavedChanges } from "../../lib/dirty";
import { useConsoleContext } from "../../lib/console-context";

type Hosting = { gate_name:string; online:boolean; colocated:boolean; prepared:boolean; component:{state:string; code?:string; version?:string; model?:string};job?:Job; operation?:{status:string;task_id?:string;code?:string;stage?:string} };
export function GateLayaHosting({gateId,host="",onUse}:{gateId:number;host?:string;onUse?:()=>void}) {
 const q=useQuery({queryKey:["gate-laya",gateId],queryFn:()=>api(`/admin/gates/${gateId}/laya`) as Promise<Hosting>,enabled:gateId>0,refetchInterval:5000});
 const confirm=useConfirm();const [ssh,setSSH]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState("");
 useConsoleContext({ object: {type:"gate", id:gateId, name:q.data?.gate_name || "VPS"}, ...(ssh ? {dialog:{title:"准备 Laya 部署权限"}} : {}) });
 const running=!!q.data && (q.data.job?.status==="running" || ["pending","leased","cancelling"].includes(q.data.operation?.status || ""));
 async function operate(operation:"install"|"remove") {
  setBusy(true);setError("");try {
   const p=await api(`/admin/gates/${gateId}/laya/preview`,"POST",{operation});
   if (!await confirm({title:`${operation==="install"?"安装或重试":"移除"}「${q.data?.gate_name}」的 Laya？`,description:operation==="install"?`使用官方批准版本 ${p.version}，下载固定模型并验证。可以保持零节点；安装不会启用后台分析。`:"移除容器并保留模型数据。",confirmText:"确认执行"}))return;
   await api(`/admin/gates/${gateId}/laya/${p.id}/confirm`,"POST");void q.refetch();
  }catch(e){setError(e instanceof Error?e.message:"操作未完成");}finally{setBusy(false);}
 }
 async function closeSSH() {if (!hasUnsavedChanges() || await confirm({title:"放弃未保存的 SSH 信息？",confirmText:"放弃修改"}))setSSH(false);}
 if (!gateId) return null;
 return <Collapsible title="Laya 组件（允许零节点）">
  {q.isPending||q.error||!q.data?<State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/>:<div className="stack">
   {q.data.colocated ? <Notice>本机执行端随中心运行。<Link className="underline" to="/system?tab=ai">在 AI 设置中部署中心 Laya</Link>。</Notice> : <>
    <div className="flex flex-wrap gap-2"><span>{q.data.gate_name}</span><Badge variant="status" tone={q.data.component.state==="available"?"ok":"neutral"}>{q.data.component.state==="available"?"模型可用":q.data.prepared?"部署权限已准备，运行状态待验证":"尚未准备部署权限"}</Badge></div>
    <small>{!q.data.online?"VPS 离线，当前状态未知":q.data.component.code||"模型版本："+(q.data.component.version||"未知")}</small>
    <div className="actions"><Button variant="outline" disabled={busy||running} onClick={()=>setSSH(true)}>一次性 SSH 准备权限</Button><Button disabled={busy||!q.data.prepared||!q.data.online||running} busy={busy} onClick={()=>void operate("install")}>安装或重试 Laya</Button><Button variant="outline" disabled={busy||!q.data.prepared||!q.data.online||running} onClick={()=>void operate("remove")}>移除容器</Button>{onUse&&<Button variant="ghost" disabled={!q.data.online||q.data.component.state!=="available"} onClick={onUse}>填入推理位置</Button>}</div>
    <Notice>准备 → 安装与模型下载 → 验证。端口只监听回环；移除保留模型卷。安装完成后保存接入配置、验证，再由管理员启用。</Notice>
    {q.data.job && <JobByID key={q.data.job.id} id={q.data.job.id} initial={q.data.job} openLogs onFinish={()=>void q.refetch()}/>}
    {!q.data.job && q.data.operation && <Notice tone={q.data.operation.status==="succeeded"?"ok":["failed","expired"].includes(q.data.operation.status)?"bad":"info"}>{({pending:"等待执行端领取",leased:"安装与验证进行中",cancelling:"正在取消",succeeded:"操作完成",failed:"操作失败，可重试",expired:"操作超时，请核对后重试",cancelled:"已取消",unknown:"结果未知，请刷新核对"} as Record<string,string>)[q.data.operation.status] || "状态未知"} · {q.data.operation.stage||q.data.operation.code||q.data.operation.task_id}</Notice>}
   </>}
  </div>}
  {error&&<Notice tone="bad">{error}</Notice>}
  <Dialog open={ssh} onOpenChange={v=>!v&&void closeSSH()}><DialogContent size="lg"><DialogTitle>准备 {q.data?.gate_name} 的 Laya 部署</DialogTitle><DialogDescription>核对 SSH 主机指纹和既有 Gate 身份，保留设备配置。</DialogDescription>{ssh&&<SSHInstall gateId={gateId} initialHost={host} name={q.data?.gate_name||`VPS ${gateId}`} setName={()=>{}} close={()=>void closeSSH()} done={()=>void q.refetch()}/>}</DialogContent></Dialog>
 </Collapsible>;
}
