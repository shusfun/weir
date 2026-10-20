import {useId,useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {Button,Card,CardContent,Input,Label,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter} from "@migate/ui";
import {api} from "../api";
import {date} from "../lib/models";
import {State} from "./feedback";
interface Session {id:string;created_at:string;ip:string;current:boolean;mfa:boolean;expires_in:number}
interface Directory {items:Session[];page:number;pages:number;total:number;all_total:number;page_size:number}
export function UserSessions({userId,onCurrentRevoked}:{userId?:number;onCurrentRevoked?:()=>void}){
 const cache=useQueryClient(),inputId=useId(),self=userId===undefined;
 const [page,P]=useState(1),[search,S]=useState(""),[scope,SC]=useState("all"),[confirm,C]=useState<Session|"others"|null>(null),[error,E]=useState(""),[busy,B]=useState(false),[notice,N]=useState("");
 const base=self?"/sessions":`/admin/users/${userId}/sessions`,key=self?["self-sessions"]:["user-sessions",userId];
 const query=useQuery({queryKey:[...key,page,search,scope],queryFn:()=>api(base+"?"+new URLSearchParams({page:String(page),page_size:"10",q:search,scope})) as Promise<Directory>,refetchInterval:30000});
 async function revoke(){if(!confirm)return;B(true);E("");try{
  const others=confirm==="others",current=!others&&confirm.current;
  const result=await api(others?"/sessions/revoke-others":base+"/"+(confirm as Session).id,others?"POST":"DELETE",others?{confirm:true}:undefined);
  C(null);N(others?`已撤销 ${result.revoked} 个其他会话；当前登录保持有效。`:"已撤销此会话，原登录将立即失效。");
  if(current){cache.clear();if(onCurrentRevoked)onCurrentRevoked();else window.location.reload();return}
  P(1);await cache.invalidateQueries({queryKey:key});
 }catch(e){E(e instanceof Error?e.message:"撤销未确认，请刷新核对后重试。")}finally{B(false)}}
 const d=query.data;
 return <Card><CardContent><div className="section-heading"><div><h2>{self?"有效登录会话":"登录会话"}</h2><p>{self?"核对来源与登录时间，及时撤销不认识的会话。":"仅展示此用户的有效会话，撤销不改变节点授权。"}</p></div><div className="actions"><Button variant="outline" disabled={query.isFetching||busy} onClick={()=>void query.refetch()}>刷新会话</Button>{self&&<Button variant="outline" disabled={busy||query.isPending||!!query.error} onClick={()=>{E("");C("others")}}>撤销其他会话</Button>}</div></div>
 {notice&&<p className="notice" role="status">{notice}</p>}
 <div className="session-filters"><div className="stack"><Label htmlFor={inputId}>搜索登录来源</Label><Input id={inputId} placeholder="按 IP 搜索" maxLength={100} value={search} onChange={e=>{S(e.target.value);P(1)}}/></div>{self&&<div className="actions" role="group" aria-label="会话范围">{[["all","全部会话"],["current","当前会话"],["other","其他会话"]].map(([v,label])=><Button key={v} variant={scope===v?"default":"outline"} aria-pressed={scope===v} onClick={()=>{SC(v);P(1)}}>{label}</Button>)}</div>}</div>
 {query.isPending?<State loading/>:query.error?<State error={query.error} onRetry={()=>void query.refetch()}/>:<>
 <p className="muted">共 {d?.all_total??0} 个有效会话 · 当前匹配 {d?.total??0} 个；每页 10 个。当前登录优先，其余按登录时间排序。</p>
 {!d?.items.length?<p className="security-inline-empty" role="status">{search||scope!=="all"?"没有匹配的登录会话。":"暂无有效登录会话。"}</p>:<div className="session-list">{d.items.map(session=><div className="list-row security-session" key={session.id}><div><strong>{session.ip||"来源未知"}{session.current&&<span className="tag">当前会话</span>}</strong><small>登录于 {date(session.created_at)} · {session.mfa?"已完成二次验证":"普通登录"}</small><small>约 {Math.ceil(session.expires_in/60)} 分钟后过期</small></div><Button variant="outline" size="sm" disabled={busy} onClick={()=>{E("");C(session)}}>撤销{session.current?"当前":"此"}会话</Button></div>)}</div>}
 <nav className="session-pagination" aria-label="会话分页"><Button variant="outline" disabled={!d||d.page<=1} onClick={()=>P((d?.page??1)-1)}>上一页会话</Button><span>第 {d?.page??1} / {d?.pages??1} 页</span><Button variant="outline" disabled={!d||d.page>=d.pages} onClick={()=>P((d?.page??1)+1)}>下一页会话</Button></nav>
 </>}
 </CardContent><Dialog open={!!confirm} onOpenChange={v=>!v&&!busy&&C(null)}><DialogContent><DialogTitle>{confirm==="others"?"撤销其他登录会话":"撤销登录会话"}</DialogTitle><DialogDescription>{confirm==="others"?"撤销本次读取时你账户的所有其他会话，保留当前登录。之后新建的登录不在本次范围；其他用户、节点与授权不受影响。":confirm?.current?"这是当前会话。撤销后需要重新登录。":"此会话将立即失效，其他登录与节点授权不受影响。"}</DialogDescription>{error&&<p role="alert">{error}</p>}<DialogFooter><Button variant="outline" disabled={busy} onClick={()=>C(null)}>取消</Button><Button disabled={busy} onClick={()=>void revoke()}>{busy?"正在撤销…":confirm==="others"?"确认撤销其他会话":"确认撤销会话"}</Button></DialogFooter></DialogContent></Dialog></Card>
}
