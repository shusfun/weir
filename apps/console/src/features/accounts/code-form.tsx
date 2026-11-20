import { useEffect, useState } from "react";
import { Button, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea, Segmented, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useFormAction } from "../../lib/action";
import { useConsoleContext } from "../../lib/console-context";
import { fromLocalInput } from "../../lib/core";
import { GrantSummary } from "./grant";
import type { Plan, CodeKind } from "./types";

export function CodeForm({ initialKind = "redeem", initialPlanId, plans, onCreatePlan, close, saved }: { initialKind?: CodeKind; initialPlanId?: number; plans: Plan[]; onCreatePlan?: () => void; close: () => void; saved: () => void }) {
  const [kind, setKind] = useState<CodeKind>(initialKind);
  const enabledPlans = plans.filter((p) => p.enabled);
  const firstEnabledId = enabledPlans[0]?.id;
  const [plan, setPlan] = useState(initialPlanId ? String(initialPlanId) : kind === "invite" ? "none" : String(firstEnabledId ?? "none"));
  useEffect(() => {
    if (initialPlanId) setPlan(String(initialPlanId));
    else if (kind === "redeem" && plan === "none" && firstEnabledId != null) setPlan(String(firstEnabledId));
  }, [initialPlanId, kind, plan, firstEnabledId]);
  const [count, setCount] = useState(1);
  const [expiry, setExpiry] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const confirm = useConfirm();
  const [acknowledged, setAcknowledged] = useState(false);
  const requestClose = async () => { if (!codes || acknowledged || await confirm({ title: "已保存生成的码？", description: "关闭后无法再次读取明文，请确认已经复制或导出。", confirmText: "我已保存" })) close(); };
  const [copyError, setCopyError] = useState("");
  const action = useFormAction(
    () => api("/admin/access-codes", "POST", { kind, count, plan_id: plan === "none" ? null : Number(plan), expires_at: fromLocalInput(expiry) }),
    { fields: { account_plan_not_available: "plan" }, onSuccess: (r) => { if (r) { setCodes(r.items.map((v: { code: string }) => v.code)); saved(); } } },
  );
  useConsoleContext({ dialog: { title: kind === "invite" ? "生成邀请码" : "生成兑换码" }, form: [{ key: "issued_codes", label: "生成的码", secret: true, filled: !!codes }, { key: "code_plan", label: "发放套餐", value: plan }] });
  const exportCodes = () => {
    const url = URL.createObjectURL(new Blob([codes!.join("\n") + "\n"], { type: "text/plain;charset=utf-8" }));
    const a = document.createElement("a"); a.href = url; a.download = `weir-${kind}-codes.txt`; a.click(); setAcknowledged(true); setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  const chosenPlan = plans.find((p) => String(p.id) === plan);
  return (
    <Dialog open onOpenChange={(o) => !o && !action.busy && void requestClose()}>
      <DialogContent size="md">
        <DialogTitle>{kind === "invite" ? "生成邀请码" : "生成兑换码"}</DialogTitle>
        <DialogDescription>一次性使用，明文仅在生成后展示一次；签发时固定保存套餐快照。</DialogDescription>
        {codes ? (
          <div className="grid gap-[var(--weir-space-3)]">
            <Notice tone="ok">已生成 {codes.length} 个{kind === "invite" ? "邀请码" : "兑换码"}。请立即复制或导出，关闭窗口后无法再次读取明文。</Notice>
            <Field label="本次生成的码">
              <Textarea readOnly mono rows={Math.min(9, Math.max(3, codes.length))} value={codes.join("\n")} />
            </Field>
            <DialogFooter error={copyError}>
              <Button variant="ghost" onClick={() => void requestClose()}>我已保存，关闭</Button>
              <Button variant="outline" onClick={exportCodes}>导出文本</Button>
              <CopyButton value={codes.join("\n")} onCopied={() => { setCopyError(""); setAcknowledged(true); }} onError={() => setCopyError("复制失败，请选择文本手动复制或导出。")}>复制全部</CopyButton>
            </DialogFooter>
          </div>
        ) : (
          <Form busy={action.busy} errors={action.errors} onSubmit={() => void action.submit()}>
            <Field label="签发类型">
              <Segmented
                value={kind}
                onValueChange={(value) => {
                  const next = value as CodeKind;
                  setKind(next);
                  if (next === "redeem" && plan === "none") setPlan(String(enabledPlans[0]?.id ?? "none"));
                }}
                aria-label="码类型"
                options={[["redeem", "兑换码（老用户充入）"], ["invite", "邀请码（新用户注册）"]]}
              />
            </Field>
            {kind === "redeem" && enabledPlans.length === 0 ? (
              <Notice tone="warn">
                兑换码需要关联一份已启用的套餐预设。
                {onCreatePlan && <Button type="button" size="xs" variant="outline" className="ml-2" onClick={onCreatePlan}>先建套餐预设</Button>}
              </Notice>
            ) : (
              <Field label="关联套餐预设" name="plan" hint={kind === "invite" ? "可不附带套餐；新用户默认无流量额度" : "后续修改预设不影响已签发的码"}>
                <Select value={plan} onValueChange={(v) => setPlan(v ?? "none")}>
                  <SelectTrigger><SelectValue>{plan === "none" ? "不附带套餐" : chosenPlan?.name ?? "套餐未知"}</SelectValue></SelectTrigger>
                  <SelectContent>
                    {kind === "invite" && <SelectItem value="none">不附带套餐</SelectItem>}
                    {enabledPlans.map((p) => <SelectItem key={p.id} value={String(p.id)}>{p.name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>
            )}
            {chosenPlan && (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-[var(--weir-space-4)] py-2.5 text-meta">
                <div className="mb-0.5 text-fine font-semibold text-[var(--weir-ink-3)]">包含权益快照</div>
                <GrantSummary grant={chosenPlan.grant} />
              </div>
            )}
            <FormGrid>
              <Field label="生成数量" hint="单次最多 100 个">
                <Input required type="number" min="1" max="100" value={count} onChange={(e) => setCount(Number(e.target.value))} />
              </Field>
              <Field label="码本身到期时间" hint="留空不限；与套餐天数独立">
                <Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} />
              </Field>
            </FormGrid>
            <DialogFooter error={action.error}>
              <Button type="button" variant="ghost" onClick={close}>取消</Button>
              <Button type="submit" disabled={kind === "redeem" && plan === "none"} busy={action.busy} busyText="正在生成…">生成{kind === "invite" ? "邀请码" : "兑换码"}</Button>
            </DialogFooter>
          </Form>
        )}
      </DialogContent>
    </Dialog>
  );
}
