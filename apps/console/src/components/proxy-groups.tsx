import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Button, CheckboxField, Collapsible, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Field, Form, Input, InsetGroup, InsetRow, InsetRows, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SortableList, Switch, useConfirm } from "@weir/ui";
import { api, ApiError } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { useUnsavedChanges } from "../lib/dirty";
import { proxyURL } from "../lib/proxy-targets";
import type { ProxyGroup, ProxyGroupView, ProxyLine, ProxyView } from "../lib/core";
import { dataObject, ObjectLink, useDialogParam, useStringDialogParam } from "./object-link";
import { State } from "./feedback";

const ways = { "url-test": "延迟最低", fallback: "主备切换", select: "手动选择" };
const gateText = { online: "在线", offline: "离线", unknown: "未知" };
const healthText = { ok: "可用", down: "不可用", unknown: "未知" };

export function ProxyGroups({ base, view, onUpdated, admin }: { base: string; view: ProxyView; onUpdated: (view: ProxyView) => void; admin: boolean }) {
  const edit = useDialogParam("proxy_group", view.groups.map((g) => g.group), true);
  const create = useStringDialogParam("new_group");
  const confirm = useConfirm();
  const client = useQueryClient();
  const action = useRowAction<string>();
  const root = view.layout.find((g) => g.ref === "proxy");
  const hidden = new Set(view.policy.hidden_lines ?? []);
  const top = new Set(root?.member_refs ?? []);
  const grouped = new Set(view.groups.flatMap((g) => g.group.members));
  const omitted = view.lines.filter((l) => !top.has(l.ref) && !grouped.has(l.ref));
  const emptyGroups = view.groups.filter((g) => !g.valid_members);
  const byRef = new Map(view.lines.map((line) => [line.ref, line]));
  const apply = async (request: Promise<ProxyView>) => {
    try {
      const updated = await request;
      onUpdated(updated);
      return updated;
    } catch (error) {
      if (error instanceof ApiError && error.code === "proxy_revision_conflict") void client.invalidateQueries({ queryKey: ["proxy", base] });
      throw error;
    }
  };
  const toggle = (ref: string, on: boolean) => action.run(ref, () => apply(api(proxyURL(base), "PUT", { ...view.policy, hidden_lines: on ? [...hidden].filter((x) => x !== ref) : [...hidden, ref] }) as Promise<ProxyView>));
  const toggleMember = (group: ProxyGroup, ref: string, on: boolean) => {
    const members = group.members.filter((member) => member !== ref && byRef.get(member)?.available);
    if (on) members.push(ref);
    return action.run("member:" + group.id + ":" + ref, () => apply(api(proxyURL(base, "/groups/" + group.id), "PUT", { revision: view.policy.revision, name: group.name, kind: group.kind, members, interval_seconds: group.interval_seconds, tolerance_ms: group.tolerance_ms }) as Promise<ProxyView>));
  };
  useConsoleContext({ list: { total: root?.members?.length ?? 0, items: view.groups.map(({ group: g }) => ({ type: "proxy_group", key: `${view.user_id}:${g.id}`, name: g.name, owner: view.user_id })) } });
  const groupActions = (g: ProxyGroup) => <span className="flex gap-1">
    <Button size="icon-sm" variant="ghost" aria-label={`编辑${g.name}`} onClick={() => edit.open(g)}><Pencil /></Button>
    <Button size="icon-sm" variant="destructive" aria-label={`删除${g.name}`} disabled={action.isBusy()} busy={action.isBusy(`group:${g.id}`)} busyText="正在删除" onClick={async () => {
      if (await confirm({ title: `删除「${g.name}」？`, description: "成员恢复各自原有的单独显示偏好。", confirmText: "删除", tone: "danger" })) await action.run(`group:${g.id}`, () => apply(api(proxyURL(base, "/groups/" + g.id), "DELETE", { revision: view.policy.revision }) as Promise<ProxyView>));
    }}><Trash2 /></Button>
  </span>;
  return <>
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <InsetGroup title="客户端 PROXY" description="按客户端的实际顺序展示。自动选择只使用直连线路，固定每 60 秒探测，容差 50 毫秒。" actions={<Button size="sm" disabled={view.groups.length >= 20} onClick={() => create.open("1")}><Plus />新建分组</Button>}>
        {!root?.members?.length ? <State empty="PROXY 没有可输出成员" detail="请开启单独节点，或为分组添加有效成员。" /> : <InsetRows>
          {root.member_refs.map((ref, index) => {
            const group = view.groups.find((g) => `group:${g.group.id}` === ref);
            const line = view.lines.find((l) => l.ref === ref);
            return <InsetRow key={ref} className="stack" {...(group ? dataObject({ type: "proxy_group", key: view.user_id + ":" + group.group.id, owner: view.user_id }) : line ? dataObject({ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner: view.user_id }) : {})}>
              <div className="flex items-center justify-between gap-2"><span className="min-w-0 break-words"><span className="mr-2 text-meta">{index + 1}</span>{group ? <ObjectLink to={{ type: "proxy_group", key: view.user_id + ":" + group.group.id, owner: view.user_id, name: group.group.name }} /> : line ? <ObjectLink href={admin ? undefined : "/proxy?tab=nodes&focus=" + encodeURIComponent((line.chain_id ? "chain:" : "node:") + (line.chain_id || line.node_id))} to={{ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner: view.user_id, name: line.name }} /> : "自动选择"}</span>{group ? groupActions(group.group) : line ? <Switch aria-label={line.name + " 单独显示"} checked disabled={action.isBusy()} onCheckedChange={(on) => void toggle(line.ref, on)} /> : null}</div>
              <p className="text-meta text-[var(--weir-ink-2)]">{group ? ways[group.group.kind] + " · " + group.valid_members + " 条有效成员" : line ? (line.chained ? "链式" : "直连") + " · 节点" + healthText[line.health] + " · VPS " + gateText[line.gate_status] : (view.layout.find((g) => g.ref === "auto")?.members.length ?? 0) + " 条直连线路"}</p>
              {group ? <GroupMembers group={group} lines={view.lines} busy={action.isBusy()} admin={admin} owner={view.user_id} onToggle={(member, on) => void toggleMember(group.group, member, on)} /> : ref === "auto" ? <Collapsible title="自动选择成员" defaultOpen><InsetRows>{(view.layout.find((g) => g.ref === "auto")?.member_refs ?? []).map((member) => <InsetRow key={member}><LineStatus line={byRef.get(member)} refName={member} admin={admin} owner={view.user_id} /></InsetRow>)}</InsetRows></Collapsible> : null}
            </InsetRow>;
          })}
        </InsetRows>}
      </InsetGroup>
      <InsetGroup title="未出现在 PROXY" description="仅列出真正未输出的线路和没有有效成员的分组；移出分组后恢复单独显示偏好。">
        <InsetRows>
          {emptyGroups.map((g) => <InsetRow key={g.group.id} className="stack" {...dataObject({ type: "proxy_group", key: view.user_id + ":" + g.group.id, owner: view.user_id })}><div className="flex items-center justify-between gap-2"><ObjectLink to={{ type: "proxy_group", key: view.user_id + ":" + g.group.id, owner: view.user_id, name: g.group.name }} />{groupActions(g.group)}</div><Notice tone="warn">没有可输出成员：{g.members.map((m) => m.reason).filter(Boolean).join("；") || "成员已失效"}</Notice><GroupMembers group={g} lines={view.lines} busy={action.isBusy()} admin={admin} owner={view.user_id} onToggle={(member, on) => void toggleMember(g.group, member, on)} /></InsetRow>)}
          {omitted.map((l) => <InsetRow key={l.ref} className="stack" {...dataObject({ type: l.chain_id ? "chain" : "node", id: l.chain_id || l.node_id, owner: view.user_id })}><div className="flex items-center justify-between gap-2"><ObjectLink href={admin ? undefined : "/proxy?tab=nodes&focus=" + encodeURIComponent((l.chain_id ? "chain:" : "node:") + (l.chain_id || l.node_id))} to={{ type: l.chain_id ? "chain" : "node", id: l.chain_id || l.node_id, owner: view.user_id, name: l.name }} /><Switch aria-label={l.name + " 单独显示"} checked={!hidden.has(l.ref)} disabled={action.isBusy() || !l.available} onCheckedChange={(on) => void toggle(l.ref, on)} /></div><p className="text-meta text-[var(--weir-ink-2)]">{!l.available ? l.why || "线路不可输出" : "已关闭单独显示"}</p></InsetRow>)}
        </InsetRows>
        {!omitted.length && !emptyGroups.length && <State empty="没有未输出项目" />}
      </InsetGroup>
    </div>
    {(edit.current || create.value) && <GroupDialog key={`${base}:${edit.current?.id ?? "new"}`} base={base} view={view} value={edit.current} close={() => { if (edit.current) edit.close(); else create.close(); }} saved={(updated) => { onUpdated(updated); if (edit.current) edit.close(); else create.close(); }} />}
  </>;
}

