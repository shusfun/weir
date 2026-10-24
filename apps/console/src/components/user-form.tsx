// 一个窗口建 / 改用户：账号、额度、周期、按月重置、节点范围、是否允许自建链式代理。
import { useState } from "react";
import { Button, Checkbox, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Label, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch } from "@weir/ui";
import { api } from "../api";
import { fromLocalInput, fromUnits, localInput, toUnits, units, type NodeItem, type Unit, type UserItem } from "../lib/core";

export function UserForm({ value, nodes, close, saved }: { value: UserItem | "new" | null; nodes: NodeItem[]; close: () => void; saved: (u: UserItem) => void }) {
  if (!value) return null;
  return <Form key={value === "new" ? "new" : value.id} value={value} nodes={nodes} close={close} saved={saved} />;
}

function Form({ value, nodes, close, saved }: { value: UserItem | "new"; nodes: NodeItem[]; close: () => void; saved: (u: UserItem) => void }) {
  const isNew = value === "new";
  const u = isNew ? undefined : value;
  const [initQuota, initUnit] = toUnits(u?.quota ?? 0);
  const [email, setEmail] = useState(u?.email ?? "");
  const [name, setName] = useState(u?.name ?? "");
  const [password, setPassword] = useState("");
  const [quota, setQuota] = useState(initQuota);
  const [unit, setUnit] = useState<Unit>(initUnit);
  const [expiry, setExpiry] = useState(localInput(u?.expires_at));
  const [monthly, setMonthly] = useState(u?.monthly_reset ?? false);
  const [all, setAll] = useState(u?.all_nodes ?? true);
  const [picked, setPicked] = useState<number[]>(u?.node_ids ?? []);
  const [chain, setChain] = useState(u?.allow_chain ?? true);
  const [enabled, setEnabled] = useState(u?.enabled ?? true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    setError("");
    const plan = {
      name: name.trim(), quota: fromUnits(quota, unit), expires_at: fromLocalInput(expiry), monthly_reset: monthly,
      all_nodes: all, node_ids: all ? [] : picked, allow_chain: chain, enabled,
    };
    try {
      const res = isNew
        ? await api("/admin/users", "POST", { ...plan, email: email.trim(), password })
        : await api("/admin/users/" + u!.id, "PATCH", { ...plan, password: password || undefined });
      saved(res as UserItem);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{isNew ? "创建用户" : "编辑用户"}</DialogTitle>
        <DialogDescription>{isNew ? "保存后自动生成一条包含所选节点的订阅链接。" : "修改后相关 VPS 会在数秒内应用新的套餐。"}</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="邮箱" htmlFor="u-email">
              <Input id="u-email" type="email" required disabled={!isNew} value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <Field label="备注名" htmlFor="u-name">
              <Input id="u-name" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
          </div>
          <Field label={isNew ? "初始密码（可选）" : "重设密码（可选）"} htmlFor="u-pass" hint={isNew ? "留空时自动生成，只显示一次。" : "至少 6 位，含大小写字母和数字；修改后该用户需重新登录。"}>
            <Input id="u-pass" type="password" autoComplete="new-password" minLength={6} maxLength={72} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="流量额度" htmlFor="u-quota" hint="0 表示不限。">
              <div className="quota-input">
                <Input id="u-quota" type="number" min="0" step="any" required value={quota} onChange={(e) => setQuota(e.target.value)} />
                <Select value={unit} onValueChange={(v) => setUnit(v as Unit)}>
                  <SelectTrigger aria-label="容量单位"><SelectValue>{unit}</SelectValue></SelectTrigger>
                  <SelectContent>{Object.keys(units).map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}</SelectContent>
                </Select>
              </div>
            </Field>
            <Field label="到期时间" htmlFor="u-exp" hint="留空为长期有效。">
              <Input id="u-exp" type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} />
            </Field>
          </div>
          <Label className="actions"><Switch aria-label="每月重置用量" checked={monthly} onCheckedChange={setMonthly} />每月重置用量（以本周期开始日为准）</Label>
          <Label className="actions"><Switch aria-label="全部节点" checked={all} onCheckedChange={setAll} />全部节点（以后新建的节点自动加入）</Label>
          {!all && (
            <fieldset className="stack">
              <legend className="text-sm">可用节点</legend>
              {nodes.length === 0 ? <small>还没有节点。</small> : nodes.map((n) => (
                <Label key={n.id} className="actions">
                  <Checkbox checked={picked.includes(n.id)} onCheckedChange={(v) => setPicked(v ? [...picked, n.id] : picked.filter((x) => x !== n.id))} />
                  {n.name}<small className="text-[var(--mg-ink-3)]">（{n.gate_name}）</small>
                </Label>
              ))}
            </fieldset>
          )}
          <Label className="actions"><Switch aria-label="允许用户自建链式代理" checked={chain} onCheckedChange={setChain} />允许用户自建链式代理</Label>
          {!isNew && <Label className="actions"><Switch aria-label="账户启用" checked={enabled} onCheckedChange={setEnabled} />账户启用</Label>}
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : isNew ? "创建用户" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
