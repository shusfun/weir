import {useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {GripVertical} from "lucide-react";
import { ReorderButtons, Field, Notice, SearchInput,Button,Input,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter} from "@migate/ui";
import {api} from "../api";
import {State} from "./feedback";
export interface NodeGroups {groups:{name:string;count:number}[];revision:string}
export const readNodeGroups=()=>api("/admin/nodes/groups") as Promise<NodeGroups>;
export function NodeGroupManager({close,saved}:{close:()=>void;saved:()=>Promise<void>}){
 const cache=useQueryClient(),q=useQuery({queryKey:["node-groups"],queryFn:readNodeGroups,refetchOnWindowFocus:false});
 const [search,S]=useState(""),[edit,ED]=useState<string|null>(null),[name,N]=useState(""),[remove,RM]=useState<string|null>(null),[busy,B]=useState(false),[error,E]=useState(""),[message,M]=useState(""),[drag,D]=useState<string|null>(null);
 async function change(body:Record<string,unknown>){if(!q.data)return;B(true);E("");M("");try{const next=await api("/admin/nodes/groups","POST",{...body,revision:q.data.revision}) as NodeGroups;cache.setQueryData(["node-groups"],next);await saved();ED(null);RM(null);M(body.operation==="rename"?"分组已重命名，节点凭据与引用保持。":body.operation==="dissolve"?"分组已解散，节点移至未分组。":"分组顺序已保存。")}catch(e){E(e instanceof Error?e.message:"分组操作失败，请刷新后重试。")}finally{B(false)}}
 function move(from:string,to:string){if(!q.data||from===to)return;const order=q.data.groups.map(g=>g.name),a=order.indexOf(from),b=order.indexOf(to);if(a<0||b<0)return;order.splice(a,1);order.splice(b,0,from);void change({operation:"order",order})}
 return <Dialog open onOpenChange={v=>!v&&!busy&&close()}><DialogContent size="lg"><DialogTitle>管理节点分组</DialogTitle><DialogDescription>重命名、调整顺序或解散分组。解散会保留节点并移至未分组；不修改订阅组引用或用户授权。</DialogDescription>
 {q.isPending?<State loading/>:q.isError?<State error={q.error} onRetry={()=>void q.refetch()}/>:<div className="stack">
  <SearchInput className="w-full" aria-label="搜索节点分组" placeholder="搜索分组名称" value={search} disabled={busy} onChange={e=>S(e.target.value)}/>
  <p className="muted">共 {q.data.groups.length} 个分组 · 拖动手柄排序，或使用上下按钮；顺序保存后分组筛选同步。</p>
  {message&&<Notice role="status">{message}</Notice>}
  {!q.data.groups.length?<p>暂无分组。导入或编辑节点时填写分组名称即可创建。</p>:<div className="node-groups-list">{q.data.groups.map((g,index)=>({g,index})).filter(({g})=>g.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(({g,index})=><article className="node-group-row" key={g.name} onDragOver={e=>{if(drag&&!busy)e.preventDefault()}} onDrop={e=>{e.preventDefault();if(drag&&!busy)move(drag,g.name);D(null)}}>
   <Button variant="ghost" size="icon-sm" draggable={!busy&&!edit} aria-label={"拖动分组 "+g.name} disabled={busy||!!edit} onDragStart={e=>{D(g.name);e.dataTransfer.setData("text/plain",g.name);e.dataTransfer.effectAllowed="move"}} onDragEnd={()=>D(null)}><GripVertical size={16}/></Button>
   <div className="node-group-identity">{edit===g.name?<form className="stack" onSubmit={e=>{e.preventDefault();void change({operation:"rename",name:g.name,new_name:name.trim()})}}><Field label="新的分组名称" htmlFor="node-group-new-name"><Input id="node-group-new-name" autoFocus maxLength={120} value={name} disabled={busy} onChange={e=>N(e.target.value)}/></Field><div className="actions"><Button size="sm" type="submit" disabled={busy||!name.trim()||name.trim()===g.name}>保存名称</Button><Button type="button" variant="outline" size="sm" disabled={busy} onClick={()=>{ED(null);E("")}}>取消编辑</Button></div></form>:<><strong>{g.name}</strong><small>{g.count} 个节点</small></>}</div>
   {edit!==g.name&&<div className="actions"><ReorderButtons label={"分组 "+g.name} onUp={()=>move(g.name,q.data.groups[index-1].name)} onDown={()=>move(g.name,q.data.groups[index+1].name)} upDisabled={busy||!!edit||index===0} downDisabled={busy||!!edit||index===q.data.groups.length-1} /><Button variant="outline" size="sm" disabled={busy||!!edit} onClick={()=>{ED(g.name);N(g.name);E("");M("")}}>重命名</Button><Button variant="ghost" size="sm" disabled={busy||!!edit} onClick={()=>{RM(g.name);E("");M("")}}>解散</Button></div>}
  </article>)}</div>}
  {!!search&&!q.data.groups.some(g=>g.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()))&&<p role="status">没有匹配的节点分组。</p>}
 </div>}
 {error&&<Notice tone="bad"><p>{error}</p><Button variant="outline" disabled={busy} onClick={()=>{E("");ED(null);RM(null);void q.refetch()}}>重新读取分组</Button></Notice>}
 <DialogFooter><Button variant="outline" disabled={busy} onClick={close}>完成</Button></DialogFooter>
 <Dialog open={!!remove} onOpenChange={v=>!v&&!busy&&RM(null)}><DialogContent><DialogTitle>解散节点分组</DialogTitle><DialogDescription>解散“{remove}”后，全部节点移至未分组。节点、凭据和订阅组引用均保留。</DialogDescription>{error&&<Notice tone="bad">{error}</Notice>}<DialogFooter><Button variant="outline" disabled={busy} onClick={()=>RM(null)}>取消</Button><Button disabled={busy} onClick={()=>void change({operation:"dissolve",name:remove})}>{busy?"正在解散…":"确认解散分组"}</Button></DialogFooter></DialogContent></Dialog>
 </DialogContent></Dialog>;
}
