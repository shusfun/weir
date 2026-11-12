import { useState } from "react";
import { Button, CheckboxField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Card, CardHeader, CardContent, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useFormAction } from "../../lib/action";
import { useConsoleContext } from "../../lib/console-context";
import { toUnits, units, type NodeItem, type Unit } from "../../lib/core";
import { emptyGrant, grantInput, GrantSummary } from "./grant";
import type { Grant, Plan } from "./types";

export function PlanForm({ value, nodes, profiles, close, saved }: { value: Plan | "new"; nodes: NodeItem[]; profiles: { id: number; name: string }[]; close: () => void; saved: () => void }) {
  const p = value === "new" ? undefined : value;
  const [name, setName] = useState(p?.name ?? "");
  const [enabled, setEnabled] = useState(p?.enabled ?? true);
  const [grant, setGrant] = useState<Grant>(p?.grant ?? emptyGrant());
  const [initialQuota, initialUnit] = toUnits(p?.grant.quota ?? 0);
  const [quota, setQuota] = useState(p?.grant.quota ? initialQuota : "");
  const [unit, setUnit] = useState<Unit>(initialUnit);
  const effectiveGrant = grantInput(grant, quota, unit);
  const confirm = useConfirm();
  const update = <K extends keyof Grant>(key: K, v: Grant[K]) => setGrant((g) => ({ ...g, [key]: v }));
  const save = useFormAction(async () => {
    if (!(await confirm({ title: "保存套餐？", description: "已签发的码继续使用原套餐快照。后续签发使用这里的新设置。", confirmText: "保存套餐" }))) return null;
    return api("/admin/account-plans" + (p ? "/" + p.id : ""), p ? "PUT" : "POST", { name, enabled, grant: effectiveGrant, revision: p?.revision });
  }, { fields: { invalid_plan_name: "name", invalid_plan_grant: "grant" }, onSuccess: (r) => { if (r) saved(); } });
  useConsoleContext({ dialog: { title: p ? "编辑套餐" : "新建套餐" }, object: p ? { type: "account_plan", id: p.id, name: p.name } : undefined, form: [{ key: "account_plan", label: "套餐配置", value: JSON.stringify({ name, enabled, grant: effectiveGrant }) }] });
  const pick = (key: "node_ids" | "profile_ids", id: number, checked: boolean) => update(key, checked ? [...(grant[key] ?? []), id] : (grant[key] ?? []).filter((x) => x !== id));
  return <Dialog open onOpenChange={(o) => !o && !save.busy && close()}><DialogContent size="md">
    <DialogTitle>{p ? "编辑套餐" : "新建套餐"}</DialogTitle><DialogDescription>按需填写流量、节点或有效期，任一项都能单独创建套餐。</DialogDescription>
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <Field label="套餐名称" name="name"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
      <SwitchField label="启用套餐" checked={enabled} onCheckedChange={setEnabled} />
      <Card variant="inset"><CardHeader title="流量" description="可单独发放流量，留空不增加额度。" /><CardContent className="stack"><SwitchField label="不限额度" checked={grant.unlimited} onCheckedChange={(v) => update("unlimited", v)} />
        <Field label="流量额度" name="grant" hint="0 不发放额外流量；每月重置的账户只加到本周期。"><div className="quota-input"><Input type="number" placeholder="不增加" min="0" max={Number.MAX_SAFE_INTEGER / units[unit]} step="any" disabled={grant.unlimited} value={quota} onChange={(e) => setQuota(e.target.value)} /><Select value={unit} onValueChange={(v) => setUnit(v as Unit)}><SelectTrigger aria-label="容量单位"><SelectValue>{unit}</SelectValue></SelectTrigger><SelectContent>{Object.keys(units).map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}</SelectContent></Select></div></Field>
      </CardContent></Card>
      <Card variant="inset"><CardHeader title="节点与订阅组" description="可只增加访问权限，不必设置流量。" /><CardContent className="stack">
      <SwitchField label="全部节点" description="叠加授权，不缩小已有范围。" checked={grant.all_nodes} onCheckedChange={(v) => update("all_nodes", v)} />
      {!grant.all_nodes && <fieldset className="stack"><legend>节点范围</legend>{nodes.length ? nodes.map((n) => <CheckboxField key={n.id} label={n.name || `${n.protocol} #${n.id}`} checked={grant.node_ids?.includes(n.id) ?? false} onCheckedChange={(v) => pick("node_ids", n.id, v)} />) : <small>尚无节点，可稍后编辑套餐。</small>}</fieldset>}
      <fieldset className="stack"><legend>订阅组授权</legend>{profiles.length ? profiles.map((g) => <CheckboxField key={g.id} label={g.name} checked={grant.profile_ids?.includes(g.id) ?? false} onCheckedChange={(v) => pick("profile_ids", g.id, v)} />) : <small>尚无订阅组。</small>}</fieldset>
      </CardContent></Card>
      <Card variant="inset"><CardHeader title="有效期" description="可单独延长现有账户期限。" /><CardContent>
        <Field label="有效天数" hint="0 不改变现有效期；现有长期账户继续长期。"><Input type="number" min="0" max="3650" step="1" value={grant.days || ""} placeholder="不改变" onChange={(e) => update("days", Number(e.target.value))} /></Field>
      </CardContent></Card>
      <GrantSummary grant={effectiveGrant} preview />
      <DialogFooter error={save.error}><Button type="button" variant="outline" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter>
    </Form>
  </DialogContent></Dialog>;
}
