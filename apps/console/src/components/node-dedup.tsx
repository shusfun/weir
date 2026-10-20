import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {Button,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter} from "@migate/ui";
import {api} from "../api";
import {State} from "./feedback";
type Plan={snapshot:string;node_count:number;invalid_count:number;items:{keep_id:number;keep_name:string;remove_id:number;remove_name:string}[]};
export function NodeDedup({close,saved}:{close:()=>void;saved:(message:string)=>Promise<void>}){
 const q=useQuery({queryKey:["node-dedup-preview"],queryFn:()=>api("/admin/nodes/dedup","POST",{preview:true}) as Promise<Plan>,staleTime:0,gcTime:0});
 const [busy,B]=useState(false),[error,E]=useState("");
 return <Dialog open onOpenChange={v=>!v&&!busy&&close()}><DialogContent className="business-dialog"><DialogTitle>清理全库存重复节点</DialogTitle><DialogDescription>比较完整连接参数，名称不参与身份判断。优先保留有分组、备注和较完整名称的节点；同步替换订阅组引用，不增加用户授权。此操作不受列表筛选限制。</DialogDescription>{q.isPending?<State loading/>:q.error?<State error={q.error} onRetry={()=>q.refetch()}/>:<div className="stack"><p>库存 {q.data.node_count} 项 · 可清理 {q.data.items.length} 项 · 解析失败 {q.data.invalid_count} 项保留</p>{!q.data.items.length?<p>没有可清理的重复节点。</p>:<div className="dedup-plan">{q.data.items.map(item=><div className="list-row" key={item.remove_id}><div><strong>保留 {item.keep_name||"未命名"} <small>#{item.keep_id}</small></strong><small>删除 {item.remove_name||"未命名"} · #{item.remove_id}</small></div></div>)}</div>}</div>}{error&&<p role="alert">{error}</p>}<DialogFooter><Button variant="outline" disabled={busy} onClick={close}>取消</Button>{error&&<Button variant="outline" disabled={busy} onClick={()=>{E("");void q.refetch()}}>重新预览</Button>}<Button disabled={busy||q.isFetching||!q.data?.items.length||!!error} onClick={async()=>{if(!q.data)return;B(true);E("");try{const result=await api("/admin/nodes/dedup","POST",{snapshot:q.data.snapshot});await saved(`已删除 ${result.removed} 个重复节点，更新 ${result.profiles_updated} 个订阅组引用`);close()}catch(e){E(e instanceof Error?e.message:"清理失败，请重新预览")}finally{B(false)}}}>{busy?"清理中…":"确认清理重复节点"}</Button></DialogFooter></DialogContent></Dialog>
}
