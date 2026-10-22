import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { SearchInput, PageHeader, Pager, Badge,Button,Card,CardContent,Select,SelectTrigger,SelectValue,SelectContent,SelectItem,Dialog,DialogContent,DialogTitle,DialogDescription } from "@migate/ui";
import { api } from "../api";
import { client } from "../lib/client";
import { date, type Items } from "../lib/models";
import { State } from "../components/feedback";
type Event = {id:number;kind:string;source:string;validity:string;observed_at?:string;created_at:string;data:{category?:string;reason?:string;action?:string;status?:string;severity?:string;device_id?:number;certificate_id?:number;audit_id?:number;not_after?:string;last_seen?:string;count?:number;window_seconds?:number;error?:string;decision?:{category?:string;confidence?:number}}};
const categories:Record<string,string>={device_contact:"设备联系",meter_quality:"计量质量",login_denials:"登录拒绝",certificate_expiry:"证书有效期",unknown:"待分类"};
const reasons:Record<string,string>={heartbeat_missing_over_60_seconds:"超过 60 秒未收到设备心跳",meter_not_fresh:"计量采样缺失或已过期",denied_login_threshold:"登录拒绝次数达到规则阈值",certificate_expired:"证书已到期",certificate_expires_within_14_days:"证书将在 14 天内到期",certificate_not_yet_valid:"证书尚未生效",certificate_metadata_invalid:"证书信息无效或缺失",certificate_removed:"证书已移除",model_unavailable_rules_continue:"模型不可用，基础规则继续运行"};
const states:Record<string,string>={all:"全部状态",observed:"待处理",resolved:"已解除",unknown:"未知",classified:"已分类",unavailable:"模型不可用"};
const severityLabels:Record<string,string>={all:"全部级别",critical:"严重",warning:"警告",info:"信息"};
// 级别 / 处理状态 → Badge 状态色；未列出的值保持中性
const severityTone:Record<string,"bad"|"warn">={critical:"bad",warning:"warn"};
const stateTone:Record<string,"warn"|"ok">={observed:"warn",resolved:"ok"};
export function EventsPage(){
 const [search,S]=useState("");const [status,F]=useState("all");const [severity,V]=useState("all");const [kind,K]=useState("all");const [page,P]=useState(1);const [detail,D]=useState<Event|null>(null);
 const translated=Object.entries(categories).find(([,label])=>search.trim()&&label===search.trim())?.[0]??search;
 const params=new URLSearchParams({page:String(page),page_size:"20",q:translated});if(status!=="all")params.set("status",status);if(severity!=="all")params.set("severity",severity);if(kind!=="all")params.set("kind",kind);
 const events=useQuery({queryKey:["event-directory",params.toString()],queryFn:()=>api("/admin/events?"+params) as Promise<Items<Event>&{queried_at:string}>,refetchInterval:15000});
 const devices=useQuery({queryKey:["devices"],queryFn:client.devices});
 const name=(id?:number)=>devices.data?.items.find(d=>d.id===id)?.name??(id?"Gate #"+id:"中心");
 const effective=(e:Event)=>e.validity==="stale"||e.validity==="unknown"?"unknown":e.data.status??"unknown";
 const current=events.data?.items.find(e=>e.id===detail?.id)??detail;
 const select=(label:string,value:string,options:Record<string,string>,set:(v:string)=>void)=><Select value={value} onValueChange={v=>{set(String(v));P(1)}}><SelectTrigger aria-label={label}><SelectValue>{options[value]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(options).map(([key,title])=><SelectItem key={key} value={key}>{title}</SelectItem>)}</SelectContent></Select>;
 return <div className="stack event-directory"><PageHeader title="安全观测与复核" description="规则独立于模型运行。缺失、过期数据标为未知；分类结果用于复核，不自动封禁或下发指令。" actions={<Button variant="outline" disabled={events.isFetching} onClick={()=>events.refetch()}>刷新事件</Button>} />
 <Card><CardContent><div className="table-toolbar"><SearchInput aria-label="搜索安全事件" placeholder="搜索设备联系、计量质量、规则或事件原因…" value={search} onChange={e=>{S(e.target.value);P(1)}}/>{select("筛选事件来源",kind,{all:"全部来源","security-event":"基础规则","model-event":"Laya 分类"},K)}{select("筛选事件状态",status,states,F)}{select("筛选事件级别",severity,severityLabels,V)}</div>
 <p className="event-query-time">查询时间：{date(events.data?.queried_at)} · 自动刷新 15 秒。筛选按记录状态；列表另外标记观测是否过期。</p>
 {events.isPending?<State loading/>:events.error?<State error={events.error} onRetry={()=>events.refetch()}/>:!events.data?.items.length?<State empty="没有符合条件的安全事件"/>:events.data.items.map(e=><div className="list-row event-row" key={e.id}><div><strong>{categories[e.data.category??""]??e.data.category??e.data.action??"安全事件"}</strong><small>{reasons[e.data.reason??""]??e.data.reason??e.data.action??"等待复核"}</small><small>{name(e.data.device_id)} · {date(e.observed_at)} · {e.source==="remote.laya"?"远程模型快照":"中心规则"}</small></div><div className="actions"><Badge variant="soft" tone={severityTone[e.data.severity??""]??"neutral"}>{severityLabels[e.data.severity??""]??"级别未知"}</Badge><Badge variant="soft" tone={stateTone[effective(e)]??"neutral"}>{states[effective(e)]??effective(e)}{e.validity==="stale"?" · 观测过期":""}</Badge><Button size="sm" variant="outline" onClick={()=>D(e)}>查看事件 #{e.id}</Button></div></div>)}
 <Pager variant="inline" page={page} pageSize={20} total={events.data?.total} busy={events.isFetching} onPage={P} /></CardContent></Card>
 <Dialog open={!!detail} onOpenChange={v=>!v&&D(null)}><DialogContent><DialogTitle>安全事件 #{current?.id}</DialogTitle><DialogDescription>观测和模型分类均为证据；此页面不会自动变更账户或设备。</DialogDescription>{current&&<div className="stack"><div className="detail-grid"><div><span>当前观测</span><strong>{states[effective(current)]??effective(current)}</strong></div><div><span>原记录状态</span><strong>{states[current.data.status??""]??"未知"}</strong></div><div><span>来源</span><strong>{current.source==="remote.laya"?"Laya 远程分类快照":"中心规则（基于实际心跳 / 计量）"}</strong></div><div><span>有效性</span><strong>{{fresh:"60 秒内观测",stale:"已过期，不能据此判断正常",unknown:"观测信息缺失",snapshot:"历史分类快照"}[current.validity]??"未知"}</strong></div><div><span>观测时间</span><strong>{date(current.observed_at)}</strong></div><div><span>首次记录</span><strong>{date(current.created_at)}</strong></div><div><span>设备</span><strong>{current.data.device_id?<Link to={"/devices/"+current.data.device_id}>{name(current.data.device_id)}</Link>:"中心"}</strong></div>{current.data.not_after&&<div><span>证书到期</span><strong>{date(current.data.not_after)}</strong></div>}{current.data.count!==undefined&&<div><span>实际计数 / 时间窗</span><strong>{current.data.count} 次 / {current.data.window_seconds??"未知"} 秒</strong></div>}</div><p>{reasons[current.data.reason??""]??current.data.reason??"暂无事件原因"}</p><p>模型分类：{current.data.decision?.category??"未知 / 未配置"}；置信度：{typeof current.data.decision?.confidence==="number"?Math.round(current.data.decision.confidence*100)+"%":"未知"}</p>{current.data.error&&<p role="status">{reasons[current.data.error]??"模型暂不可用，基础规则继续运行"}</p>}</div>}</DialogContent></Dialog>
 </div>;
}
