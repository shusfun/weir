import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { ActionGroup, Badge, Button, Card, CardContent, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, InsetGroup, InsetRow, InsetRows, LaneMap, Notice, PageHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, SwitchGroup, Textarea, useConfirm, useToast } from "@weir/ui";
import { Activity, Pencil, Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { bytes } from "../lib/models";
import type { ExitItem, ExitSpec, Gate, NodeItem } from "../lib/core";
import { exitChoices, exitInput, exitTitle, useExitCatalog } from "../lib/exits";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { PendingExits } from "../components/pending-exits";
import { DomainCandidates } from "../components/domain-candidates";
import { ExitFields, exitErrorFields } from "../components/chain-exit";
import { ObjectLink, dataObject, useDialogParam } from "../components/object-link";
import { State } from "../components/feedback";
import { SyncLine } from "../components/sync-line";
import { ExitSummary } from "../components/node-summary";
import { WaitText } from "../components/wait";
import { ExitProbe } from "../components/exit-probe";
import { ExitCredentials } from "../components/exit-credentials";

interface RouteItem {id:number;gate_id:number;exit_id:number;domain:string;ip:string;priority:number;enabled:boolean;node_id?:number;user_id?:number;usage?:{bytes:number|null;observed_at:string|null;coverage:string;reason:string}}

export function ProxiesPage() {
  const catalog = useExitCatalog(true);
  const items = catalog.data?.items.filter((exit) => exit.kind !== "freedom");
  const detail = useDialogParam("exit", items, catalog.isSuccess);
  const [creating, setCreating] = useState(false);
  const [probe, setProbe] = useState<ExitItem | null>(null);
  const edit = creating ? "new" : detail.current;
  const nodes = useQuery({ queryKey: ["nodes"], enabled: !!edit, queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const gates = useQuery({ queryKey: ["gates"], enabled: !!probe, queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const owners = new Map<number | null, NonNullable<typeof catalog.data>["directory"]>();
  for (const group of catalog.data?.directory ?? []) {
    const groups = owners.get(group.owner_user_id) ?? [];
    groups.push(group);
    owners.set(group.owner_user_id, groups);
  }
  const refresh = async () => (await catalog.refetch()).isSuccess;
  const closeEditor = () => {
    setCreating(false);
    detail.close();
  };

  useConsoleContext({ page: "代理", list: { total: items?.length ?? 0, items: (items ?? []).slice(0, 20).map((exit) => ({ type: "exit", id: exit.id, name: exitTitle(exit) })) } });

  return <div className="stack">
    <PageHeader size="data" title="代理" description={<span className="weir-sum"><span><b>{items?.length ?? "未知"}</b> 条代理记录</span><SyncLine /></span>} actions={<Button data-tour="add-proxy" onClick={() => setCreating(true)}><Plus aria-hidden />添加代理</Button>} />
    <div className="stack" data-tour="proxy-library">
      {catalog.isError && <State error={catalog.error} onRetry={refresh} />}
      {!catalog.data ? !catalog.isError && <State loading /> : !owners.size ? <State empty="还没有代理" /> :
        [...owners.entries()].sort(([left], [right]) => (left ?? 0) - (right ?? 0)).map(([owner, groups]) => (
          <InsetGroup key={owner ?? "system"} title={owner == null ? "系统共享" : <ObjectLink to={{ type: "user", id: owner, name: `用户 #${owner}` }} />}>
            <InsetRows as="ul">
              {groups.flatMap((group) => group.deployments.map((exit) => (
                <ProxyRow key={exit.id} exit={exit} conflict={group.conflict ? group.reason : undefined} refresh={refresh} onEdit={() => detail.open(exit)} onProbe={() => setProbe(exit)} />
              )))}
            </InsetRows>
          </InsetGroup>
        ))}
    </div>
    <PendingExits all />
    {edit && <ExitEditor key={edit === "new" ? "new" : edit.id} value={edit} nodes={nodes.data ?? []} close={closeEditor} saved={closeEditor} />}
    {probe && <ExitProbe exit={probe} base="/admin" gates={gates.data ?? []} loading={gates.isPending} error={gates.error} retry={() => void gates.refetch()} close={() => setProbe(null)} saved={() => void refresh()} />}
  </div>;
}

interface ExitDeletePreview { preview: string[]; removes: { type: "node" | "chain" | "route"; id: number; name: string }[]; blocked_by_exits: number[]; preview_token: string }

function ProxyRow({ exit, conflict, refresh, onEdit, onProbe }: { exit: ExitItem; conflict?: string; refresh: () => Promise<boolean>; onEdit: () => void; onProbe: () => void }) {
  const confirm = useConfirm();
  const toast = useToast();
  const action = useRowAction<number>({ invalidate: [["exits"], ["nodes"], ["gates"]] });
  const [preparing, setPreparing] = useState(false);
  const title = exit.remark?.trim() || exitTitle(exit);
  const referenceCount = exit.references.length;
  const uses = exit.uses ?? [];
  const busy = action.isBusy(exit.id);
  const address = exit.address && exit.port ? `${exit.address.includes(":") ? `[${exit.address.replace(/^\[|\]$/g, "")}]` : exit.address}:${exit.port}` : exit.address;
  const metadata = [exit.label, address === title ? undefined : address, exit.udp === "unsupported" ? "仅 TCP" : undefined].filter(Boolean).join(" · ");

  // 删除前向中心取级联预览：引用它的节点、链式代理与规则会一起删除，确认框逐项列出。
  async function remove() {
    if (busy || preparing) return;
    setPreparing(true);
    let plan: ExitDeletePreview;
    try {
      plan = await api(`/admin/exits/${exit.id}/delete-preview`) as ExitDeletePreview;
    } catch (error) {
      toast("无法读取删除影响", { tone: "bad", description: error instanceof Error ? error.message : String(error) });
      return;
    } finally {
      setPreparing(false);
    }
    if (plan.blocked_by_exits.length) {
      toast("暂时不能删除", { tone: "bad", description: `要一起删除的节点还被 ${plan.blocked_by_exits.length} 个代理用作出口节点，请先修改那些代理。` });
      return;
    }
    const ok = await confirm({
      title: `删除代理「${title}」？`,
      tone: "danger",
      confirmText: plan.removes.length ? "全部删除" : "删除",
      description: <div className="stack gap-[var(--weir-space-2)]">
        {plan.preview.slice(1).map((line) => <p key={line}>{line}</p>)}
        {plan.removes.length > 0 && <div className="flex flex-wrap gap-[var(--weir-space-2)]">
          {plan.removes.map((ref) => <Badge key={`${ref.type}:${ref.id}`} variant="soft" tone="neutral" className="max-w-full">{ref.name}</Badge>)}
        </div>}
        <p>删除后无法恢复。</p>
      </div>,
    });
    if (!ok) return;
    await action.run(exit.id, () => api(`/admin/exits/${exit.id}`, "DELETE", { preview_token: plan.preview_token }), "已删除", { detail: () => plan.removes.length ? `已一并删除 ${plan.removes.length} 个引用对象` : undefined });
    await refresh();
  }

  return <InsetRow as="li" {...dataObject({ type: "exit", id: exit.id })} className="grid gap-[var(--weir-space-2)]">
    <div className="flex flex-wrap items-start justify-between gap-[var(--weir-space-2)]">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-[var(--weir-space-2)]">
          <strong className="min-w-0 break-all text-body">{title}</strong>
          {!exit.enabled && <Badge variant="soft" tone="neutral">已停用</Badge>}
        </div>
        <div className="break-all text-meta text-[var(--weir-ink-3)]">{metadata}</div>
      </div>
      <ActionGroup wrap={false}>
        <Button size="icon-sm" variant="ghost" title="编辑" aria-label={`编辑 ${title}`} disabled={busy} onClick={onEdit}><Pencil aria-hidden /></Button>
        {exit.kind !== "blackhole" && <Button size="icon-sm" variant="ghost" title="实测" aria-label={`实测 ${title}`} disabled={busy} onClick={onProbe}><Activity aria-hidden /></Button>}
        <Button size="icon-sm" variant="ghost" title="删除" aria-label={`删除 ${title}`} busy={busy || preparing} busyText={preparing ? "正在读取影响" : "正在删除"} onClick={() => void remove()}><Trash2 aria-hidden /></Button>
      </ActionGroup>
    </div>
    {conflict && <div className="text-meta text-[var(--weir-warn-ink)]">{conflict}</div>}
    <div className="flex min-w-0 flex-wrap items-center gap-x-[var(--weir-space-3)] gap-y-[var(--weir-space-1)] text-meta">
      <span className="text-[var(--weir-ink-3)]">引用 VPS</span>
      {uses.length ? uses.map((use) => <ObjectLink key={use.gate_id} to={{ type: "gate", id: use.gate_id, name: use.name ?? `VPS #${use.gate_id}` }} className="max-w-full" />) :
        <span className="text-[var(--weir-ink-3)]">{referenceCount ? "仅规则或链式引用" : "未引用"}</span>}
    </div>
    {referenceCount > 0 && <Collapsible title={`被 ${referenceCount} 个对象引用 · 删除时一并删除`}>
      <div className="flex flex-wrap gap-[var(--weir-space-2)]">
        {exit.references.map((ref) => <Badge key={`${ref.type}:${ref.id}`} variant="soft" tone="neutral" className="max-w-full">
          <ObjectLink to={{ type: ref.type, id: ref.id, name: ref.name.split("\n")[0], owner: ref.owner }} className="max-w-full" />
        </Badge>)}
      </div>
    </Collapsible>}
  </InsetRow>;
}

export function RulesPage() {
  const [params, setParams] = useSearchParams();
  const gateId = Number(params.get("gate")) || undefined;
  const exits = useExitCatalog(true, gateId);
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const visible = (gates.data ?? []).filter((gate) => !gateId || gate.id === gateId);
  const routeQueries = useQueries({ queries: visible.map((gate) => ({ queryKey: ["routes", gate.id], queryFn: async () => (await api(`/admin/gates/${gate.id}/routes`)).items as RouteItem[] })) });
  const routes = routeQueries.flatMap((query) => query.data ?? []);
  const routesKnown = gates.isSuccess && routeQueries.every((query) => query.isSuccess);
  useConsoleContext({ page: "规则", list: { total: routes.length, items: routes.slice(0, 20).map((route) => ({ type: "route", id: route.id, name: route.domain || route.ip })) } });

  return <div className="stack">
    <PageHeader size="data" title="规则" description={<span className="weir-sum"><span><b>{routesKnown ? routes.length : "未知"}</b> 条分流规则</span></span>} actions={
      <Select value={gateId ? String(gateId) : "all"} onValueChange={(value) => setParams(value === "all" ? {} : { gate: String(value) })}>
        <SelectTrigger aria-label="入口 VPS" className="min-w-[161px]"><SelectValue>{gates.data?.find((gate) => gate.id === gateId)?.name ?? "全部 VPS"}</SelectValue></SelectTrigger>
        <SelectContent><SelectItem value="all">全部 VPS</SelectItem>{gates.data?.map((gate) => <SelectItem key={gate.id} value={String(gate.id)}>{gate.name}</SelectItem>)}</SelectContent>
      </Select>
    } />
    {gates.isPending || gates.error ? <State loading={gates.isPending && !gates.error} error={gates.error} onRetry={() => void gates.refetch()} /> : routes.length > 0 &&
      <ExitLanes routes={routes} exits={exits.data?.items ?? []} known={routesKnown} failed={routeQueries.some((query) => query.isError)} onRetry={() => routeQueries.forEach((query) => void query.refetch())} />}
    {visible.map((gate) => <Routes key={gate.id} gate={gate} exits={exits.data?.items.filter((exit) => exit.owner_user_id == null && (exit.gate_id == null || exit.gate_id === gate.id)) ?? []} />)}
    <DomainCandidates gateId={gateId} />
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
  const save=useFormAction(async track=>{if(old?.references.length && !await confirm({title:"确认修改共享代理？",description:"以下对象都会受影响："+old.references.map(r=>r.name).join("、"),confirmText:"保存并影响全部引用"}))return false;return api(old?`/admin/exits/${old.id}`:"/admin/exits",old?"PATCH":"POST",{...spec,enabled},track);},{onDetach:close,fields:{...exitErrorFields(spec.kind),invalid_remark:"remark",exit_gate_rejected:"kind"},invalidate:[["exits"]],onSuccess:r=>{if(r&&typeof r==="object"&&"applications" in r)setApplications((r as ExitItem).applications);if(r!==false && !(r as ExitItem|undefined)?.applications?.some(item=>item.status!=="applied"))saved();}});
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
