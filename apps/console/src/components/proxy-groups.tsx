import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { ActionGroup, Badge, Button, CheckboxField, ChoiceCard, Collapsible, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Field, Form, FormGrid, Input, InsetGroup, InsetRow, InsetRows, Notice, Radio, RadioGroup, SortableList, Switch, useConfirm } from "@weir/ui";
import { api, ApiError } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { useUnsavedChanges } from "../lib/dirty";
import { proxyURL } from "../lib/proxy-targets";
import type { ProxyGroup, ProxyView } from "../lib/core";
import { dataObject, ObjectLink, useDialogParam, useStringDialogParam } from "./object-link";
import { State } from "./feedback";

const ways = { "url-test": "自动测速", fallback: "主备容灾", select: "手动切换" };
const wayHints: Record<ProxyGroup["kind"], string> = {
  "url-test": "客户端自动选延迟最低节点",
  fallback: "按顺序优先首节点，故障才切换",
  select: "在客户端面板里手动点选",
};
const quickNames = ["AI 专线", "流媒体", "主力节点", "备用线路"];
const gateText = { online: "在线", offline: "离线", unknown: "未知" };
const healthText = { ok: "可用", down: "不可用", unknown: "未知" };

export function ProxyGroups({ base, view, onUpdated, admin }: { base: string; view: ProxyView; onUpdated: (view: ProxyView) => void; admin: boolean }) {
  const edit = useDialogParam("proxy_group", view.groups.map((g) => g.group), true);
  const create = useStringDialogParam("new_group");
  const confirm = useConfirm();
  const client = useQueryClient();
  const action = useRowAction<string>();
  const root = view.layout.find((g) => g.ref === "proxy");
  const autoGroup = view.layout.find((g) => g.ref === "auto");
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
  useConsoleContext({ list: { total: root?.members?.length ?? 0, items: view.groups.map(({ group: g }) => ({ type: "proxy_group", key: `${view.user_id}:${g.id}`, name: g.name, owner: view.user_id })) } });
  const groupActions = (g: ProxyGroup) => <ActionGroup wrap={false}>
    <Button size="sm" variant="outline" aria-label={`编辑${g.name}`} onClick={() => edit.open(g)}><Pencil />配置成员</Button>
    <Button size="icon-sm" variant="ghost" aria-label={`删除${g.name}`} disabled={action.isBusy()} busy={action.isBusy(`group:${g.id}`)} onClick={async () => {
      if (await confirm({ title: `删除分组「${g.name}」？`, description: "组内节点恢复各自原有的单独显示状态。", confirmText: "删除", tone: "danger" })) await action.run(`group:${g.id}`, () => apply(api(proxyURL(base, "/groups/" + g.id), "DELETE", { revision: view.policy.revision }) as Promise<ProxyView>));
    }}><Trash2 /></Button>
  </ActionGroup>;
  return <>
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
      <InsetGroup title="客户端 PROXY 选项" description="决定客户端主代理组里出现哪些分组与单节点。" actions={<Button size="sm" disabled={view.groups.length >= 20} onClick={() => create.open("1")}><Plus />新建分组</Button>}>
        {!root?.members?.length ? <State empty="PROXY 没有可输出成员" detail="请开启单独节点，或新建包含有效节点的自定义分组。" /> : <InsetRows>
          {root.member_refs.map((ref, index) => {
            const group = view.groups.find((g) => `group:${g.group.id}` === ref);
            const line = view.lines.find((l) => l.ref === ref);
            if (group) {
              const memberNames = group.group.members.map((m) => byRef.get(m)?.name ?? "失效线路");
              return (
                <InsetRow key={ref} className="space-y-1.5" {...dataObject({ type: "proxy_group", key: view.user_id + ":" + group.group.id, owner: view.user_id })}>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="font-mono text-meta text-[var(--weir-ink-3)]">{index + 1}</span>
                      <ObjectLink className="font-semibold" to={{ type: "proxy_group", key: view.user_id + ":" + group.group.id, owner: view.user_id, name: group.group.name }} />
                      <Badge variant="soft" tone="info">{ways[group.group.kind]} · {group.valid_members} 节点</Badge>
                    </span>
                    {groupActions(group.group)}
                  </div>
                  <p className="m-0 truncate text-meta text-[var(--weir-ink-3)]" title={memberNames.join("、")}>成员：{memberNames.join("、")}</p>
                </InsetRow>
              );
            }
            if (line) {
              return (
                <InsetRow key={ref} className="flex items-center justify-between gap-3" {...dataObject({ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner: view.user_id })}>
                  <div className="min-w-0 space-y-0.5">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-meta text-[var(--weir-ink-3)]">{index + 1}</span>
                      <ObjectLink href={admin ? undefined : "/proxy?tab=nodes&focus=" + encodeURIComponent((line.chain_id ? "chain:" : "node:") + (line.chain_id || line.node_id))} to={{ type: line.chain_id ? "chain" : "node", id: line.chain_id || line.node_id, owner: view.user_id, name: line.name }} />
                      {line.chained && <Badge variant="soft" tone="info">链式</Badge>}
                    </div>
                    <small className="block text-[var(--weir-ink-3)]">{line.gate_name} · 节点{healthText[line.health]} · VPS {gateText[line.gate_status]}</small>
                  </div>
                  <label className="inline-flex min-h-[var(--weir-tap-touch)] shrink-0 cursor-pointer items-center gap-2 text-meta text-[var(--weir-ink-3)]">
                    <span className="max-sm:sr-only">单独显示</span>
                    <Switch aria-label={line.name + " 单独显示"} checked disabled={action.isBusy()} onCheckedChange={(on) => void toggle(line.ref, on)} />
                  </label>
                </InsetRow>
              );
            }
            return (
              <InsetRow key={ref} className="space-y-1.5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="flex items-center gap-2">
                    <span className="font-mono text-meta text-[var(--weir-ink-3)]">{index + 1}</span>
                    <strong>自动选择</strong>
                    <Badge variant="soft" tone="neutral">自动测速 · {autoGroup?.members.length ?? 0} 条直连</Badge>
                  </span>
                </div>
                {(autoGroup?.member_refs?.length ?? 0) > 0 && (
                  <Collapsible title={`查看自动测速池（${autoGroup?.member_refs.length}）`}>
                    <div className="flex flex-wrap gap-1.5 pt-1">
                      {(autoGroup?.member_refs ?? []).map((member) => {
                        const m = byRef.get(member);
                        return <Badge key={member} variant="outline" tone="neutral">{m ? `${m.name}（${m.gate_name}）` : member}</Badge>;
                      })}
                    </div>
                  </Collapsible>
                )}
              </InsetRow>
            );
          })}
        </InsetRows>}
      </InsetGroup>
      <InsetGroup title="未单独显示与待补齐项" description="隐藏的单节点仍可加入自定义分组或参与自动选择。">
        {!omitted.length && !emptyGroups.length ? (
          <p className="m-0 text-meta text-[var(--weir-ink-3)]">当前没有隐藏节点或失效分组。若觉得客户端节点列表太长，可在左侧关闭单节点的「单独显示」，只保留分组入口。</p>
        ) : (
          <InsetRows>
            {emptyGroups.map((g) => <InsetRow key={g.group.id} className="space-y-2" {...dataObject({ type: "proxy_group", key: view.user_id + ":" + g.group.id, owner: view.user_id })}><div className="flex items-center justify-between gap-2"><ObjectLink to={{ type: "proxy_group", key: view.user_id + ":" + g.group.id, owner: view.user_id, name: g.group.name }} />{groupActions(g.group)}</div><Notice tone="warn">暂无可用成员：{g.members.map((m) => m.reason).filter(Boolean).join("；") || "原成员已失效"}</Notice></InsetRow>)}
            {omitted.map((l) => <InsetRow key={l.ref} className="flex items-center justify-between gap-3" {...dataObject({ type: l.chain_id ? "chain" : "node", id: l.chain_id || l.node_id, owner: view.user_id })}><div className="min-w-0 space-y-0.5"><ObjectLink href={admin ? undefined : "/proxy?tab=nodes&focus=" + encodeURIComponent((l.chain_id ? "chain:" : "node:") + (l.chain_id || l.node_id))} to={{ type: l.chain_id ? "chain" : "node", id: l.chain_id || l.node_id, owner: view.user_id, name: l.name }} /><small className="block text-[var(--weir-ink-3)]">{!l.available ? l.why || "线路不可输出" : `${l.gate_name} · 已隐藏单独显示`}</small></div><label className="inline-flex min-h-[var(--weir-tap-touch)] shrink-0 cursor-pointer items-center"><Switch aria-label={l.name + " 单独显示"} checked={!hidden.has(l.ref)} disabled={action.isBusy() || !l.available} onCheckedChange={(on) => void toggle(l.ref, on)} /></label></InsetRow>)}
          </InsetRows>
        )}
      </InsetGroup>
    </div>
    {(edit.current || create.value) && <GroupDialog key={`${base}:${edit.current?.id ?? "new"}`} base={base} view={view} value={edit.current} close={() => { if (edit.current) edit.close(); else create.close(); }} saved={(updated) => { onUpdated(updated); if (edit.current) edit.close(); else create.close(); }} />}
  </>;
}

