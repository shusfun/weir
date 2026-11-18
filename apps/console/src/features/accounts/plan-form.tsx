import { useState } from "react";
import { Button, CheckboxField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, FormSection, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, SwitchGroup } from "@weir/ui";
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
  const update = <K extends keyof Grant>(key: K, v: Grant[K]) => setGrant((g) => ({ ...g, [key]: v }));
  const save = useFormAction(
    () => api("/admin/account-plans" + (p ? "/" + p.id : ""), p ? "PUT" : "POST", { name, enabled, grant: effectiveGrant, revision: p?.revision }),
    { fields: { invalid_plan_name: "name", invalid_plan_grant: "grant" }, onSuccess: (r) => { if (r) saved(); } },
  );
  useConsoleContext({ dialog: { title: p ? "编辑套餐" : "新建套餐" }, object: p ? { type: "account_plan", id: p.id, name: p.name } : undefined, form: [{ key: "account_plan", label: "套餐配置", value: JSON.stringify({ name, enabled, grant: effectiveGrant }) }] });
  const pick = (key: "node_ids" | "profile_ids", id: number, checked: boolean) => update(key, checked ? [...(grant[key] ?? []), id] : (grant[key] ?? []).filter((x) => x !== id));
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{p ? "编辑套餐预设" : "新建套餐预设"}</DialogTitle>
        <DialogDescription>可按需组合流量、时长与节点权限；修改预设不会影响之前已签发的兑换码。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormSection title="基本">
            <Field label="套餐名称" name="name">
              <Input required maxLength={100} placeholder="例如：月度 200G 标准包" value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <SwitchGroup>
              <SwitchField placement="end" label="启用此预设" description="关闭后不可用于签发新码" checked={enabled} onCheckedChange={setEnabled} />
            </SwitchGroup>
          </FormSection>

          <FormSection title="流量与有效期">
            <SwitchGroup>
              <SwitchField placement="end" label="不限流量额度" description="开启后兑换账户变为不限流量" checked={grant.unlimited} onCheckedChange={(v) => update("unlimited", v)} />
            </SwitchGroup>
            <FormGrid>
              <Field label="发放流量" name="grant" hint="留空或 0 不增加额度">
                <div className="quota-input">
                  <Input type="number" placeholder="不增加" min="0" max={Number.MAX_SAFE_INTEGER / units[unit]} step="any" disabled={grant.unlimited} value={quota} onChange={(e) => setQuota(e.target.value)} />
                  <Select value={unit} onValueChange={(v) => setUnit(v as Unit)}>
                    <SelectTrigger aria-label="容量单位"><SelectValue>{unit}</SelectValue></SelectTrigger>
                    <SelectContent>{Object.keys(units).map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
              </Field>
              <Field label="顺延天数" hint="留空或 0 不改变现有效期">
                <Input type="number" min="0" max="3650" step="1" value={grant.days || ""} placeholder="不改变" onChange={(e) => update("days", Number(e.target.value))} />
              </Field>
            </FormGrid>
          </FormSection>

          <FormSection title="节点与订阅组权限">
            <SwitchGroup>
              <SwitchField placement="end" label="包含全部自有节点" description="叠加授权，不缩小账户已有范围" checked={grant.all_nodes} onCheckedChange={(v) => update("all_nodes", v)} />
            </SwitchGroup>
            {!grant.all_nodes && (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-[var(--weir-space-4)] py-[var(--weir-space-3)]">
                <div className="mb-2 text-fine font-semibold text-[var(--weir-ink-2)]">指定自有节点（已选 {grant.node_ids?.length ?? 0}）</div>
                <div className="grid max-h-36 gap-2 overflow-y-auto">
                  {nodes.length ? nodes.map((n) => (
                    <CheckboxField key={n.id} label={n.name || `${n.protocol} #${n.id}`} checked={grant.node_ids?.includes(n.id) ?? false} onCheckedChange={(v) => pick("node_ids", n.id, v)} />
                  )) : <small className="text-[var(--weir-ink-3)]">尚无节点，可稍后编辑补充。</small>}
                </div>
              </div>
            )}
            {profiles.length > 0 && (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-[var(--weir-space-4)] py-[var(--weir-space-3)]">
                <div className="mb-2 text-fine font-semibold text-[var(--weir-ink-2)]">附加外部订阅组（已选 {grant.profile_ids?.length ?? 0}）</div>
                <div className="grid max-h-36 gap-2 overflow-y-auto">
                  {profiles.map((g) => (
                    <CheckboxField key={g.id} label={g.name} checked={grant.profile_ids?.includes(g.id) ?? false} onCheckedChange={(v) => pick("profile_ids", g.id, v)} />
                  ))}
                </div>
              </div>
            )}
          </FormSection>

          <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-[var(--weir-space-4)] py-2.5 text-meta">
            <div className="mb-0.5 text-fine font-semibold text-[var(--weir-ink-3)]">效果预览</div>
            <GrantSummary grant={effectiveGrant} />
          </div>

          <DialogFooter error={save.error}>
            <Button type="button" variant="ghost" onClick={close} disabled={save.busy}>取消</Button>
            <Button type="submit" busy={save.busy} busyText="正在保存…">保存预设</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
