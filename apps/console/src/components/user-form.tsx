// 一个窗口建 / 改用户：账号、额度、周期、按月重置、节点范围、是否允许自建链式代理。
import { useState } from "react";
import { Button, CheckboxField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, PasswordInput, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { fromLocalInput, fromUnits, localInput, toUnits, units, type NodeItem, type Unit, type UserItem } from "../lib/core";

export function UserForm({ value, nodes, close, saved }: { value: UserItem | "new" | null; nodes: NodeItem[]; close: () => void; saved: (u: UserItem) => void }) {
  if (!value) return null;
  return <UserFormDialog key={value === "new" ? "new" : value.id} value={value} nodes={nodes} close={close} saved={saved} />;
}

function UserFormDialog({ value, nodes, close, saved }: { value: UserItem | "new"; nodes: NodeItem[]; close: () => void; saved: (u: UserItem) => void }) {
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
  const save = useFormAction(
    () => {
      const plan = {
        name: name.trim(), quota: fromUnits(quota, unit), expires_at: fromLocalInput(expiry), monthly_reset: monthly,
        all_nodes: all, node_ids: all ? [] : picked, allow_chain: chain, enabled,
      };
      return (isNew
        ? api("/admin/users", "POST", { ...plan, email: email.trim(), password })
        : api("/admin/users/" + u!.id, "PATCH", { ...plan, password: password || undefined })) as Promise<UserItem>;
    },
    {
      fields: { invalid_email: "email", email_taken: "email", weak_password: "password", invalid_password: "password", invalid_quota: "quota", invalid_expires_at: "expiry" },
      onSuccess: saved,
    },
  );
  // 页面感知：填到哪里（密码只标已填 / 未填），dsh 可以接着这张表拼出草稿
  useConsoleContext({
    dialog: { title: isNew ? "创建用户" : "编辑用户" },
    object: u ? { type: "user", name: u.name || u.email, id: u.id } : undefined,
    form: [
      field("email", "邮箱", email), field("name", "备注名", name), field("password", "密码", password, true),
      field("quota", "额度", quota ? `${quota} ${unit}` : "不限"), field("expires_at", "到期", expiry || "长期"), field("monthly_reset", "按月重置", monthly ? "是" : "否"),
      field("all_nodes", "全部节点", all ? "是" : "否"), field("node_ids", "选中的节点", nodes.filter((n) => picked.includes(n.id)).map((n) => n.name).join("、")),
      field("allow_chain", "允许自建链式代理", chain ? "是" : "否"), field("enabled", "启用", enabled ? "是" : "否"),
    ],
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{isNew ? "创建用户" : "编辑用户"}</DialogTitle>
        <DialogDescription>{isNew ? "保存后自动生成一条包含所选节点的订阅链接。" : "修改后相关 VPS 会在数秒内应用新的套餐。"}</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="邮箱" name="email">
              <Input type="email" required disabled={!isNew} value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <Field label="备注名">
              <Input maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
          </FormGrid>
          <Field label={isNew ? "初始密码（可选）" : "重设密码（可选）"} name="password" hint={isNew ? "留空时自动生成，只显示一次。" : "至少 6 位，含大小写字母和数字；修改后该用户需重新登录。"}>
            <PasswordInput autoComplete="new-password" minLength={6} maxLength={72} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <FormGrid>
            <Field label="流量额度" htmlFor="u-quota" name="quota" hint="0 表示不限。">
              <div className="quota-input">
                <Input id="u-quota" type="number" min="0" step="any" required value={quota} onChange={(e) => setQuota(e.target.value)} />
                <Select value={unit} onValueChange={(v) => setUnit(v as Unit)}>
                  <SelectTrigger aria-label="容量单位"><SelectValue>{unit}</SelectValue></SelectTrigger>
                  <SelectContent>{Object.keys(units).map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}</SelectContent>
                </Select>
              </div>
            </Field>
            <Field label="到期时间" name="expiry" hint="留空为长期有效。">
              <Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} />
            </Field>
          </FormGrid>
          <SwitchField label="每月重置用量" description="以本周期开始日为准。" checked={monthly} onCheckedChange={setMonthly} />
          <SwitchField label="全部节点" description="以后新建的节点自动加入。" checked={all} onCheckedChange={setAll} />
          {!all && (
            <fieldset className="grid gap-2">
              <legend className="mb-1 text-[12.5px] font-medium text-[var(--mg-ink-2)]">可用节点</legend>
              {nodes.length === 0 ? <small>还没有节点。</small> : nodes.map((n) => (
                <CheckboxField
                  key={n.id}
                  label={<>{n.name}<small className="text-[var(--mg-ink-3)]">（{n.gate_name}）</small></>}
                  checked={picked.includes(n.id)}
                  onCheckedChange={(v) => setPicked(v ? [...picked, n.id] : picked.filter((x) => x !== n.id))}
                />
              ))}
            </fieldset>
          )}
          <SwitchField label="允许用户自建链式代理" checked={chain} onCheckedChange={setChain} />
          {!isNew && <SwitchField label="账户启用" checked={enabled} onCheckedChange={setEnabled} />}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">{isNew ? "创建用户" : "保存"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
