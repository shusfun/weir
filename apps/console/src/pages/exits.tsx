import { PendingExits } from "../components/pending-exits";
import { DomainCandidates } from "../components/domain-candidates";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, LaneMap, Notice, PageHeader, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Textarea, useConfirm } from "@weir/ui";
import { api } from "../api";
import { bytes } from "../lib/models";
import { objectStatus, type ExitItem, type ExitSpec, type Gate, type NodeItem } from "../lib/core";
import { exitInput, exitTitle, useExitCatalog } from "../lib/exits";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { ExitFields, exitErrorFields, probeText } from "../components/chain-exit";
import { ObjectLink, dataObject, useDialogParam } from "../components/object-link";
import { State } from "../components/feedback";
import { ExitSummary } from "../components/node-summary";
import { WaitText } from "../components/wait";
import { ExitProbe } from "../components/exit-probe";
import { ExitCredentials } from "../components/exit-credentials";

interface RouteItem {id:number;gate_id:number;exit_id:number;domain:string;ip:string;priority:number;enabled:boolean;node_id?:number;user_id?:number;usage?:{bytes:number|null;observed_at:string|null;coverage:string;reason:string}}
export function ExitsPage({ view = "all" }: { view?: "all" | "proxies" | "rules" }) {
  const [params,setParams]=useSearchParams(), gid=Number(params.get("gate"))||undefined;
  const exits=useExitCatalog(true,gid);
  const gates=useQuery({queryKey:["gates"],queryFn:async()=>(await api("/admin/gates")).items as Gate[]});
  const visible=(gates.data??[]).filter(g=>!gid||g.id===gid);
  const routeQueries=useQueries({queries:visible.map(g=>({queryKey:["routes",g.id],queryFn:async()=>(await api(`/admin/gates/${g.id}/routes`)).items as RouteItem[]}))});
  const routes=routeQueries.flatMap(q=>q.data??[]);
  const routesKnown=gates.isSuccess&&routeQueries.every(q=>q.isSuccess);
  const tcpOnly=(exits.data?.items??[]).filter(e=>e.kind==="http"||e.kind==="https").length;
  const nodes=useQuery({queryKey:["nodes"],queryFn:async()=>(await api("/admin/nodes")).items as NodeItem[]});
  const detail=useDialogParam("exit",exits.data?.items,!!exits.data);
  const [creating,setCreating]=useState(false);
  const [probe,setProbe]=useState<ExitItem|null>(null);
  const edit: ExitItem | "new" | null = creating ? "new" : detail.current;
  const setEdit=(value:ExitItem|"new"|null)=>{setCreating(value==="new");if(value&&value!=="new")detail.open(value);else detail.close();};
  const confirm=useConfirm();
  const action=useRowAction<number>({invalidate:[["exits"]]});
  useConsoleContext({page:view === "rules" ? "规则" : view === "proxies" ? "代理" : "出口与分流",list:{total:exits.data?.items.length??0,items:(exits.data?.items??[]).slice(0,20).map(e=>({type:"exit",id:e.id,name:exitTitle(e)}))}});
  return <div className="stack"><PageHeader size="data" title={view === "rules" ? "规则" : view === "proxies" ? "代理" : "出口与分流"} description={<span className="weir-sum"><span><b>{exits.data ? exits.data.items.length : "未知"}</b> 个出口</span><span><b>{routesKnown ? routes.length : "未知"}</b> 条分流规则</span><span>{exits.data ? `${tcpOnly} 个只能走 TCP` : "TCP 限制未知"}</span></span>} actions={<><Select value={gid?String(gid):"all"} onValueChange={v=>setParams(v==="all"?{}:{gate:String(v)})}><SelectTrigger aria-label="入口 VPS" className="min-w-[161px]"><SelectValue>{gates.data?.find(g=>g.id===gid)?.name??"全部 VPS"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">全部 VPS</SelectItem>{gates.data?.map(g=><SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}</SelectContent></Select>{view !== "rules" && <Button onClick={()=>setEdit("new")}>新建代理</Button>}</>}/>
    {(gates.isPending || gates.error) ? <State loading={gates.isPending && !gates.error} error={gates.error} onRetry={()=>void gates.refetch()}/> : view !== "proxies" && <ExitLanes routes={routes} exits={exits.data?.items??[]} known={routesKnown} failed={routeQueries.some(q=>q.isError)} onRetry={()=>routeQueries.forEach(q=>void q.refetch())}/>}
    {view !== "rules" && <>{!exits.data ? <State loading={exits.isPending} error={exits.error} onRetry={()=>exits.refetch()}/> : !exits.data.directory.length ? <State empty="还没有代理"/> : <div className="exit-grid">{exits.data.directory.map(group=><Card key={group.id}><CardContent className="stack">
      <SectionHeader flush title={group.address ? `${group.address}:${group.port}` : exitTitle(group.deployments[0])} description={`${group.owner_user_id ? `用户 ${group.owner_user_id}` : "系统代理"} · ${new Set(group.deployments.flatMap(e => (e.uses ?? []).map(use => use.gate_id))).size} 台 VPS 使用`}/>
      {group.conflict&&<Notice tone="warn">{group.reason}</Notice>}
      {group.deployments.map(e=><div className="stack" key={e.id} {...dataObject({type:"exit",id:e.id})}>
        <div>{e.username || "无用户名"} · {e.references.length} 处引用 <Badge variant="soft" tone="neutral">{e.kind==="blackhole"?"拦截 TCP 与 UDP":e.udp==="supported"?"支持 UDP":e.udp==="unsupported"?"仅 TCP":"UDP 未知"}</Badge></div>
        <p>{objectStatus(e.object_state)[2]}</p>
        {(e.probes??[]).filter((p)=>p.location==="center").map((p)=><p key="center">{probeText(p)}</p>)}
        {(e.uses??[]).map((use)=><p key={use.gate_id}>{use.name??`VPS ${use.gate_id}`}：{use.status==="applied"?"已生效":use.status==="kept"?"保留原配置":"未知"}{use.reason?` · ${use.reason}`:""}{use.probe?` · ${probeText(use.probe)}`:" · 还没有这台 VPS 的实测"}</p>)}
        <div className="actions"><Button size="sm" variant="outline" onClick={()=>setEdit(e)}>详情与编辑</Button>{e.kind!=="blackhole"&&e.kind!=="freedom"&&<Button size="sm" variant="ghost" onClick={()=>setProbe(e)}>实测</Button>}<Button size="sm" variant="ghost" disabled={action.isBusy()} onClick={async()=>{if(await confirm({title:`删除出口「${exitTitle(e)}」？`,description:e.references.length?"此出口仍有引用，需先切换或删除引用对象。":"删除后无法恢复。",tone:"danger",confirmText:"删除"}))void action.run(e.id,()=>api(`/admin/exits/${e.id}`,"DELETE"),"已删除");}}>删除</Button></div>
      </div>)}
    </CardContent></Card>)}</div>}
    <PendingExits all /></>
    }
    {view !== "proxies" && <><SectionHeader flush title="分流"/><p>哪些网站走哪个出口。规则按优先级从小到大匹配；用户链路优先，不匹配时使用节点出口。</p>
    {(gates.data??[]).filter(g=>!gid||g.id===gid).map(g=><Routes key={g.id} gate={g} exits={exits.data?.items.filter(e=>e.owner_user_id==null&&(e.gate_id==null||e.gate_id===g.id))??[]}/>)}</>}
    {view === "rules" && <DomainCandidates gateId={gid} />}
    {edit&&<ExitEditor key={edit==="new"?"new":edit.id} value={edit} nodes={nodes.data??[]} close={()=>setEdit(null)} saved={()=>{setEdit(null);void exits.refetch();}}/>}
    {probe&&<ExitProbe exit={probe} base="/admin" gates={gates.data??[]} loading={gates.isPending} error={gates.error} retry={()=>void gates.refetch()} close={()=>setProbe(null)} saved={()=>void exits.refetch()}/>}
  </div>;
}
function ExitLanes({ routes, exits, known, failed, onRetry }: { routes: RouteItem[]; exits: ExitItem[]; known: boolean; failed: boolean; onRetry: () => void }) {
  if (failed) return <Card><CardContent><State error="分流规则没有读全" onRetry={onRetry} /></CardContent></Card>;
  if (!known) return <Card><CardContent><State loading /></CardContent></Card>;
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
function ExitEditor({value,nodes,close,saved}:{value:ExitItem|"new";nodes:NodeItem[];close:()=>void;saved:()=>void}) {
  const old=value==="new"?undefined:value;
  const [spec,setSpec]=useState<ExitSpec>(old?exitInput(old):{kind:"socks5"});
  const [enabled,setEnabled]=useState(old?.enabled??true), confirm=useConfirm();
  const [applications,setApplications]=useState<ExitItem["applications"]>();
  const save=useFormAction(async track=>{if(old?.references.length && !await confirm({title:"确认修改共享出口？",description:"以下对象都会受影响："+old.references.map(r=>r.name).join("、"),confirmText:"保存并影响全部引用"}))return false;return api(old?`/admin/exits/${old.id}`:"/admin/exits",old?"PATCH":"POST",{...spec,enabled},track);},{fields:{...exitErrorFields(spec.kind),invalid_remark:"remark",exit_gate_rejected:"kind"},onSuccess:r=>{if(r&&typeof r==="object"&&"applications" in r)setApplications((r as ExitItem).applications);if(r!==false && !(r as ExitItem|undefined)?.applications?.some(item=>item.status!=="applied"))saved();}});
  useConsoleContext({dialog:{title:old?"编辑出口":"添加出口"}});
  return <Dialog open onOpenChange={o=>!o&&!save.busy&&close()}><DialogContent size="lg"><DialogTitle>{old?"编辑出口":"添加出口"}</DialogTitle><DialogDescription>只保存出口资料。已被使用时，每台 VPS 验证通过才替换生效配置。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}><ExitFields savedSecrets={old?.kind===spec.kind ? {password:old.has_password,link:old.has_link} : undefined} admin={!old?.owner_user_id} sourcePath={old?.owner_user_id?`/admin/users/${old.owner_user_id}/chain-sources`:"/admin/chain-sources"} value={spec} onChange={setSpec} nodes={nodes} nodeHint="需要另一台 VPS 的节点"/>{old&&<ExitCredentials exit={old}/>}<SwitchField label="启用" checked={enabled} onCheckedChange={setEnabled}/>{old?.references.length? <Notice>影响：{old.references.map(r=>r.name).join("、")}</Notice>:null}{applications?.length?<Notice>{applications.map(item=>`${item.name??`VPS ${item.gate_id}`}：${item.status==="applied"?"已替换":item.status==="waiting"?"等待重试":item.status==="kept"?"保留原配置":"未应用"}${item.reason?`，${item.reason}`:""}`).join("；")}</Notice>:null}<DialogFooter error={save.error} status={<WaitText wait={save.wait}/>}><Button type="button" variant="outline" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
function Routes({gate,exits}:{gate:Gate;exits:ExitItem[]}) {
  const base=`/admin/gates/${gate.id}/routes`;
  const q=useQuery({queryKey:["routes",gate.id],queryFn:async()=>(await api(base)).items as RouteItem[]});
  const [edit,setEdit]=useState<RouteItem|"new"|null>(null), confirm=useConfirm();
  const action=useRowAction<number>({refetch:()=>q.refetch(),invalidate:[["exits"]]});
  return <Card><CardContent className="stack"><SectionHeader flush title={gate.name} actions={<Button variant="outline" onClick={()=>setEdit("new")}>添加规则</Button>}/>{!q.data?<State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/>:!q.data.length?<State empty="还没有分流规则"/>:q.data.map(r=><div key={r.id} {...dataObject({type:"route",id:r.id})} className="flex flex-wrap justify-between gap-2"><div className="min-w-0 break-words">#{r.priority} {r.domain} {r.ip} → {exits.find(e=>e.id===r.exit_id)?exitTitle(exits.find(e=>e.id===r.exit_id)!):`出口 ${r.exit_id}`}{!r.enabled&&"（停用）"}<p className="text-[var(--weir-ink-3)]" title={r.usage?.reason}>近 30 天 {r.usage?.bytes == null ? "用量未知" : bytes(r.usage.bytes)} · {r.usage?.coverage === "native" ? "原生计数" : r.usage?.coverage === "partial" ? "部分覆盖 / 观测过期" : "尚无观测"}{r.usage?.observed_at && ` · ${new Date(r.usage.observed_at).toLocaleString()}`}</p></div><div className="actions"><Button variant="outline" onClick={()=>setEdit(r)}>编辑</Button><Button variant="destructive" onClick={async()=>{if(await confirm({title:"删除分流规则？",tone:"danger",confirmText:"删除"}))void action.run(r.id,()=>api(`${base}/${r.id}`,"DELETE"),"已删除");}}>删除</Button></div></div>)}{edit&&<RouteEditor value={edit} base={base} exits={exits} close={()=>setEdit(null)} saved={()=>{setEdit(null);void q.refetch();}}/>}</CardContent></Card>;
}
function RouteEditor({value,base,exits,close,saved}:{value:RouteItem|"new";base:string;exits:ExitItem[];close:()=>void;saved:()=>void}) {
  const old=value==="new"?undefined:value;
  const [v,setV]=useState({exit_id:old?.exit_id??0,domain:old?.domain??"",ip:old?.ip??"",priority:old?.priority??0,enabled:old?.enabled??true,node_id:old?.node_id,user_id:old?.user_id});
  const save=useFormAction(()=>api(old?`${base}/${old.id}`:base,old?"PATCH":"POST",v),{fields:{exit_not_allowed:"exit_id",invalid_route_match:"domain",invalid_priority:"priority"},invalidate:[["exits"]],onSuccess:saved});
  useConsoleContext({dialog:{title:"分流规则"}});
  return <Dialog open onOpenChange={o=>!o&&!save.busy&&close()}><DialogContent><DialogTitle>分流规则</DialogTitle><DialogDescription>域名或 IP 至少填一项；整个节点的出口请在节点编辑中设置。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}><Field label="出口" name="exit_id" required><Select required value={v.exit_id?String(v.exit_id):""} onValueChange={id=>setV({...v,exit_id:Number(id)})}><SelectTrigger><SelectValue>{exits.find(e=>e.id===v.exit_id)?<ExitSummary exit={exits.find(e=>e.id===v.exit_id)!} />:"请选择"}</SelectValue></SelectTrigger><SelectContent>{exits.filter(e=>e.enabled).map(e=><SelectItem key={e.id} value={String(e.id)}><ExitSummary exit={e} /></SelectItem>)}</SelectContent></Select></Field><Field label="域名" name="domain" customError={!v.domain.trim()&&!v.ip.trim()?"域名或 IP 至少填一项":""}><Textarea value={v.domain} onChange={e=>setV({...v,domain:e.target.value})}/></Field><Field label="IP / CIDR" name="ip"><Textarea value={v.ip} onChange={e=>setV({...v,ip:e.target.value})}/></Field><Field label="优先级" name="priority"><Input type="number" min={0} max={100000} value={v.priority} onChange={e=>setV({...v,priority:Number(e.target.value)})}/></Field><SwitchField label="启用" checked={v.enabled} onCheckedChange={enabled=>setV({...v,enabled})}/><DialogFooter error={save.error}><Button type="button" variant="outline" onClick={close}>取消</Button><Button type="submit" busy={save.busy}>保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
