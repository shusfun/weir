import { useRef, useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Button, CheckboxField, Collapsible, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Field, Form, Input, InsetGroup, InsetRow, InsetRows, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SortableList, Switch, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { useUnsavedChanges } from "../lib/dirty";
import { proxyURL } from "../lib/proxy-targets";
import type { ProxyGroup, ProxyView } from "../lib/core";
import { dataObject, ObjectLink, useDialogParam, useStringDialogParam } from "./object-link";
import { State } from "./feedback";

const ways = { "url-test": "延迟最低", fallback: "主备切换", select: "手动选择" };
const gateText = { online: "在线", offline: "离线", unknown: "未知" };
const healthText = { ok: "可用", down: "不可用", unknown: "未知" };

export function ProxyGroups({ base, view, onSaved, admin }: { base: string; view: ProxyView; onSaved: () => unknown; admin: boolean }) {
  const edit = useDialogParam("proxy_group", view.groups.map((g) => g.group), true);
  const create = useStringDialogParam("new_group");
  const confirm = useConfirm();
  const action = useRowAction<string>({ refetch: onSaved });
  const root = view.layout.find((g) => g.ref === "proxy");
  const hidden = new Set(view.policy.hidden_lines ?? []);
  const top = new Set(root?.member_refs ?? []);
  const grouped = new Set(view.groups.filter((g) => g.valid_members > 0).flatMap((g) => g.group.members));
  const toggle = (ref: string, on: boolean) => action.run(ref, () => api(proxyURL(base), "PUT", { ...view.policy, hidden_lines: on ? [...hidden].filter((x) => x !== ref) : [...hidden, ref] }));
  useConsoleContext({ list: { total: root?.members?.length ?? 0, items: view.groups.map(({ group: g }) => ({ type: "proxy_group", key: `${view.user_id}:${g.id}`, name: g.name, owner: view.user_id })) } });
  const groupActions = (g: ProxyGroup) => <span className="flex gap-1">
    <Button size="icon-sm" variant="ghost" aria-label={`编辑${g.name}`} onClick={() => edit.open(g)}><Pencil /></Button>
    <Button size="icon-sm" variant="destructive" aria-label={`删除${g.name}`} disabled={action.isBusy()} busy={action.isBusy(`group:${g.id}`)} busyText="正在删除" onClick={async () => {
      if (await confirm({ title: `删除「${g.name}」？`, description: "成员恢复各自原有的单独显示偏好。", confirmText: "删除", tone: "danger" })) await action.run(`group:${g.id}`, () => api(proxyURL(base, `/groups/${g.id}`), "DELETE", { revision: view.policy.revision }));
    }}><Trash2 /></Button>
  </span>;
  return <>
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <InsetGroup title="客户端 PROXY" description="按客户端的实际顺序展示。自动选择只使用直连线路，固定每 60 秒探测，容差 50 毫秒。" actions={<Button size="sm" disabled={view.groups.length >= 20} onClick={() => create.open("1")}><Plus />新建分组</Button>}>
        {!root?.members?.length ? <State empty="PROXY 没有可输出成员" detail="请开启单独节点，或为分组添加有效成员。" /> : <InsetRows>
          {root.member_refs.map((ref, index) => {
            const group = view.groups.find((g) => `group:${g.group.id}` === ref);
            const line = view.lines.find((l) => l.ref === ref);
            return <InsetRow key={ref} className="stack" {...(group ? dataObject({ type: "proxy_group", key: `${view.user_id}:${group.group.id}`, owner: view.user_id }) : line ? dataObject({ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner: view.user_id }) : {})}>
              <div className="flex items-center justify-between gap-2"><span className="min-w-0 break-words"><span className="mr-2 text-meta">{index + 1}</span>{group ? <ObjectLink to={{ type: "proxy_group", key: `${view.user_id}:${group.group.id}`, owner: view.user_id, name: group.group.name }} /> : line ? <ObjectLink href={admin ? undefined : `/proxy?tab=nodes&focus=${encodeURIComponent(`${line.chain_id ? "chain" : "node"}:${line.chain_id || line.node_id}`)}`} to={{ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner: view.user_id, name: line.name }} /> : "自动选择"}</span>{group ? groupActions(group.group) : line ? <Switch aria-label={`${line.name} 单独显示`} checked disabled={action.isBusy()} onCheckedChange={(on) => void toggle(line.ref, on)} /> : null}</div>
              <p className="text-meta text-[var(--weir-ink-2)]">{group ? `${ways[group.group.kind]} · ${group.valid_members} 条有效成员` : line ? `${line.chained ? "链式" : "直连"} · 节点${healthText[line.health]} · VPS ${gateText[line.gate_status]}` : `${view.layout.find((g) => g.ref === "auto")?.members.length ?? 0} 条直连线路`}</p>
            </InsetRow>;
          })}
        </InsetRows>}
      </InsetGroup>
      <InsetGroup title="未出现在 PROXY" description="分组中的节点不再单独显示；移出分组后恢复原有偏好。">
        <InsetRows>
          {view.groups.filter((g) => !g.valid_members).map((g) => <InsetRow key={g.group.id} className="stack" {...dataObject({ type: "proxy_group", key: `${view.user_id}:${g.group.id}`, owner: view.user_id })}><div className="flex items-center justify-between gap-2"><ObjectLink to={{ type: "proxy_group", key: `${view.user_id}:${g.group.id}`, owner: view.user_id, name: g.group.name }} />{groupActions(g.group)}</div><Notice tone="warn">没有可输出成员：{g.members.map((m) => m.reason).filter(Boolean).join("；") || "成员已失效"}</Notice></InsetRow>)}
          {view.lines.filter((l) => !top.has(l.ref)).map((l) => <InsetRow key={l.ref} className="stack" {...dataObject({ type: l.chain_id ? "chain" : "node", id: l.chain_id || l.node_id, owner: view.user_id })}><div className="flex items-center justify-between gap-2"><ObjectLink href={admin ? undefined : `/proxy?tab=nodes&focus=${encodeURIComponent(`${l.chain_id ? "chain" : "node"}:${l.chain_id || l.node_id}`)}`} to={{ type: l.chain_id ? "chain" : "node", id: l.chain_id || l.node_id, owner: view.user_id, name: l.name }} /><Switch aria-label={`${l.name} 单独显示`} checked={!hidden.has(l.ref)} disabled={action.isBusy() || !l.available || grouped.has(l.ref)} onCheckedChange={(on) => void toggle(l.ref, on)} /></div><p className="text-meta text-[var(--weir-ink-2)]">{!l.available ? l.why || "线路不可输出" : grouped.has(l.ref) ? "已在自建分组内，可编辑分组移出" : "已关闭单独显示"}</p></InsetRow>)}
        </InsetRows>
        {view.lines.every((l) => top.has(l.ref)) && view.groups.every((g) => g.valid_members > 0) && <State empty="没有未输出项目" />}
      </InsetGroup>
    </div>
    {(edit.current || create.value) && <GroupDialog key={`${base}:${edit.current?.id ?? "new"}`} base={base} view={view} value={edit.current} close={() => { if (edit.current) edit.close(); else create.close(); }} saved={async () => { await onSaved(); if (edit.current) edit.close(); else create.close(); }} />}
  </>;
}

function GroupDialog({ base, view, value, close, saved }: { base: string; view: ProxyView; value: ProxyGroup | null; close: () => void; saved: () => unknown }) {
  const [name, setName] = useState(value?.name ?? "");
  const [kind, setKind] = useState<ProxyGroup["kind"]>(value?.kind ?? "url-test");
  const [members, setMembers] = useState(value?.members ?? []);
  const membersControl = useRef<HTMLDivElement>(null);
  const [interval, setInterval] = useState(String(value?.interval_seconds ?? 60));
  const [tolerance, setTolerance] = useState(String(value?.tolerance_ms ?? 50));
  const [memberError, setMemberError] = useState("");
  const confirm = useConfirm();
  const dirty = name !== (value?.name ?? "") || kind !== (value?.kind ?? "url-test") || JSON.stringify(members) !== JSON.stringify(value?.members ?? []) || interval !== String(value?.interval_seconds ?? 60) || tolerance !== String(value?.tolerance_ms ?? 50);
  const markSaved = useUnsavedChanges(dirty);
  const save = useFormAction(() => api(proxyURL(base, `/groups${value ? `/${value.id}` : ""}`), value ? "PUT" : "POST", { revision: view.policy.revision, name, kind, members, interval_seconds: Number(interval), tolerance_ms: Number(tolerance) }), {
    fields: { invalid_group_name: "name", invalid_group_members: "members", invalid_group_kind: "kind", invalid_group_interval_seconds: "interval_seconds", invalid_group_tolerance_ms: "tolerance_ms" }, onSuccess: async () => { markSaved(); await saved(); },
  });
  const requestClose = async () => { if (!save.busy && (!dirty || await confirm({ title: "放弃未保存修改？", description: "分组尚未保存。", confirmText: "放弃修改", tone: "danger" }))) { markSaved(); close(); } };
  const gates = [...new Set(view.lines.map((l) => l.gate_id))];
  const byRef = new Map(view.lines.map((l) => [l.ref, l]));
  useConsoleContext({ dialog: { title: value ? "编辑分组" : "新建分组" }, form: [field("name", "名称", name), field("kind", "选法", ways[kind]), field("members", "成员", members.map((m) => byRef.get(m)?.name ?? "失效线路").join("、")), field("interval_seconds", "探测间隔", interval), field("tolerance_ms", "容差", tolerance)] });
  return <Dialog open onOpenChange={(o) => { if (!o) void requestClose(); }}><DialogContent size="lg">
    <DialogTitle>{value ? "编辑分组" : "新建分组"}</DialogTitle><DialogDescription>起名、勾选节点，然后保存。默认由客户端选择延迟最低的节点。</DialogDescription>
    <Form busy={save.busy} errors={{ ...save.errors, ...(memberError ? { members: memberError } : {}) }} onSubmit={() => { if (!members.length) { setMemberError("请至少选择一条线路"); membersControl.current?.focus(); return; } void save.submit(); }}>
      <Field name="name" label="名称" required><Input name="name" required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} /></Field>
      <Field name="members" label="选节点" required hint="最多 50 条。节点按 VPS 固定排列，勾选不改变位置。"><div ref={membersControl} className="stack" tabIndex={-1}>
        {gates.map((id) => <InsetGroup key={id} title={view.lines.find((l) => l.gate_id === id)?.gate_name}><InsetRows>{view.lines.filter((l) => l.gate_id === id).map((l) => <InsetRow key={l.ref} className="stack"><CheckboxField label={l.name} checked={members.includes(l.ref)} disabled={!l.available || (!members.includes(l.ref) && members.length >= 50)} onCheckedChange={(on) => { setMembers(on ? [...members, l.ref] : members.filter((m) => m !== l.ref)); setMemberError(""); }} /><p className="text-meta text-[var(--weir-ink-2)]">{l.chained ? "链式" : "直连"} · 节点{healthText[l.health]} · VPS {gateText[l.gate_status]}{l.why ? ` · ${l.why}` : ""}</p></InsetRow>)}</InsetRows></InsetGroup>)}
        {members.filter((m) => !byRef.has(m)).map((m) => <CheckboxField key={m} label="已失效线路，取消勾选以移除" checked onCheckedChange={() => setMembers(members.filter((ref) => ref !== m))} />)}
        {!gates.length && <State empty="没有可选择的节点" />}
      </div></Field>
      <Collapsible title="高级设置">
        <Field name="kind" label="选法"><Select value={kind} onValueChange={(v) => setKind(v as ProxyGroup["kind"])}><SelectTrigger aria-label="分组选法"><SelectValue>{ways[kind]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(ways).map(([k, label]) => <SelectItem key={k} value={k}>{label}</SelectItem>)}</SelectContent></Select></Field>
        <Field name="interval_seconds" label="探测间隔（秒）" required><Input type="number" min={30} max={86400} step={1} required value={interval} onChange={(e) => setInterval(e.target.value)} /></Field>
        {kind === "url-test" && <Field name="tolerance_ms" label="延迟容差（毫秒）" required><Input type="number" min={0} max={10000} step={1} required value={tolerance} onChange={(e) => setTolerance(e.target.value)} /></Field>}
      </Collapsible>
      {kind === "fallback" && <InsetGroup title="主备顺序" description="优先使用靠前的可用节点，可使用上移、下移或键盘调整。"><SortableList items={members} getId={(m) => m} label="主备顺序" itemLabel={(m) => byRef.get(m)?.name ?? "失效线路"} onReorder={setMembers} renderItem={(m, { reorder, index }) => <div className="flex min-w-0 items-center gap-2"><span>{index + 1}</span><span className="min-w-0 flex-1 break-words">{byRef.get(m)?.name ?? "失效线路"}</span>{reorder}</div>} /></InsetGroup>}
      <DialogFooter error={save.error}><Button type="button" variant="outline" disabled={save.busy} onClick={() => void requestClose()}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存">保存</Button></DialogFooter>
    </Form>
  </DialogContent></Dialog>;
}

export function ProxyMigration({ base, view, onSaved }: { base: string; view: ProxyView; onSaved: () => unknown }) {
  const action = useRowAction({ refetch: onSaved });
  if (!view.policy.target_migration?.length) return null;
  return <Notice tone="warn">{view.policy.target_migration.join("；")}。<Button size="sm" variant="outline" busy={action.isBusy()} busyText="正在确认" onClick={() => action.run("ack", () => api(proxyURL(base, "/migration/ack"), "POST", { revision: view.policy.revision }))}>知道了</Button></Notice>;
}