function LineStatus({ line, refName, admin, owner }: { line?: ProxyLine; refName: string; admin?: boolean; owner?: number }) {
  if (!line) return <p className="text-meta text-[var(--weir-ink-2)]">线路已删除或取消授权 · {refName}</p>;
  return <p className="text-meta text-[var(--weir-ink-2)]"><ObjectLink href={admin ? undefined : "/proxy?tab=nodes&focus=" + encodeURIComponent(line.ref)} to={{ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner, name: line.name }} /> · {admin ? <ObjectLink to={{ type: "gate", id: line.gate_id, name: line.gate_name }} /> : line.gate_name} · {line.chained ? "链式" : "直连"} · 节点{healthText[line.health]} · VPS {gateText[line.gate_status]}{line.why ? " · " + line.why : ""}</p>;
}

function GroupMembers({ group, lines, busy, onToggle, admin, owner }: { group: ProxyGroupView; lines: ProxyLine[]; busy: boolean; onToggle: (ref: string, on: boolean) => void; admin?: boolean; owner?: number }) {
  const selected = new Set(group.group.members);
  const byRef = new Map(lines.map((line) => [line.ref, line]));
  const valid = group.group.members.filter((ref) => byRef.get(ref)?.available).length;
  const choices = lines.filter((line) => line.available || selected.has(line.ref));
  const gates = [...new Set(choices.map((line) => line.gate_id))];
  return <Collapsible title={"成员与可选节点（" + group.group.members.length + "）"} defaultOpen>
    <InsetRows>
      {group.group.members.filter((ref) => !byRef.has(ref)).map((ref) => <InsetRow key={ref} className="stack"><CheckboxField label="失效线路" checked disabled={busy || valid === 0} description="添加可用节点后可移除" onCheckedChange={() => onToggle(ref, false)} /><LineStatus refName={ref} /></InsetRow>)}
    </InsetRows>
    {group.group.members.some((ref) => !byRef.get(ref)?.available) && <Notice tone="warn">下次修改成员时将清理失效线路。</Notice>}
    {gates.map((id) => <InsetGroup key={id} title={lines.find((line) => line.gate_id === id)?.gate_name ?? "VPS 未知"} description="勾选后立即保存；按 VPS 固定排列，主备顺序可在编辑中调整。"><InsetRows>{choices.filter((line) => line.gate_id === id).map((line) => <InsetRow key={line.ref} className="stack" {...dataObject({ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner })}><CheckboxField label={line.name} checked={selected.has(line.ref)} disabled={busy || (selected.has(line.ref) ? valid <= 1 && line.available || valid === 0 : valid >= 50)} description={!line.available ? "不可输出；添加可用节点后可移除" : selected.has(line.ref) ? "已在分组 · " + (group.group.members.indexOf(line.ref) + 1) + " · 取消勾选即移出" : "可添加 · " + (line.chained ? "链式" : "直连")} onCheckedChange={(on) => onToggle(line.ref, on)} /><LineStatus line={line} refName={line.ref} admin={admin} owner={owner} /></InsetRow>)}</InsetRows></InsetGroup>)}
    {!choices.some((line) => !selected.has(line.ref)) && <p className="text-meta text-[var(--weir-ink-2)]">没有其他可添加的线路。</p>}
  </Collapsible>;
}