function GroupDialog({ base, view, value, close, saved }: { base: string; view: ProxyView; value: ProxyGroup | null; close: () => void; saved: (updated: ProxyView) => void }) {
  const client = useQueryClient();
  const [revision] = useState(view.policy.revision);
  const [name, setName] = useState(value?.name ?? "");
  const [kind, setKind] = useState<ProxyGroup["kind"]>(value?.kind ?? "url-test");
  const [members, setMembers] = useState(value?.members ?? []);
  const membersControl = useRef<HTMLDivElement>(null);
  const [interval, setInterval] = useState(String(value?.interval_seconds ?? 30));
  const [tolerance, setTolerance] = useState(String(value?.tolerance_ms ?? 50));
  const [memberError, setMemberError] = useState("");
  const [conflicted, setConflicted] = useState(false);
  const confirm = useConfirm();
  const dirty = name !== (value?.name ?? "") || kind !== (value?.kind ?? "url-test") || JSON.stringify(members) !== JSON.stringify(value?.members ?? []) || interval !== String(value?.interval_seconds ?? 30) || tolerance !== String(value?.tolerance_ms ?? 50);
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
    <DialogTitle>{value ? `配置分组「${value.name}」` : "新建客户端分组"}</DialogTitle><DialogDescription>把同一地区或用途的节点编成一组，出现在客户端的 PROXY 选择列表里。</DialogDescription>
    <Form busy={save.busy} errors={{ ...save.errors, ...(memberError ? { members: memberError } : {}) }} onSubmit={() => { if (!members.length) { setMemberError("请至少勾选一条节点线路"); membersControl.current?.focus(); return; } void save.submit(); }}>
      <div className="space-y-2">
        <Field name="name" label="分组名称" required><Input name="name" required maxLength={80} placeholder="例如：AI 专线、流媒体、日本组" value={name} onChange={(e) => setName(e.target.value)} /></Field>
        {!value && (
          <div className="flex flex-wrap items-center gap-1.5">
            <small className="text-[var(--weir-ink-3)]">常用名称：</small>
            {quickNames.filter((q) => !view.groups.some((g) => g.group.name === q)).map((q) => (
              <Button key={q} type="button" size="xs" variant="outline" onClick={() => setName(q)}>{q}</Button>
            ))}
          </div>
        )}
      </div>
      <Field name="kind" label="组内选路方式">
        <RadioGroup className="grid gap-[var(--weir-space-2)] sm:grid-cols-3" value={kind} onValueChange={(v) => setKind(v as ProxyGroup["kind"])} aria-label="组内选路方式">
          {(["url-test", "fallback", "select"] as const).map((k) => (
            <ChoiceCard key={k} render={<label />} selected={kind === k} className="cursor-pointer p-3">
              <span className="flex items-center gap-2"><Radio value={k} /><strong className="text-body">{ways[k]}</strong></span>
              <small className="block text-[var(--weir-ink-3)]">{wayHints[k]}</small>
            </ChoiceCard>
          ))}
        </RadioGroup>
      </Field>
      <Field name="members" label={`勾选节点（已选 ${members.length}）`} required><div ref={membersControl} className="stack" tabIndex={-1}>
        {gates.map((id) => {
          const gateLines = view.lines.filter((l) => l.gate_id === id);
          const availableRefs = gateLines.filter((l) => l.available).map((l) => l.ref);
          const allPicked = availableRefs.length > 0 && availableRefs.every((r) => members.includes(r));
          return (
            <InsetGroup
              key={id}
              title={gateLines[0]?.gate_name ?? "VPS"}
              actions={availableRefs.length > 1 ? (
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => {
                    setMemberError("");
                    if (allPicked) setMembers(members.filter((m) => !availableRefs.includes(m)));
                    else setMembers([...new Set([...members, ...availableRefs])].slice(0, 50));
                  }}
                >
                  {allPicked ? "取消本机全选" : "全选本机"}
                </Button>
              ) : undefined}
            >
              <InsetRows>
                {gateLines.map((l) => (
                  <InsetRow key={l.ref} className="py-2">
                    <CheckboxField
                      label={l.name}
                      description={`${l.chained ? "链式" : "直连"} · 节点${healthText[l.health]} · VPS ${gateText[l.gate_status]}${l.why ? ` · ${l.why}` : ""}`}
                      checked={members.includes(l.ref)}
                      disabled={!l.available || (!members.includes(l.ref) && members.length >= 50)}
                      onCheckedChange={(on) => { setMembers(on ? [...members, l.ref] : members.filter((m) => m !== l.ref)); setMemberError(""); }}
                    />
                  </InsetRow>
                ))}
              </InsetRows>
            </InsetGroup>
          );
        })}
        {members.filter((m) => !byRef.has(m)).map((m) => <CheckboxField key={m} label="已失效线路，取消勾选以移除" checked onCheckedChange={() => setMembers(members.filter((ref) => ref !== m))} />)}
        {!gates.length && <State empty="没有可选择的节点" />}
      </div></Field>
      {kind === "fallback" && members.length > 1 && <InsetGroup title="主备优先顺序" description="靠前的节点优先使用，断连时自动切到下一条。"><SortableList items={members} getId={(m) => m} label="主备顺序" itemLabel={(m) => byRef.get(m)?.name ?? "失效线路"} onReorder={setMembers} renderItem={(m, { reorder, index }) => <div className="flex min-w-0 items-center gap-2"><span className="font-mono text-meta text-[var(--weir-ink-3)]">{index + 1}</span><span className="min-w-0 flex-1 break-words">{byRef.get(m)?.name ?? "失效线路"}</span>{reorder}</div>} /></InsetGroup>}
      {kind !== "select" && (
        <Collapsible title={`探测参数（当前 ${interval} 秒${kind === "url-test" ? ` · 容差 ${tolerance} ms` : ""}）`}>
          <FormGrid>
            <Field name="interval_seconds" label="探测间隔（秒）" required><Input type="number" min={30} max={86400} step={1} required value={interval} onChange={(e) => setInterval(e.target.value)} /></Field>
            {kind === "url-test" && <Field name="tolerance_ms" label="延迟容差（毫秒）" required><Input type="number" min={0} max={10000} step={1} required value={tolerance} onChange={(e) => setTolerance(e.target.value)} /></Field>}
          </FormGrid>
        </Collapsible>
      )}
      <DialogFooter error={conflicted ? "分流配置已有新版本，请关闭弹窗后重试。" : save.error}><Button type="button" variant="outline" disabled={save.busy} onClick={() => void requestClose()}>取消</Button><Button type="submit" disabled={conflicted} busy={save.busy} busyText="正在保存">{value ? "保存修改" : "创建分组"}</Button></DialogFooter>
    </Form>
  </DialogContent></Dialog>;
}

export function ProxyMigration({ base, view, onSaved }: { base: string; view: ProxyView; onSaved: () => unknown }) {
  const action = useRowAction({ refetch: onSaved });
  if (!view.policy.target_migration?.length) return null;
  return <Notice tone="warn">{view.policy.target_migration.join("；")}。<Button size="sm" variant="outline" busy={action.isBusy()} busyText="正在确认" onClick={() => action.run("ack", () => api(proxyURL(base, "/migration/ack"), "POST", { revision: view.policy.revision }))}>知道了</Button></Notice>;
}
