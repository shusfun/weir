import { PendingExits } from "../components/pending-exits";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, LaneMap, Notice, PageHeader, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Textarea, useConfirm } from "@weir/ui";
import { api } from "../api";
import type { ExitItem, ExitSpec, Gate, NodeItem } from "../lib/core";
import { exitInput, exitTitle, useExitCatalog } from "../lib/exits";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { ExitFields, exitErrorFields, probeText } from "../components/chain-exit";
import { ObjectLink, dataObject } from "../components/object-link";
import { State } from "../components/feedback";
import { ExitSummary } from "../components/node-summary";
import { WaitText } from "../components/wait";

interface RouteItem {id:number;gate_id:number;exit_id:number;domain:string;ip:string;priority:number;enabled:boolean;node_id?:number;user_id?:number}
export function ExitsPage() {
  const [params,setParams]=useSearchParams(), gid=Number(params.get("gate"))||undefined;
  const exits=useExitCatalog(true,gid);
  const gates=useQuery({queryKey:["gates"],queryFn:async()=>(await api("/admin/gates")).items as Gate[]});
  const visible=(gates.data??[]).filter(g=>!gid||g.id===gid);
  const routeQueries=useQueries({queries:visible.map(g=>({queryKey:["routes",g.id],queryFn:async()=>(await api(`/admin/gates/${g.id}/routes`)).items as RouteItem[]}))});
  const routes=routeQueries.flatMap(q=>q.data??[]);
  const routesKnown=visible.length===0||routeQueries.every(q=>q.isSuccess||q.isError);
  const tcpOnly=(exits.data?.items??[]).filter(e=>e.kind==="http"||e.kind==="https").length;
  const nodes=useQuery({queryKey:["nodes"],queryFn:async()=>(await api("/admin/nodes")).items as NodeItem[]});
  const [edit,setEdit]=useState<ExitItem|"new"|null>(null);
  const confirm=useConfirm();
  const action=useRowAction<number>({invalidate:[["exits"]]});
  useConsoleContext({page:"出口与分流",list:{total:exits.data?.items.length??0,items:(exits.data?.items??[]).slice(0,20).map(e=>({type:"exit",id:e.id,name:exitTitle(e)}))}});
  return <div className="stack"><PageHeader size="data" title="出口与分流" description={<span className="weir-sum"><span><b>{exits.data ? exits.data.items.length : "未知"}</b> 个出口</span><span><b>{routesKnown ? routes.length : "未知"}</b> 条分流规则</span><span>{exits.data ? `${tcpOnly} 个只能走 TCP` : "TCP 限制未知"}</span></span>} actions={<><Select value={gid?String(gid):"all"} onValueChange={v=>setParams(v==="all"?{}:{gate:String(v)})}><SelectTrigger aria-label="入口 VPS" className="min-w-[161px]"><SelectValue>{gates.data?.find(g=>g.id===gid)?.name??"全部 VPS"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">全部 VPS</SelectItem>{gates.data?.map(g=><SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}</SelectContent></Select><Button onClick={()=>setEdit("new")} disabled={!gates.data?.length}>新建出口</Button></>}/>
    <ExitLanes routes={routes} exits={exits.data?.items??[]} known={routesKnown} failed={routeQueries.some(q=>q.isError)} onRetry={()=>routeQueries.forEach(q=>void q.refetch())}/>
    {!exits.data ? <State loading={exits.isPending} error={exits.error} onRetry={()=>exits.refetch()}/> : !exits.data.items.length ? <State empty="还没有出口"/> : <div className="exit-grid">{exits.data.items.map(e=><Card key={e.id} className="px-[21px] py-[19px]" {...dataObject({type:"exit",id:e.id})}>
      <div className="flex items-start justify-between gap-3"><b className="truncate text-name">{exitTitle(e)}</b>{e.udp==="unsupported"?<Badge variant="soft" tone="neutral">仅 TCP</Badge>:<span className="text-meta text-[var(--weir-ink-3)]">{e.udp==="supported"?"支持 UDP":"UDP 未知"}</span>}</div>
      <div className="mt-[3px] truncate font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{[gates.data?.find(g=>g.id===e.gate_id)?.name ?? "VPS 未知", e.address ? `${e.address}${e.port?`:${e.port}`:""}` : null, e.owner_user_id ? `用户 ${e.owner_user_id}` : "公共出口"].filter(Boolean).join(" · ")}</div>
      <p className="mt-[13px] mb-0 text-meta text-[var(--weir-ink-2)]">{e.references.length ? `${e.references.length} 处在用：${e.references.slice(0,3).map((r)=>r.name).join("、")}${e.references.length>3?" 等":""}。` : "暂无引用。"}{probeText(e.probe)}{!e.enabled && "（已停用）"}</p>
      <div className="mt-[13px] flex flex-wrap gap-1"><Button size="sm" variant="ghost" onClick={()=>setEdit(e)} disabled={e.tag==="direct"}>编辑</Button>{!["freedom","blackhole"].includes(e.kind)&&<Button size="sm" variant="ghost" busy={action.isBusy(e.id)} onClick={()=>void action.run(e.id,()=>api(`/admin/exits/${e.id}/probe`,"POST",{}),"实测完成")}>实测</Button>}{e.tag!=="direct"&&<Button size="sm" variant="ghost" className="text-[var(--weir-bad-ink)]" disabled={action.isBusy()} onClick={async()=>{if(await confirm({title:`删除出口「${exitTitle(e)}」？`,description:e.references.length?"此出口仍有引用，需先切换或删除引用对象。":"删除后无法恢复。",tone:"danger",confirmText:"删除"}))void action.run(e.id,()=>api(`/admin/exits/${e.id}`,"DELETE"),"已删除");}}>删除</Button>}</div>
    </Card>)}</div>}
    <PendingExits all />
    <SectionHeader title="分流"/><p>哪些网站走哪个出口。规则按优先级从小到大匹配；用户链路优先，不匹配时使用节点出口。</p>
    {(gates.data??[]).filter(g=>!gid||g.id===gid).map(g=><Routes key={g.id} gate={g} exits={exits.data?.items.filter(e=>e.gate_id===g.id&&e.owner_user_id==null)??[]}/>)}
    {edit&&<ExitEditor key={edit==="new"?"new":edit.id} value={edit} gateId={gid} gates={gates.data??[]} nodes={nodes.data??[]} close={()=>setEdit(null)} saved={()=>{setEdit(null);void exits.refetch();}}/>}
  </div>;
}
function ExitLanes({ routes, exits, known, failed, onRetry }: { routes: RouteItem[]; exits: ExitItem[]; known: boolean; failed: boolean; onRetry: () => void }) {
  if (!known) return <Card><CardContent><State loading /></CardContent></Card>;
  if (failed) return <Card><CardContent><State error="分流规则没有读全" onRetry={onRetry} /></CardContent></Card>;
  const lanes = routes.filter((r) => r.enabled).map((r) => {
    const found = exits.find((e) => e.id === r.exit_id);
    return { id: String(r.id), from: (r.domain || r.ip || "未命名规则").split("\n")[0], to: found ? exitTitle(found) : "出口 " + r.exit_id };
  });
  return (
    <Card>
      <CardContent className="stack">
        <div className="flex flex-wrap items-center justify-between gap-2"><strong>每类流量沿轨道流向它的出口</strong><small className="text-[var(--weir-ink-3)]">顺序由规则优先级决定，在下面的分流里改</small></div>
        <LaneMap label="分流轨道" lanes={lanes} empty="还没有启用的分流规则" />
      </CardContent>
    </Card>
  );
}
function ExitEditor({value,gateId,gates,nodes,close,saved}:{value:ExitItem|"new";gateId?:number;gates:Gate[];nodes:NodeItem[];close:()=>void;saved:()=>void}) {
  const old=value==="new"?undefined:value;
  const [gid,setGid]=useState(old?.gate_id??gateId??gates[0]?.id??0), [spec,setSpec]=useState<ExitSpec>(old?exitInput(old):{kind:"socks5"});
  const [enabled,setEnabled]=useState(old?.enabled??true), confirm=useConfirm();
  const save=useFormAction(async track=>{if(old?.references.length && !await confirm({title:"确认修改共享出口？",description:"以下对象都会受影响："+old.references.map(r=>r.name).join("、"),confirmText:"保存并影响全部引用"}))return false;return api(old?`/admin/exits/${old.id}`:"/admin/exits",old?"PATCH":"POST",{...spec,gate_id:gid,enabled},track);},{fields:{...exitErrorFields(spec.kind),invalid_remark:"remark"},onSuccess:r=>{if(r!==false)saved();}});
  useConsoleContext({dialog:{title:old?"编辑出口":"添加出口"}});
  return <Dialog open onOpenChange={o=>!o&&!save.busy&&close()}><DialogContent size="lg"><DialogTitle>{old?"编辑出口":"添加出口"}</DialogTitle><DialogDescription>修改共享出口会影响全部引用者。出口编号与入口 VPS 保存后固定。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}><Field label="入口 VPS" required><Select disabled={!!old} value={String(gid)} onValueChange={v=>setGid(Number(v))}><SelectTrigger><SelectValue>{gates.find(g=>g.id===gid)?.name}</SelectValue></SelectTrigger><SelectContent>{gates.map(g=><SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}</SelectContent></Select></Field><ExitFields admin={!old?.owner_user_id} sourcePath={old?.owner_user_id?`/admin/users/${old.owner_user_id}/chain-sources`:"/admin/chain-sources"} value={spec} onChange={setSpec} nodes={nodes.filter(n=>n.gate_id!==gid)} nodeHint="需要另一台 VPS 的节点"/><SwitchField label="启用" checked={enabled} onCheckedChange={setEnabled}/>{old?.references.length? <Notice>影响：{old.references.map(r=>r.name).join("、")}</Notice>:null}<DialogFooter error={save.error} status={<WaitText wait={save.wait}/>}><Button type="button" variant="outline" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
function Routes({gate,exits}:{gate:Gate;exits:ExitItem[]}) {
  const base=`/admin/gates/${gate.id}/routes`;
  const q=useQuery({queryKey:["routes",gate.id],queryFn:async()=>(await api(base)).items as RouteItem[]});
  const [edit,setEdit]=useState<RouteItem|"new"|null>(null), confirm=useConfirm();
  const action=useRowAction<number>({refetch:()=>q.refetch(),invalidate:[["exits"]]});
  return <Card><CardContent className="stack"><SectionHeader title={gate.name} actions={<Button variant="outline" onClick={()=>setEdit("new")}>添加规则</Button>}/>{!q.data?<State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/>:!q.data.length?<State empty="还没有分流规则"/>:q.data.map(r=><div key={r.id} {...dataObject({type:"route",id:r.id})} className="flex flex-wrap justify-between gap-2"><div className="min-w-0 break-words">#{r.priority} {r.domain} {r.ip} → {exits.find(e=>e.id===r.exit_id)?exitTitle(exits.find(e=>e.id===r.exit_id)!):`出口 ${r.exit_id}`}{!r.enabled&&"（停用）"}</div><div className="actions"><Button variant="outline" onClick={()=>setEdit(r)}>编辑</Button><Button variant="destructive" onClick={async()=>{if(await confirm({title:"删除分流规则？",tone:"danger",confirmText:"删除"}))void action.run(r.id,()=>api(`${base}/${r.id}`,"DELETE"),"已删除");}}>删除</Button></div></div>)}{edit&&<RouteEditor value={edit} base={base} exits={exits} close={()=>setEdit(null)} saved={()=>{setEdit(null);void q.refetch();}}/>}</CardContent></Card>;
}
function RouteEditor({value,base,exits,close,saved}:{value:RouteItem|"new";base:string;exits:ExitItem[];close:()=>void;saved:()=>void}) {
  const old=value==="new"?undefined:value;
  const [v,setV]=useState({exit_id:old?.exit_id??0,domain:old?.domain??"",ip:old?.ip??"",priority:old?.priority??0,enabled:old?.enabled??true,node_id:old?.node_id,user_id:old?.user_id});
  const save=useFormAction(()=>api(old?`${base}/${old.id}`:base,old?"PATCH":"POST",v),{fields:{exit_not_allowed:"exit_id",invalid_route_match:"domain",invalid_priority:"priority"},invalidate:[["exits"]],onSuccess:saved});
  useConsoleContext({dialog:{title:"分流规则"}});
  return <Dialog open onOpenChange={o=>!o&&!save.busy&&close()}><DialogContent><DialogTitle>分流规则</DialogTitle><DialogDescription>域名或 IP 至少填一项；整个节点的出口请在节点编辑中设置。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}><Field label="出口" name="exit_id" required><Select required value={v.exit_id?String(v.exit_id):""} onValueChange={id=>setV({...v,exit_id:Number(id)})}><SelectTrigger><SelectValue>{exits.find(e=>e.id===v.exit_id)?<ExitSummary exit={exits.find(e=>e.id===v.exit_id)!} />:"请选择"}</SelectValue></SelectTrigger><SelectContent>{exits.filter(e=>e.enabled).map(e=><SelectItem key={e.id} value={String(e.id)}><ExitSummary exit={e} /></SelectItem>)}</SelectContent></Select></Field><Field label="域名" name="domain" customError={!v.domain.trim()&&!v.ip.trim()?"域名或 IP 至少填一项":""}><Textarea value={v.domain} onChange={e=>setV({...v,domain:e.target.value})}/></Field><Field label="IP / CIDR" name="ip"><Textarea value={v.ip} onChange={e=>setV({...v,ip:e.target.value})}/></Field><Field label="优先级" name="priority"><Input type="number" min={0} max={100000} value={v.priority} onChange={e=>setV({...v,priority:Number(e.target.value)})}/></Field><SwitchField label="启用" checked={v.enabled} onCheckedChange={enabled=>setV({...v,enabled})}/><DialogFooter error={save.error}><Button type="button" variant="outline" onClick={close}>取消</Button><Button type="submit" busy={save.busy}>保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
