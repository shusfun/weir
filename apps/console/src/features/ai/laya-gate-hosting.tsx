import { lazy, Suspense } from "react";
const LayaCard = lazy(() => import("../../components/defense").then(module => ({default: module.LayaCard})));
import { Settings2 } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { Badge, Button, Collapsible, Dialog, DialogContent, DialogDescription, DialogTitle, Notice, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import type { Job } from "../../lib/jobs";
import { State } from "../../components/feedback";
import { JobByID } from "../../components/job";
import type { ObjectState } from "../../lib/core";
import { hasUnsavedChanges } from "../../lib/dirty";
import { useConsoleContext } from "../../lib/console-context";

type Hosting = { gate_name:string; online:boolean; colocated:boolean; prepared:boolean;can_prepare:boolean; object_state:ObjectState; component:{state:string; code?:string; version?:string; model?:string};job?:Job; operation?:{status:string;task_id?:string;code?:string;stage?:string} };
export function GateLayaHosting({gateId,onUse}:{gateId:number;onUse?:()=>void}) {
 const q=useQuery({queryKey:["gate-laya",gateId],queryFn:()=>api(`/admin/gates/${gateId}/laya`) as Promise<Hosting>,enabled:gateId>0,refetchInterval:5000});
 const [settings,setSettings]=useState(false);
 const confirm=useConfirm();const [busy,setBusy]=useState(false);const [error,setError]=useState("");
 useConsoleContext({ object: {type:"gate", id:gateId, name:q.data?.gate_name || "VPS"} });
 async function operate(operation:"prepare"|"install"|"remove") {
  setBusy(true);setError("");try {
   const p=await api(`/admin/gates/${gateId}/laya/preview`,"POST",{operation});
   if (!await confirm({title:`${operation==="prepare"?"准备部署权限":operation==="install"?"安装或重试":"移除"}「${q.data?.gate_name}」的 Laya？`,description:operation==="prepare"?"通过已安装的固定宿主服务准备官方 Ollaya 与固定模型批准档案；不开放远程命令，也不启用后台分析。":operation==="install"?`使用官方批准版本 ${p.version}，下载固定模型并验证。可以保持零节点；安装不会启用后台分析。`:"移除容器并保留模型数据。",confirmText:"确认执行"}))return;
   await api(`/admin/gates/${gateId}/laya/${p.id}/confirm`,"POST");void q.refetch();
  }catch(e){setError(e instanceof Error?e.message:"操作未完成");}finally{setBusy(false);}
 }
 if (!gateId) return null;
 const summary=q.isPending?"读取中…":q.error?"读取失败":q.data?.object_state.label ?? "状态未知";
 const allowed=(action:string)=>q.data?.object_state.next.includes(action) ?? false;
 return <Collapsible variant="section" title="Laya 组件" summary={summary}>
  <p className="text-meta text-[var(--weir-ink-3)]">可独立部署 Laya，无须先创建节点。</p>
  {q.isPending||q.error||!q.data?<State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/>:<div className="stack">
   <div className="actions"><Button size="icon" variant="outline" title="接入设置与测试" aria-label="Laya 接入设置与测试" onClick={()=>setSettings(true)}><Settings2 /></Button></div>
   {q.data.colocated ? <Notice>本机执行端随中心运行。<Link className="underline" to="/system?tab=ai">在 AI 设置中部署中心 Laya</Link>。</Notice> : <>
    <div className="flex flex-wrap gap-2"><span>{q.data.gate_name}</span><Badge variant="status" tone={q.data.object_state.status==="ok"?"ok":"neutral"}>{q.data.object_state.label}</Badge></div>
    <small>{q.data.object_state.reason}</small>
    <div className="actions"><Button variant="outline" disabled={busy||!allowed("prepare")} onClick={()=>void operate("prepare")}>准备部署权限</Button><Button disabled={busy||!allowed("install")} busy={busy} onClick={()=>void operate("install")}>安装或重试 Laya</Button><Button variant="outline" disabled={busy||!allowed("remove")} onClick={()=>void operate("remove")}>移除容器</Button>{onUse&&<Button variant="ghost" disabled={!allowed("use")} onClick={onUse}>填入推理位置</Button>}</div>
    <Notice>准备 → 安装与模型下载 → 验证。端口只监听回环；移除保留模型卷。安装完成后保存接入配置、验证，再由管理员启用。</Notice>
    {q.data.job && <JobByID key={q.data.job.id} id={q.data.job.id} initial={q.data.job} openLogs onFinish={()=>void q.refetch()}/>}
    {!q.data.job && q.data.operation && <Notice tone={q.data.operation.status==="succeeded"?"ok":["failed","expired"].includes(q.data.operation.status)?"bad":"info"}>{({pending:"等待执行端领取",leased:"安装与验证进行中",cancelling:"正在取消",succeeded:"操作完成",failed:"操作失败，可重试",expired:"操作超时，请核对后重试",cancelled:"已取消",unknown:"结果未知，请刷新核对"} as Record<string,string>)[q.data.operation.status] || "状态未知"} · {q.data.operation.stage||q.data.operation.code||q.data.operation.task_id}</Notice>}
   </>}
  </div>}
  {error&&<Notice tone="bad">{error}</Notice>}
  <Dialog open={settings} onOpenChange={async v=>{if(v || !hasUnsavedChanges() || await confirm({title:"放弃未保存的 Laya 配置？",confirmText:"放弃修改"}))setSettings(v);}}><DialogContent size="lg"><DialogTitle>Laya 接入设置与测试</DialogTitle><DialogDescription>修改当前推理位置及允许的参数。保存并测试通过后才能启用后台分析。</DialogDescription>{settings&&<Suspense fallback={<State loading />}><LayaCard hosting={false}/></Suspense>}</DialogContent></Dialog>
 </Collapsible>;
}
