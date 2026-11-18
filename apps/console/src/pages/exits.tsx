import { PendingExits } from "../components/pending-exits";
import { DomainCandidates } from "../components/domain-candidates";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { ActionGroup, Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, InsetGroup, InsetRow, InsetRows, LaneMap, Notice, PageHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, SwitchGroup, Textarea, useConfirm } from "@weir/ui";
import { Activity, Pencil, Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { bytes } from "../lib/models";
import { objectStatus, type ExitItem, type ExitSpec, type Gate, type NodeItem } from "../lib/core";
import { exitChoices, exitInput, exitTitle, useExitCatalog } from "../lib/exits";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { ExitFields, exitErrorFields, exitKindName, probeText } from "../components/chain-exit";
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
  const externalItems = (exits.data?.items ?? []).filter(e => e.kind !== "freedom" && e.kind !== "blackhole");
  const tcpOnly = externalItems.filter(e => e.kind === "http" || e.kind === "https").length;
  const nodes=useQuery({queryKey:["nodes"],queryFn:async()=>(await api("/admin/nodes")).items as NodeItem[]});
  const detail=useDialogParam("exit",exits.data?.items,!!exits.data);
  const [creating,setCreating]=useState(false);
  const [probe,setProbe]=useState<ExitItem|null>(null);
  const edit: ExitItem | "new" | null = creating ? "new" : detail.current;
  const setEdit=(value:ExitItem|"new"|null)=>{setCreating(value==="new");if(value&&value!=="new")detail.open(value);else detail.close();};
  const confirm=useConfirm();
  const action=useRowAction<number>({invalidate:[["exits"]]});
  const pageTitle = view === "rules" ? "规则" : view === "proxies" ? "代理" : "出口与分流";
  useConsoleContext({page:pageTitle,list:{total:externalItems.length,items:externalItems.slice(0,20).map(e=>({type:"exit",id:e.id,name:exitTitle(e)}))}});
  return <div className="stack"><PageHeader size="data" title={pageTitle} description={<span className="weir-sum"><span><b>{exits.data ? externalItems.length : "未知"}</b> 个代理</span>{view !== "proxies" && <span><b>{routesKnown ? routes.length : "未知"}</b> 条分流规则</span>}{tcpOnly > 0 && <span>{tcpOnly} 个仅支持 TCP</span>}</span>} actions={<><Select value={gid?String(gid):"all"} onValueChange={v=>setParams(v==="all"?{}:{gate:String(v)})}><SelectTrigger aria-label="入口 VPS" className="min-w-[161px]"><SelectValue>{gates.data?.find(g=>g.id===gid)?.name??"全部 VPS"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">全部 VPS</SelectItem>{gates.data?.map(g=><SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}</SelectContent></Select>{view !== "rules" && <Button onClick={()=>setEdit("new")}><Plus />添加代理</Button>}</>}/>
    {(gates.isPending || gates.error) ? <State loading={gates.isPending && !gates.error} error={gates.error} onRetry={()=>void gates.refetch()}/> : view !== "proxies" && routes.length > 0 && <ExitLanes routes={routes} exits={exits.data?.items??[]} known={routesKnown} failed={routeQueries.some(q=>q.isError)} onRetry={()=>routeQueries.forEach(q=>void q.refetch())}/>}
    {view !== "rules" && <>{!exits.data ? <State loading={exits.isPending} error={exits.error} onRetry={()=>exits.refetch()}/> : !exits.data.directory.length ? <State empty="还没有代理" detail="添加 SOCKS5 / HTTP 代理、节点链接或中转节点后，可供多台 VPS 节点与分流规则复用。"/> : <div className="grid gap-[var(--weir-space-4)] xl:grid-cols-2">{exits.data.directory.map(group=>{
      const primary = group.deployments[0];
      const relayNode = group.kind === "node" && primary?.node_id ? (nodes.data ?? []).find((n) => n.id === primary.node_id) : undefined;
      const relayGate = relayNode ? (relayNode.gate_name || (gates.data ?? []).find((g) => g.id === relayNode.gate_id)?.name) : undefined;
      const addrPort = group.address ? (group.port ? `${group.address}:${group.port}` : group.address) : "";
      const title = primary?.remark?.trim() || (relayNode ? `中转 · ${relayNode.name}` : addrPort || exitTitle(primary));
      const scopeLabel = group.owner_user_id ? `用户 #${group.owner_user_id}` : "系统共享";
      const kindLabel = relayNode ? "VPS 中转" : group.kind === "socks5" ? "SOCKS5 代理" : group.kind === "http" ? "HTTP 代理" : group.kind === "https" ? "HTTPS 代理" : (exitKindName[group.kind] ?? group.kind);
      return <Card key={group.id} className="flex flex-col">
        <CardContent className="flex flex-1 flex-col justify-between gap-[var(--weir-space-4)] p-[var(--weir-space-5)]">
          {group.conflict && <Notice tone="warn">{group.reason}</Notice>}
          {group.deployments.map((e, idx) => {
            const [, stateTone, stateReason] = objectStatus(e.object_state);
            const centerProbe = (e.probes ?? []).find((p) => p.location === "center");
            const udpLabel = e.kind === "blackhole" ? "拦截流量" : e.udp === "supported" ? "支持 UDP" : e.udp === "unsupported" ? "仅 TCP" : "UDP 未知";
            const itemNode = e.kind === "node" && e.node_id ? (nodes.data ?? []).find((n) => n.id === e.node_id) : undefined;
            const itemGate = itemNode ? (itemNode.gate_name || (gates.data ?? []).find((g) => g.id === itemNode.gate_id)?.name) : relayGate;
            const itemAddr = e.address ? (e.port ? `${e.address}:${e.port}` : e.address) : addrPort;
            const itemTitle = e.remark?.trim() || (itemNode ? `中转 · ${itemNode.name}` : title);
            const regionText = !itemNode && e.summary?.region && e.summary.region !== "地区未知" && e.summary.region !== itemTitle ? e.summary.region : null;
            const metaLine = [
              kindLabel,
              itemNode ? `落地 ${itemGate || "另一台 VPS"}` : (itemAddr && itemAddr !== itemTitle ? itemAddr : null),
              regionText,
              scopeLabel,
              e.username ? `账号 ${e.username}` : null,
            ].filter(Boolean).join(" · ");
            const uses = e.uses ?? [];
            return <div key={e.id} {...dataObject({ type: "exit", id: e.id })} className={idx > 0 ? "flex flex-1 flex-col justify-between gap-[var(--weir-space-4)] border-t border-[var(--weir-line)] pt-[var(--weir-space-4)]" : "flex flex-1 flex-col justify-between gap-[var(--weir-space-4)]"}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 flex-1 basis-[201px] space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <strong className="break-all text-headline font-semibold">{itemTitle}</strong>
                    <Badge variant="soft" tone={e.udp === "supported" ? "ok" : e.udp === "unsupported" ? "warn" : "neutral"}>{udpLabel}</Badge>
                    {!e.enabled && <Badge variant="soft" tone="neutral">已停用</Badge>}
                    {stateTone === "bad" && <Badge variant="soft" tone="bad">异常</Badge>}
                  </div>
                  <div className="break-words text-meta text-[var(--weir-ink-3)]">{metaLine}</div>
                </div>
                <ActionGroup wrap={false}>
                  <Button size="sm" variant="outline" onClick={() => setEdit(e)}><Pencil />编辑</Button>
                  {e.kind !== "blackhole" && e.kind !== "freedom" && <Button size="sm" variant="outline" onClick={() => setProbe(e)}><Activity />实测</Button>}
                  <Button size="icon-sm" variant="ghost" aria-label={`删除 ${itemTitle}`} disabled={action.isBusy()} onClick={async () => {
                    if (await confirm({ title: `删除代理「${itemTitle}」？`, description: e.references.length ? "此代理仍被节点或规则引用，请先切换或移除引用对象。" : "删除后无法恢复。", tone: "danger", confirmText: "删除" })) void action.run(e.id, () => api(`/admin/exits/${e.id}`, "DELETE"), "已删除");
                  }}><Trash2 /></Button>
                </ActionGroup>
              </div>
              <div className="border-t border-[var(--weir-line-soft)] pt-[var(--weir-space-3)]">
                <InsetRows>
                  <InsetRow className="flex min-h-[29px] flex-wrap items-center justify-between gap-2 py-2">
                    <span className="text-meta text-[var(--weir-ink-3)]">引用对象</span>
                    {e.references.length === 0 ? (
                      <span className="text-meta text-[var(--weir-ink-3)]">暂未引用</span>
                    ) : (
                      <span className="flex flex-wrap items-center justify-end gap-1.5">
                        {e.references.map((ref) => (
                          <Badge key={`${ref.type}:${ref.id}`} variant="soft" tone={ref.enabled ? "info" : "neutral"}>
                            <ObjectLink to={{ type: ref.type, id: ref.id, name: ref.name, owner: ref.owner }} />
                          </Badge>
                        ))}
                      </span>
                    )}
                  </InsetRow>
                  <InsetRow className="flex min-h-[29px] flex-wrap items-center justify-between gap-2 py-2">
                    <span className="text-meta text-[var(--weir-ink-3)]">连通与下发</span>
                    {uses.length > 0 ? (
                      <span className="flex flex-wrap items-center justify-end gap-1.5">
                        {uses.map((use) => (
                          <Badge key={use.gate_id} variant="soft" tone={use.status === "applied" ? "ok" : use.status === "kept" ? "warn" : "neutral"}>
                            {use.name ?? `VPS #${use.gate_id}`} · {use.status === "applied" ? "已生效" : use.status === "kept" ? "保留原配置" : "待验证"}{use.probe?.latency_ms != null ? ` · ${use.probe.latency_ms} ms` : use.probe && !use.probe.ok ? " · 实测未通" : ""}
                          </Badge>
                        ))}
                      </span>
                    ) : (
                      <span className="text-meta text-[var(--weir-ink-3)]">{centerProbe ? probeText(centerProbe) : (stateReason || "尚未在 VPS 上使用")}</span>
                    )}
                  </InsetRow>
                </InsetRows>
              </div>
            </div>;
          })}
        </CardContent>
      </Card>;
    })}</div>}
    <PendingExits all /></>
    }
    {view !== "proxies" && <>
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
  const save=useFormAction(async track=>{if(old?.references.length && !await confirm({title:"确认修改共享代理？",description:"以下对象都会受影响："+old.references.map(r=>r.name).join("、"),confirmText:"保存并影响全部引用"}))return false;return api(old?`/admin/exits/${old.id}`:"/admin/exits",old?"PATCH":"POST",{...spec,enabled},track);},{fields:{...exitErrorFields(spec.kind),invalid_remark:"remark",exit_gate_rejected:"kind"},invalidate:[["exits"]],onSuccess:r=>{if(r&&typeof r==="object"&&"applications" in r)setApplications((r as ExitItem).applications);if(r!==false && !(r as ExitItem|undefined)?.applications?.some(item=>item.status!=="applied"))saved();}});
  useConsoleContext({dialog:{title:old?"编辑代理":"添加代理"}});
  return <Dialog open onOpenChange={o=>!o&&!save.busy&&close()}><DialogContent size="lg"><DialogTitle>{old?"编辑代理":"添加代理"}</DialogTitle><DialogDescription>保存可供节点与分流规则复用的连接资料；已被引用时，关联 VPS 验证通过后才替换生效配置。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}><ExitFields savedSecrets={old?.kind===spec.kind ? {password:old.has_password,link:old.has_link} : undefined} admin={!old?.owner_user_id} sourcePath={old?.owner_user_id?`/admin/users/${old.owner_user_id}/chain-sources`:"/admin/chain-sources"} value={spec} onChange={setSpec} nodes={nodes} nodeHint="需要另一台 VPS 的节点"/>{old&&<ExitCredentials exit={old}/>}<SwitchGroup><SwitchField placement="end" label="启用此代理" description="关闭后不可被新建节点与分流规则选用" checked={enabled} onCheckedChange={setEnabled}/></SwitchGroup>{old?.references.length? <Notice>影响对象：{old.references.map(r=>r.name).join("、")}</Notice>:null}{applications?.length?<Notice>{applications.map(item=>`${item.name??`VPS ${item.gate_id}`}：${item.status==="applied"?"已替换":item.status==="waiting"?"等待重试":item.status==="kept"?"保留原配置":"未应用"}${item.reason?`，${item.reason}`:""}`).join("；")}</Notice>:null}<DialogFooter error={save.error} status={<WaitText wait={save.wait}/>}><Button type="button" variant="ghost" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
function Routes({gate,exits}:{gate:Gate;exits:ExitItem[]}) {
  const base=`/admin/gates/${gate.id}/routes`;
  const q=useQuery({queryKey:["routes",gate.id],queryFn:async()=>(await api(base)).items as RouteItem[]});
  const [edit,setEdit]=useState<RouteItem|"new"|null>(null), confirm=useConfirm();
  const action=useRowAction<number>({refetch:()=>q.refetch(),invalidate:[["exits"]]});
  return <InsetGroup title={gate.name} actions={<Button size="sm" variant="outline" onClick={()=>setEdit("new")}><Plus />添加规则</Button>}>{!q.data?<State loading={q.isPending} error={q.error} onRetry={()=>q.refetch()}/>:!q.data.length?<State empty="还没有分流规则"/>:<InsetRows>{q.data.map(r=><InsetRow key={r.id} {...dataObject({type:"route",id:r.id})} className="flex flex-wrap items-center justify-between gap-3"><div className="min-w-0 space-y-1 break-words"><div className="flex flex-wrap items-center gap-2"><span className="font-mono text-meta text-[var(--weir-ink-3)]">#{r.priority}</span><strong className="font-mono text-body">{[r.domain, r.ip].filter(Boolean).join(" · ")}</strong><Badge variant="soft" tone={r.enabled ? "info" : "neutral"}>⇢ {exits.find(e=>e.id===r.exit_id)?exitTitle(exits.find(e=>e.id===r.exit_id)!):`代理 #${r.exit_id}`}{!r.enabled&&" · 停用"}</Badge></div><small className="block text-[var(--weir-ink-3)]" title={r.usage?.reason}>近 30 天 {r.usage?.bytes == null ? "用量未知" : bytes(r.usage.bytes)} · {r.usage?.coverage === "native" ? "原生计数" : r.usage?.coverage === "partial" ? "部分覆盖" : "尚无观测"}{r.usage?.observed_at && ` · ${new Date(r.usage.observed_at).toLocaleString()}`}</small></div><ActionGroup wrap={false}><Button size="sm" variant="outline" onClick={()=>setEdit(r)}><Pencil />编辑</Button><Button size="icon-sm" variant="ghost" aria-label={`删除规则 #${r.priority}`} onClick={async()=>{if(await confirm({title:"删除分流规则？",tone:"danger",confirmText:"删除"}))void action.run(r.id,()=>api(`${base}/${r.id}`,"DELETE"),"已删除");}}><Trash2 /></Button></ActionGroup></InsetRow>)}</InsetRows>}{edit&&<RouteEditor value={edit} base={base} gateId={gate.id} close={()=>setEdit(null)} saved={()=>{setEdit(null);void q.refetch();}}/>}</InsetGroup>;
}
function RouteEditor({value,base,gateId,close,saved}:{value:RouteItem|"new";base:string;gateId:number;close:()=>void;saved:()=>void}) {
  const catalog = useExitCatalog(true);
  const exits = exitChoices(catalog.data?.items ?? [], gateId, null);
  const old=value==="new"?undefined:value;
  const [v,setV]=useState({exit_id:old?.exit_id??0,domain:old?.domain??"",ip:old?.ip??"",priority:old?.priority??0,enabled:old?.enabled??true,node_id:old?.node_id,user_id:old?.user_id});
  const save=useFormAction(()=>api(old?`${base}/${old.id}`:base,old?"PATCH":"POST",v),{fields:{exit_not_allowed:"exit_id",invalid_route_match:"domain",invalid_priority:"priority"},invalidate:[["exits"]],onSuccess:saved});
  useConsoleContext({dialog:{title:"分流规则"}});
  return <Dialog open onOpenChange={o=>!o&&!save.busy&&close()}><DialogContent size="md"><DialogTitle>{old ? "编辑分流规则" : "添加分流规则"}</DialogTitle><DialogDescription>域名或 IP 至少填一项（每行一条）；整个节点的默认出口请在节点编辑中设置。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={()=>void save.submit()}><div className="grid min-w-0 gap-x-[var(--weir-space-3)] gap-y-[var(--weir-gap-9)] sm:grid-cols-[minmax(0,1fr)_131px]"><Field label="目标出口" name="exit_id" required><Select required value={v.exit_id?String(v.exit_id):""} onValueChange={id=>setV({...v,exit_id:Number(id)})}><SelectTrigger><SelectValue>{exits.find(e=>e.id===v.exit_id)?<ExitSummary exit={exits.find(e=>e.id===v.exit_id)!} />:"请选择"}</SelectValue></SelectTrigger><SelectContent>{exits.map(e=><SelectItem key={e.id} value={String(e.id)}><ExitSummary exit={e} /></SelectItem>)}</SelectContent></Select></Field><Field label="优先级" name="priority" hint="越小越优先"><Input type="number" min={0} max={100000} value={v.priority} onChange={e=>setV({...v,priority:Number(e.target.value)})}/></Field></div>{!catalog.data ? <State loading={catalog.isPending} error={catalog.error} onRetry={()=>catalog.refetch()}/> : !exits.length && <State empty="没有可选的出口" detail="请先在「代理」页添加并启用出口。"/>}<FormGrid><Field label="匹配域名" name="domain" hint="例如 openai.com" customError={!v.domain.trim()&&!v.ip.trim()?"域名或 IP 至少填一项":""}><Textarea rows={4} mono value={v.domain} onChange={e=>setV({...v,domain:e.target.value})}/></Field><Field label="匹配 IP / CIDR" name="ip" hint="例如 1.1.1.0/24"><Textarea rows={4} mono value={v.ip} onChange={e=>setV({...v,ip:e.target.value})}/></Field></FormGrid><SwitchGroup><SwitchField placement="end" label="启用此规则" checked={v.enabled} onCheckedChange={enabled=>setV({...v,enabled})}/></SwitchGroup><DialogFooter error={save.error}><Button type="button" variant="ghost" onClick={close}>取消</Button><Button type="submit" busy={save.busy}>保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
