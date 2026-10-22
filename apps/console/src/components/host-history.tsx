import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {CartesianGrid,Line,LineChart,XAxis,YAxis} from "recharts";
import {ChartContainer,ChartTooltip,ChartTooltipContent} from "@migate/ui/chart";
import { PageState, SectionHeader,Button,Card,CardContent,Select,SelectTrigger,SelectValue,SelectContent,SelectItem,Segmented} from "@migate/ui";
import {api} from "../api";
import {State} from "./feedback";
import {date} from "../lib/models";
interface Sample {at:string;observed_at:string;quality:string;cpu_percent:number|null;cpu_interval_seconds:number|null;memory_percent:number|null;disk_used_percent:number|null;load_1m:number|null}
interface History {items:Sample[];source:string;observed_at:string;retention_days:number}
const windows:Record<string,string>={"60":"最近 1 小时","360":"最近 6 小时","1440":"最近 24 小时"};
const metrics:Record<string,string>={cpu_percent:"CPU 使用率",memory_percent:"内存使用率",disk_used_percent:"数据盘使用率"};
export function HostHistory({deviceId}:{deviceId:number}){
 const [window,W]=useState("60"),[metric,M]=useState("cpu_percent"),[details,D]=useState(false);
 const q=useQuery({queryKey:["host-history",deviceId,window],queryFn:()=>api(`/admin/devices/${deviceId}/host-history?minutes=${window}`) as Promise<History>,refetchInterval:30000});
 const rows=q.data?.items??[],points:{at:number;value:number|null}[]=[];
 for(const [i,p]of rows.entries()){const at=Date.parse(p.at),last=rows[i-1];if(last&&at-Date.parse(last.at)>90000)points.push({at:Date.parse(last.at)+60000,value:null});const value=p[metric as "cpu_percent"|"memory_percent"|"disk_used_percent"];points.push({at,value:p.quality!=="unknown"&&typeof value==="number"&&Number.isFinite(value)?value:null})}
 const known=points.filter(p=>p.value!==null),latest=rows.at(-1),fresh=latest&&Date.now()-Date.parse(latest.at)<=90000;
 return <Card aria-label="主机历史"><CardContent className="stack"><SectionHeader title="主机历史" description="真实系统采样，每分钟记录一次；CPU 为采样间隔平均，内存 / 数据盘为快照。" actions={<div className="actions"><Select value={window} onValueChange={v=>v&&W(v)}><SelectTrigger aria-label="主机历史时间范围"><SelectValue>{windows[window]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(windows).map(([v,n])=><SelectItem key={v} value={v}>{n}</SelectItem>)}</SelectContent></Select><Button variant="outline" disabled={q.isFetching} onClick={()=>void q.refetch()}>刷新主机历史</Button></div>} />
 <Segmented aria-label="主机历史指标" value={metric} onValueChange={M} options={Object.entries(metrics)}/>
 {q.isPending?<State loading/>:q.error?<State error={q.error} onRetry={()=>void q.refetch()}/>:<>
 <p className="muted">来源：Linux procfs / statfs 已接受样本 · {rows.length} 次实际记录，当前指标 {known.length} 次有效 · {fresh?"最近样本 "+date(latest.at):"当前采样未知 / 已过期"}</p>
 {!known.length?<PageState size="sm" kind="empty" title={<>当前范围没有有效的{metrics[metric]}采样。首次 CPU、重启、计数重置和缺失数据均为未知，不补零。</>}/>:<ChartContainer className="h-[250px] w-full" config={{value:{label:metrics[metric],color:"var(--mg-chart-1)"}}}><LineChart data={points}><CartesianGrid vertical={false}/><XAxis dataKey="at" type="number" domain={["dataMin","dataMax"]} tickFormatter={v=>new Date(v).toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"})}/><YAxis domain={[0,100]} tickFormatter={v=>v+"%"}/><ChartTooltip content={<ChartTooltipContent labelFormatter={v=>new Date(Number(v)).toLocaleString()} formatter={value=>typeof value==="number"?value.toFixed(1)+"%":"未知"}/>}/><Line type="linear" dataKey="value" stroke="var(--color-value)" dot={{r:3}} connectNulls={false} isAnimationActive={false}/></LineChart></ChartContainer>}
 <p className="muted">保留 7 天，可查最近 24 小时。停机 / 断连会留下未知或空档，曲线不跨空档连接。历史数据不作用户额度计费。</p>
 <Button variant="outline" onClick={()=>D(!details)}>{details?"收起主机采样明细":"查看主机采样明细"}</Button>{details&&<div className="host-sample-list">{[...rows].reverse().slice(0,60).map(p=><div className="list-row" key={p.at}><div><strong>{date(p.at)}</strong><small>CPU {p.cpu_percent==null?"未知":p.cpu_percent.toFixed(1)+"%"} · 内存 {p.memory_percent==null?"未知":p.memory_percent.toFixed(1)+"%"} · 数据盘 {p.disk_used_percent==null?"未知":p.disk_used_percent.toFixed(1)+"%"}</small><small>源采样 {p.observed_at?date(p.observed_at):"未知"} · {p.quality==="available"?"有效":p.quality==="partial"?"部分未知":"未知"}</small></div></div>)}<p className="muted">明细最多显示最近 60 次实际记录；曲线使用所选完整范围。</p></div>}
 </>}
 </CardContent></Card>
}
