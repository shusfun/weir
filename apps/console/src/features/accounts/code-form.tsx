import { useState } from "react";
import { Button, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea, Segmented, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useFormAction } from "../../lib/action";
import { useConsoleContext } from "../../lib/console-context";
import { fromLocalInput } from "../../lib/core";
import { GrantSummary } from "./grant";
import type { Plan, CodeKind } from "./types";

export function CodeForm({ initialKind = "invite", plans, close, saved }: { initialKind?: CodeKind; plans: Plan[]; close: () => void; saved: () => void }) {
  const [kind, setKind] = useState<CodeKind>(initialKind);
  const [plan, setPlan] = useState(kind === "invite" ? "none" : String(plans.find((p) => p.enabled)?.id ?? "none"));
  const [count, setCount] = useState(1);
  const [expiry, setExpiry] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const confirm = useConfirm();
  const [acknowledged, setAcknowledged] = useState(false);
  const requestClose = async () => { if (!codes || acknowledged || await confirm({ title: "已保存生成的码？", description: "关闭后无法再次读取明文，请确认已经复制或导出。", confirmText: "我已保存" })) close(); };
  const [copyError, setCopyError] = useState("");
  const action = useFormAction(async () => {
    if (!(await confirm({ title: `生成 ${count} 个${kind === "invite" ? "邀请码" : "兑换码"}？`, description: "明文只显示一次；关闭窗口后不能再次读取。", confirmText: "生成" }))) return null;
    return api("/admin/access-codes", "POST", { kind, count, plan_id: plan === "none" ? null : Number(plan), expires_at: fromLocalInput(expiry) });
  }, { fields: { account_plan_not_available: "plan" }, onSuccess: (r) => { if (r) { setCodes(r.items.map((v: { code: string }) => v.code)); saved(); } } });
  useConsoleContext({ dialog: { title: kind === "invite" ? "生成邀请码" : "生成兑换码" }, form: [{ key: "issued_codes", label: "生成的码", secret: true, filled: !!codes }, { key: "code_plan", label: "发放套餐", value: plan }] });
  const exportCodes = () => {
    const url = URL.createObjectURL(new Blob([codes!.join("\n") + "\n"], { type: "text/plain;charset=utf-8" }));
    const a = document.createElement("a"); a.href = url; a.download = `weir-${kind}-codes.txt`; a.click(); setAcknowledged(true); setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return <Dialog open onOpenChange={(o) => !o && !action.busy && void requestClose()}><DialogContent size="md"><DialogTitle>{kind === "invite" ? "生成邀请码" : "生成兑换码"}</DialogTitle><DialogDescription>使用一次后失效；只保存摘要和签发时的套餐快照。</DialogDescription>
    {codes ? <><Notice>已生成 {codes.length} 个{kind === "invite" ? "邀请码" : "兑换码"}。请现在复制或导出，关闭后无法取回明文。</Notice><Field label="本次生成的码"><Textarea readOnly mono rows={Math.min(9, Math.max(3, codes.length))} value={codes.join("\n")} /></Field><DialogFooter error={copyError}><Button variant="ghost" onClick={() => void requestClose()}>我已保存，关闭</Button><Button variant="outline" onClick={exportCodes}>导出文本</Button><CopyButton value={codes.join("\n")} onCopied={() => { setCopyError(""); setAcknowledged(true); }} onError={() => setCopyError("复制失败，请选择文本手动复制或导出。")}>复制全部</CopyButton></DialogFooter></> :
      <Form busy={action.busy} errors={action.errors} onSubmit={() => void action.submit()}>
        <Field label="类型"><Segmented value={kind} onValueChange={(value) => { const next = value as CodeKind; setKind(next); if (next === "redeem" && plan === "none") setPlan(String(plans.find((p) => p.enabled)?.id ?? "none")); }} aria-label="码类型" options={[["invite", "邀请码"], ["redeem", "兑换码"]]} /></Field>
        <Field label="发放套餐" name="plan" hint={kind === "invite" ? "可不附带套餐；新用户默认没有额度。" : "兑换码必须选择已启用套餐。"}>
          <Select value={plan} onValueChange={(v) => setPlan(v ?? "none")}><SelectTrigger><SelectValue>{plan === "none" ? "不附带套餐" : plans.find((p) => String(p.id) === plan)?.name ?? "套餐未知"}</SelectValue></SelectTrigger><SelectContent>{kind === "invite" && <SelectItem value="none">不附带套餐</SelectItem>}{plans.filter((p) => p.enabled).map((p) => <SelectItem key={p.id} value={String(p.id)}>{p.name} · 版本 {p.revision}</SelectItem>)}</SelectContent></Select>
        </Field>
        {plans.find((p) => String(p.id) === plan) && <GrantSummary grant={plans.find((p) => String(p.id) === plan)!.grant} preview />}
        <Field label="生成数量"><Input required type="number" min="1" max="100" value={count} onChange={(e) => setCount(Number(e.target.value))} /></Field>
        <Field label="码到期时间" hint="留空为不限；最长十年。与套餐有效期分开计算。"><Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} /></Field>
        <DialogFooter error={action.error}><Button type="button" variant="outline" onClick={close}>取消</Button><Button type="submit" disabled={kind === "redeem" && plan === "none"} busy={action.busy} busyText="正在生成…">生成</Button></DialogFooter>
      </Form>}
  </DialogContent></Dialog>;
}
