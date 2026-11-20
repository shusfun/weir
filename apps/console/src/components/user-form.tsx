import { useFormGuard } from "../lib/dirty";
import { NodeSummary, nodeTitle } from "./node-summary";
// 一个窗口建 / 改用户：账号、额度、周期、按月重置、节点范围、是否允许自建链式代理。
import { useState } from "react";
import { Button, CheckboxField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, FormSection, Input, PasswordInput, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, SwitchGroup } from "@weir/ui";
import { api, clause } from "../api";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { fromLocalInput, fromUnits, localInput, toUnits, units, type NodeItem, type Unit, type UserItem } from "../lib/core";

// nodesState：节点列表还在读取或读取失败时写明原因，不能显示成「还没有节点」
export type NodesState = { loading: boolean; error?: unknown };
export function UserForm({ value, nodes, nodesState, close, saved }: { value: UserItem | "new" | null; nodes: NodeItem[]; nodesState: NodesState; close: () => void; saved: (u: UserItem) => void }) {
  if (!value) return null;
  return <UserFormDialog key={value === "new" ? "new" : value.id} value={value} nodes={nodes} nodesState={nodesState} close={close} saved={saved} />;
}

function UserFormDialog({ value, nodes, nodesState, close, saved }: { value: UserItem | "new"; nodes: NodeItem[]; nodesState: NodesState; close: () => void; saved: (u: UserItem) => void }) {
  const isNew = value === "new";
  const u = isNew ? undefined : value;
  const [initQuota, initUnit] = toUnits(u?.quota ?? 0);
  const [email, setEmail] = useState(u?.email ?? "");
  const [name, setName] = useState(u?.name ?? "");
  const [password, setPassword] = useState("");
  const [quota, setQuota] = useState(initQuota);
  const [unlimited, setUnlimited] = useState(u?.unlimited ?? false);
  const [unit, setUnit] = useState<Unit>(initUnit);
  const [expiry, setExpiry] = useState(localInput(u?.expires_at));
  const [monthly, setMonthly] = useState(u?.monthly_reset ?? false);
  const [all, setAll] = useState(u?.all_nodes ?? true);
  const [picked, setPicked] = useState<number[]>(u?.node_ids ?? []);
  const [chain, setChain] = useState(u?.allow_chain ?? true);
  const [enabled, setEnabled] = useState(u?.enabled ?? true);
  const guard = useFormGuard({ email, name, password, quota, unlimited, unit, expiry, monthly, all, picked, chain, enabled }, close);
  const save = useFormAction(
    () => {
      const plan = {
        name: name.trim(), quota: fromUnits(quota, unit), unlimited, expires_at: fromLocalInput(expiry), monthly_reset: monthly,
        all_nodes: all || u?.role === "admin", node_ids: all || u?.role === "admin" ? [] : picked, allow_chain: chain, enabled,
      };
      return (isNew
        ? api("/admin/users", "POST", { ...plan, email: email.trim(), password })
        : api("/admin/users/" + u!.id, "PATCH", { ...plan, password: password || undefined })) as Promise<UserItem>;
    },
    {
      fields: { invalid_email: "email", email_taken: "email", weak_password: "password", invalid_password: "password", invalid_quota: "quota", invalid_expires_at: "expiry" },
      onSuccess: (u) => { guard.commit(); saved(u); },
    },
  );
  // 页面感知：填到哪里（密码只标已填 / 未填），dsh 可以接着这张表拼出草稿
  useConsoleContext({
    dialog: { title: isNew ? "创建用户" : "编辑用户" },
    object: u ? { type: "user", name: u.name || u.email, id: u.id } : undefined,
    form: [
      field("email", "邮箱", email), field("name", "备注名", name), field("password", "密码", password, true),
      field("quota", "额度", unlimited ? "不限" : `${quota} ${unit}`), field("unlimited", "不限额度", unlimited ? "是" : "否"), field("expires_at", "到期", expiry || "长期"), field("monthly_reset", "按月重置", monthly ? "是" : "否"),
      field("all_nodes", "全部节点", all ? "是" : "否"), field("node_ids", "选中的节点", nodes.filter((n) => picked.includes(n.id)).map(nodeTitle).join("、")),
      field("allow_chain", "允许自建链式代理", chain ? "是" : "否"), field("enabled", "启用", enabled ? "是" : "否"),
    ],
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && void guard.close()}>
      <DialogContent size="md">
        <DialogTitle>{isNew ? "创建用户" : "编辑用户"}</DialogTitle>
        <DialogDescription>{isNew ? "保存后自动生成一条包含所选节点的订阅链接。" : "修改后相关 VPS 会在数秒内应用新的套餐。"}</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormSection title="账号信息">
            <FormGrid>
              <Field label="邮箱" name="email">
                <Input type="email" required disabled={!isNew} placeholder="user@example.com" value={email} onChange={(e) => setEmail(e.target.value)} />
              </Field>
              <Field label="备注名">
                <Input maxLength={100} placeholder="例如：朋友 MacBook" value={name} onChange={(e) => setName(e.target.value)} />
              </Field>
            </FormGrid>
            <Field label={isNew ? "初始密码（可选）" : "重设密码（可选）"} name="password" hint={isNew ? "留空自动生成并只显示一次" : "至少 6 个字符；修改后该用户需重新登录"}>
              <PasswordInput autoComplete="new-password" minLength={6} maxLength={72} value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
          </FormSection>

          <FormSection title="额度与周期">
            <SwitchGroup data-tour="user-plan">
              <SwitchField placement="end" label="不限流量额度" description="关闭后按下方额度执行；0 表示没有可用流量" checked={unlimited} onCheckedChange={setUnlimited} />
              <SwitchField placement="end" label="每月自动重置用量" description="按本周期起始日每月归零" checked={monthly} onCheckedChange={setMonthly} />
            </SwitchGroup>
            <FormGrid>
              <Field label="流量额度" htmlFor="u-quota" name="quota" hint={unlimited ? "已开启不限额度" : "0 表示没有可用流量"}>
                <div className="quota-input">
                  <Input id="u-quota" type="number" min="0" step="any" required disabled={unlimited} value={quota} onChange={(e) => setQuota(e.target.value)} />
                  <Select value={unit} onValueChange={(v) => setUnit(v as Unit)}>
                    <SelectTrigger aria-label="容量单位"><SelectValue>{unit}</SelectValue></SelectTrigger>
                    <SelectContent>{Object.keys(units).map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
              </Field>
              <Field label="到期时间" name="expiry" hint="留空为长期有效">
                <Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} />
              </Field>
            </FormGrid>
          </FormSection>

          <FormSection title="节点与权限">
            {u?.role === "admin" && <small className="text-[var(--weir-ink-3)]">管理员账户固定包含全部节点，新建节点自动进入订阅。</small>}
            <SwitchGroup>
              {u?.role !== "admin" && (
                <SwitchField placement="end" label="授权全部节点" description="后续新建节点自动加入；关闭后按列表勾选" checked={all} onCheckedChange={setAll} />
              )}
              <SwitchField placement="end" label="允许自建链式代理" description="允许该用户在「我的代理」中挂载自己的落地代理" checked={chain} onCheckedChange={setChain} />
              {!isNew && <SwitchField placement="end" label="账户启用" description="停用后所有订阅链接与连接立即暂停" checked={enabled} onCheckedChange={setEnabled} />}
            </SwitchGroup>
            {!all && u?.role !== "admin" && (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-[var(--weir-space-4)] py-[var(--weir-space-3)]">
                <div className="mb-2 flex items-center justify-between gap-2">
                  <span className="text-fine font-semibold text-[var(--weir-ink-2)]">可用节点（已选 {picked.length}{nodesState.loading ? "" : ` / ${nodes.length}`}）</span>
                  {nodes.length > 0 && (
                    <button
                      type="button"
                      className="weir-press cursor-pointer border-0 bg-transparent p-0 text-fine font-semibold text-[var(--weir-accent-text)] hover:underline"
                      onClick={() => setPicked(picked.length === nodes.length ? [] : nodes.map((n) => n.id))}
                    >
                      {picked.length === nodes.length ? "清空" : "全选"}
                    </button>
                  )}
                </div>
                <div className="grid max-h-44 gap-2 overflow-y-auto">
                  {nodesState.error ? <small className="text-[var(--weir-bad-ink)]">读取节点失败：{clause(nodesState.error)}</small> : nodesState.loading ? <small className="text-[var(--weir-ink-3)]">正在读取节点…</small> : nodes.length === 0 ? <small className="text-[var(--weir-ink-3)]">还没有节点。</small> : nodes.map((n) => (
                    <CheckboxField
                      key={n.id}
                      label={<NodeSummary node={n} />}
                      checked={picked.includes(n.id)}
                      onCheckedChange={(v) => setPicked(v ? [...picked, n.id] : picked.filter((x) => x !== n.id))}
                    />
                  ))}
                </div>
              </div>
            )}
          </FormSection>

          <DialogFooter error={save.error}>
            <Button type="button" variant="ghost" disabled={save.busy} onClick={() => { if (!save.busy) void guard.close(); }}>取消</Button>
            <Button data-tour="user-submit" type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">{isNew ? "创建用户" : "保存"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
