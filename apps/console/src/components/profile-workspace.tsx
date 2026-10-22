import {useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {GripVertical,Download} from "lucide-react";
import { ReorderButtons, CopyButton, Field, Notice,Button,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter,Textarea,Select,SelectTrigger,SelectValue,SelectContent,SelectItem} from "@migate/ui";
import {api} from "../api";
import {State} from "./feedback";
import type {Resource} from "../lib/models";

interface Order {items:{id:number;name:string;position:number}[];revision:string}
export function ProfileOrdering({close,saved}:{close:()=>void;saved:()=>Promise<void>}){
 const q=useQuery({queryKey:["profile-order"],queryFn:()=>api("/admin/profiles/order") as Promise<Order>,staleTime:0,gcTime:0}),cache=useQueryClient();
 const [draft,D]=useState<number[]|null>(null),[drag,DR]=useState<number|null>(null),[busy,B]=useState(false),[error,E]=useState("");
 const ids=draft??q.data?.items.map(p=>p.id)??[];
 function move(from:number,to:number){const next=[...ids],a=next.indexOf(from),b=next.indexOf(to);if(a<0||b<0||a===b)return;next.splice(a,1);next.splice(b,0,from);D(next);E("")}
 async function save(){if(!q.data||!draft)return;B(true);E("");try{await api("/admin/profiles/order","POST",{ids:draft,revision:q.data.revision});await cache.invalidateQueries({queryKey:["selection","profiles"]});await saved();close()}catch(e){E(e instanceof Error?e.message:"保存失败，请核对后重试。")}finally{B(false)}}
 return <Dialog open onOpenChange={v=>!v&&!busy&&close()}><DialogContent size="lg"><DialogTitle>调整订阅组顺序</DialogTitle><DialogDescription>拖动手柄或使用上下按钮；保存前只调整本次草稿，取消保留原顺序。不会改变组内节点、处理链或用户授权。</DialogDescription>
 {q.isPending?<State loading/>:q.error?<State error={q.error} onRetry={()=>void q.refetch()}/>:!ids.length?<p>暂无订阅组，请先创建。</p>:<div className="profile-order-list">{ids.map((id,i)=>{const p=q.data.items.find(p=>p.id===id);return <div className="profile-order-row" key={id} onDragOver={e=>{if(drag!==null&&!busy)e.preventDefault()}} onDrop={e=>{e.preventDefault();if(drag!==null&&!busy)move(drag,id);DR(null)}}><Button variant="ghost" size="icon-sm" draggable={!busy} disabled={busy} aria-label={"拖动订阅组 "+p?.name} onDragStart={e=>{DR(id);e.dataTransfer.setData("text/plain",String(id))}} onDragEnd={()=>DR(null)}><GripVertical size={16}/></Button><strong>{p?.name??"未知订阅组"}</strong><div className="actions"><ReorderButtons label={"订阅组 "+p?.name} onUp={()=>move(id,ids[i-1])} onDown={()=>move(id,ids[i+1])} upDisabled={busy||i===0} downDisabled={busy||i===ids.length-1} /></div></div>})}</div>}
 {error&&<Notice tone="bad"><p>{error}</p><Button variant="outline" disabled={busy} onClick={()=>{D(null);E("");void q.refetch()}}>重新读取订阅组</Button></Notice>}
 <DialogFooter><Button variant="outline" disabled={busy} onClick={close}>取消排序</Button><Button disabled={busy||!draft||!!q.error||q.isPending} onClick={()=>void save()}>{busy?"正在保存…":"保存订阅组顺序"}</Button></DialogFooter></DialogContent></Dialog>
}

interface Output {output:string;node_count:number;warnings?:string[];upstream_usage?:{included:number;unknown_sources:number;excluded:number}}
const formats:Record<string,{name:string;ext:string}>={links:{name:"节点链接",ext:"txt"},base64:{name:"Base64",ext:"txt"},clash:{name:"Clash / Mihomo",ext:"yaml"},singbox:{name:"sing-box",ext:"json"},surge:{name:"Surge",ext:"conf"},loon:{name:"Loon",ext:"conf"},quanx:{name:"Quantumult X",ext:"conf"},egern:{name:"Egern",ext:"yaml"}};
export function ProfileOutput({profile,close}:{profile:Resource;close:()=>void}){
 const [target,T]=useState("links"),[notice,N]=useState(""),[error,E]=useState("");
 const q=useQuery({queryKey:["profile-output",profile.id,target],queryFn:()=>api("/admin/profiles/preview","POST",{id:profile.id,target}) as Promise<Output>,staleTime:0,gcTime:0});
 function download(){if(!q.data)return;N("");E("");const url=URL.createObjectURL(new Blob([q.data.output],{type:target==="singbox"?"application/json;charset=utf-8":"text/plain;charset=utf-8"}));const link=document.createElement("a");link.href=url;link.download=`migate-profile-${profile.id}.${formats[target].ext}`;link.click();setTimeout(()=>URL.revokeObjectURL(url),3000);N("已生成当前格式下载文件，请检查浏览器下载记录。");}
 return <Dialog open onOpenChange={v=>!v&&close()}><DialogContent size="xl" className="profile-output-dialog"><DialogTitle>订阅组输出</DialogTitle><DialogDescription>{String(profile.data.name??"订阅组")} · 仅管理员可见。使用实际组内来源、处理链与对应格式模板；导出不会给用户新增授权。</DialogDescription><Field label="输出客户端格式" htmlFor="profile-output-target"><Select value={target} onValueChange={v=>{if(v){T(v);N("");E("")}}}><SelectTrigger id="profile-output-target"><SelectValue>{formats[target].name}</SelectValue></SelectTrigger><SelectContent>{Object.entries(formats).map(([v,f])=><SelectItem key={v} value={v}>{f.name}</SelectItem>)}</SelectContent></Select></Field>
 {q.isPending?<State loading/>:q.error?<State error={q.error} detail={q.error instanceof Error?q.error.message:undefined} onRetry={()=>void q.refetch()}/>:<div className="stack"><p className="muted">实际输出 {q.data.node_count} 个节点 · {formats[target].name}{q.data.upstream_usage&&` · 上游 ${q.data.upstream_usage.included} 个计入 / ${q.data.upstream_usage.excluded} 个排除 / ${q.data.upstream_usage.unknown_sources} 个未知（不作用户额度）`}</p>{q.data.warnings?.length? <Notice role="status">{q.data.warnings.map((w,i)=><p key={i}>{w}</p>)}</Notice>:null}{!q.data.node_count&&<p role="status">当前订阅组没有可输出的节点，请检查库存与处理链。</p>}<Textarea aria-label="订阅组格式输出" mono rows={12} readOnly value={q.data.output}/></div>}
 {notice&&<Notice role="status">{notice}</Notice>}{error&&<Notice tone="bad">{error}</Notice>}
 <DialogFooter><Button variant="outline" onClick={close}>关闭输出</Button><CopyButton variant="outline" disabled={q.isPending||!!q.error||!q.data?.output} value={()=>q.data?.output??""} onCopied={()=>{E("");N("已复制当前格式输出，内容包含节点凭据，请妥善保管。")}} onError={()=>{N("");E("浏览器未允许复制，请选择预览内容手动复制，或下载文件。")}}>复制当前格式</CopyButton><Button disabled={q.isPending||!!q.error||!q.data?.output} onClick={download}><Download size={15}/>下载当前格式</Button></DialogFooter></DialogContent></Dialog>
}
