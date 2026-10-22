import {useId,useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import { PageState, Field, Notice, Pager, SectionHeader,Badge,Button,Card,CardContent,Input,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter,Segmented} from "@migate/ui";
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
 return <Card><CardContent><SectionHeader title={<>{self?"有效登录会话":"登录会话"}</>} description={<>{self?"核对来源与登录时间，及时撤销不认识的会话。":"仅展示此用户的有效会话，撤销不改变节点授权。"}</>} actions={<div className="actions"><Button variant="outline" disabled={query.isFetching||busy} onClick={()=>void query.refetch()}>刷新会话</Button>{self&&<Button variant="destructive" disabled={busy||query.isPending||!!query.error} onClick={()=>{E("");C("others")}}>撤销其他会话</Button>}</div>} />
 {notice&&<Notice role="status">{notice}</Notice>}
 <div className="session-filters"><div className="stack"><Field label="搜索登录来源" htmlFor={inputId}><Input id={inputId} placeholder="按 IP 搜索" maxLength={100} value={search} onChange={e=>{S(e.target.value);P(1)}}/></Field></div>{self&&<Segmented aria-label="会话范围" value={scope} onValueChange={v=>{SC(v);P(1)}} options={[["all","全部会话"],["current","当前会话"],["other","其他会话"]]}/>}</div>
 {query.isPending?<State loading/>:query.error?<State error={query.error} onRetry={()=>void query.refetch()}/>:<>
 <p className="muted">共 {d?.all_total??0} 个有效会话 · 当前匹配 {d?.total??0} 个；每页 10 个。当前登录优先，其余按登录时间排序。</p>
 {!d?.items.length?<PageState size="sm" kind="empty" title={search||scope!=="all"?"没有匹配的登录会话。":"暂无有效登录会话。"}/>:<div className="session-list">{d.items.map(session=><div className="list-row security-session" key={session.id}><div><strong>{session.ip||"来源未知"}{session.current&&<Badge variant="soft" tone="info" className="ml-2 align-middle">当前会话</Badge>}</strong><small>登录于 {date(session.created_at)} · {session.mfa?"已完成二次验证":"普通登录"}</small><small>约 {Math.ceil(session.expires_in/60)} 分钟后过期</small></div><Button variant="destructive" size="sm" disabled={busy} onClick={()=>{E("");C(session)}}>撤销{session.current?"当前":"此"}会话</Button></div>)}</div>}
 <Pager variant="inline" name="会话" page={d?.page??page} pageSize={d?.page_size??10} total={d?.total} busy={query.isFetching} onPage={P}/>
 </>}
 </CardContent><Dialog open={!!confirm} onOpenChange={v=>!v&&!busy&&C(null)}><DialogContent><DialogTitle>{confirm==="others"?"撤销其他登录会话":"撤销登录会话"}</DialogTitle><DialogDescription>{confirm==="others"?"撤销本次读取时你账户的所有其他会话，保留当前登录。之后新建的登录不在本次范围；其他用户、节点与授权不受影响。":confirm?.current?"这是当前会话。撤销后需要重新登录。":"此会话将立即失效，其他登录与节点授权不受影响。"}</DialogDescription>{error&&<Notice tone="bad">{error}</Notice>}<DialogFooter><Button variant="outline" disabled={busy} onClick={()=>C(null)}>取消</Button><Button variant="destructive" disabled={busy} onClick={()=>void revoke()}>{busy?"正在撤销…":confirm==="others"?"确认撤销其他会话":"确认撤销会话"}</Button></DialogFooter></DialogContent></Dialog></Card>
}