function GroupDialog({ base, view, value, close, saved }: { base: string; view: ProxyView; value: ProxyGroup | null; close: () => void; saved: (updated: ProxyView) => void }) {
  const client = useQueryClient();
  const [revision] = useState(view.policy.revision);
  const [name, setName] = useState(value?.name ?? "");
  const [kind, setKind] = useState<ProxyGroup["kind"]>(value?.kind ?? "url-test");
  const [members, setMembers] = useState(value?.members ?? []);
  const membersControl = useRef<HTMLDivElement>(null);
  const [interval, setInterval] = useState(String(value?.interval_seconds ?? 60));
  const [tolerance, setTolerance] = useState(String(value?.tolerance_ms ?? 50));
  const [memberError, setMemberError] = useState("");
  const [conflicted, setConflicted] = useState(false);
  const confirm = useConfirm();
  const dirty = name !== (value?.name ?? "") || kind !== (value?.kind ?? "url-test") || JSON.stringify(members) !== JSON.stringify(value?.members ?? []) || interval !== String(value?.interval_seconds ?? 60) || tolerance !== String(value?.tolerance_ms ?? 50);
  const markSaved = useUnsavedChanges(dirty);
  const save = useFormAction<ProxyView>(() => api(proxyURL(base, `/groups${value ? `/${value.id}` : ""}`), value ? "PUT" : "POST", { revision, name, kind, members, interval_seconds: Number(interval), tolerance_ms: Number(tolerance) }) as Promise<ProxyView>, {
    fields: { invalid_group_name: "name", invalid_group_members: "members", invalid_group_kind: "kind", invalid_group_interval_seconds: "interval_seconds", invalid_group_tolerance_ms: "tolerance_ms" }, onSuccess: (updated) => { markSaved(); saved(updated); },
    onError: (error) => { if (error instanceof ApiError && error.code === "proxy_revision_conflict") { setConflicted(true); void client.invalidateQueries({ queryKey: ["proxy", base] }); } },
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
      <DialogFooter error={conflicted ? "代理设置已有新版本，请关闭弹窗后重新编辑。" : save.error}><Button type="button" variant="outline" disabled={save.busy} onClick={() => void requestClose()}>取消</Button><Button type="submit" disabled={conflicted} busy={save.busy} busyText="正在保存">保存</Button></DialogFooter>
    </Form>
  </DialogContent></Dialog>;
}

export function ProxyMigration({ base, view, onSaved }: { base: string; view: ProxyView; onSaved: () => unknown }) {
  const action = useRowAction({ refetch: onSaved });
  if (!view.policy.target_migration?.length) return null;
  return <Notice tone="warn">{view.policy.target_migration.join("；")}。<Button size="sm" variant="outline" busy={action.isBusy()} busyText="正在确认" onClick={() => action.run("ack", () => api(proxyURL(base, "/migration/ack"), "POST", { revision: view.policy.revision }))}>知道了</Button></Notice>;
}
