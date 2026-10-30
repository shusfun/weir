// 规则模板（6 个内置 + 自定义 ini / Clash YAML）与订阅源页的默认设置。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, SectionHeader, Segmented, SwitchField, Textarea, useToast, useConfirm } from "@weir/ui";
import { Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { ruleLevels, type CustomTemplate, type SubscriptionSettings } from "../lib/subscriptions";
import { OperatorEditor, type Operator } from "./operator-editor";
import { TemplatePicker, useTemplates } from "./subscription-profiles";
import { State } from "./feedback";
import { field, useConsoleContext } from "../lib/console-context";

export function TemplatesPanel() {
  const confirm = useConfirm();
  const toast = useToast();
  const t = useTemplates();
  const [edit, setEdit] = useState<CustomTemplate | "new" | null>(null);
  const { run, isBusy } = useRowAction<number>({ refetch: t.refetch });
  async function remove(c: CustomTemplate) {
    if (!(await confirm({ title: `删除模板「${c.name}」？`, confirmText: "删除", tone: "danger" }))) return;
    await run(c.id, () => api("/admin/subscriptions/templates/" + c.id, "DELETE"), "模板已删除");
  }
  useConsoleContext({ tab: "输出模板" });
  return (
    <section className="stack">
      <SectionHeader title="规则模板" description="决定策略组与分流规则。内置模板可直接使用；也可以粘贴 ini 模板或完整的 Clash YAML 配置。" actions={<Button size="sm" onClick={() => setEdit("new")}><Plus />新建模板</Button>} />
      {t.isPending || t.error ? (
        <State loading={t.isPending} error={t.error} onRetry={() => t.refetch()} />
      ) : (
        <>
          <ul className="grid gap-2 sm:grid-cols-2">
            {t.data.builtin.map((b) => (
              <li key={b.id} className="rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
                <strong>{b.name}</strong> <Badge variant="soft" tone="neutral">内置</Badge>
                <small className="block text-[var(--weir-ink-3)]">{b.description}</small>
              </li>
            ))}
          </ul>
          {t.data.custom.length === 0 ? (
            <State empty="还没有自定义模板" />
          ) : (
            <ul className="stack">
              {t.data.custom.map((c) => (
                <li key={c.id} className="flex items-center justify-between gap-2">
                  <span><strong>{c.name}</strong> <Badge variant="soft" tone="neutral">{c.format === "ini" ? "ini" : "Clash YAML"}</Badge></span>
                  <span className="flex flex-nowrap gap-1.5">
                    <Button size="sm" variant="outline" onClick={() => setEdit(c)}>编辑</Button>
                    <Button size="icon-sm" variant="destructive" aria-label={"删除 " + c.name} disabled={isBusy(c.id)} onClick={() => void remove(c)}><Trash2 /></Button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {edit && <TemplateForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void t.refetch(); }} />}
    </section>
  );
}

function TemplateForm({ value, close, saved }: { value: CustomTemplate | "new"; close: () => void; saved: () => void }) {
  const c = value === "new" ? undefined : value;
  const [name, setName] = useState(c?.name ?? "");
  const [format, setFormat] = useState<"ini" | "yaml">(c?.format ?? "ini");
  const [content, setContent] = useState(c?.content ?? "");
  const save = useFormAction(
    () => {
      const body = { name: name.trim(), format, content };
      return c ? api("/admin/subscriptions/templates/" + c.id, "PATCH", body) : api("/admin/subscriptions/templates", "POST", body);
    },
    { fields: { invalid_template: "content", invalid_output_template: "content" }, onSuccess: saved },
  );
  useConsoleContext({ dialog: { title: c ? "编辑模板" : "新建模板" }, form: [field("name", "名称", name), field("format", "格式", format), field("content", "模板内容", content.split("\n").length + " 行")] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="xl">
        <DialogTitle>{c ? "编辑模板" : "新建模板"}</DialogTitle>
        <DialogDescription>ini 模板用 custom_proxy_group= 与 ruleset= 定义；Clash YAML 需要包含 proxy-groups 与 rules，节点由系统填入。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="名称"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="格式"><Segmented aria-label="模板格式" value={format} onValueChange={setFormat} options={[["ini", "ini"], ["yaml", "Clash YAML"]] as const} /></Field>
          </FormGrid>
          <Field label="内容" name="content"><Textarea required rows={16} className="font-mono text-xs" value={content} onChange={(e) => setContent(e.target.value)} /></Field>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

export function SettingsPanel() {
  const q = useQuery({ queryKey: ["subscription-settings"], queryFn: () => api("/admin/subscriptions/settings") as Promise<SubscriptionSettings> });
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />;
  return <SettingsForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />;
}

const switches = [
  ["flag_emoji", "地区旗帜"],
  ["prepend_manual", "手动节点加前缀"],
  ["prepend_sources", "订阅源节点加源名称"],
  ["prepend_group_name", "手动节点加分组名"],
  ["traffic_nodes", "置顶流量 / 到期提示节点"],
  ["enable_udp", "输出中开启 UDP"],
  ["skip_cert_verify", "输出中跳过证书校验（不推荐）"],
  ["disguise", "伪装页：浏览器打开订阅地址只看到普通页面（用量与一键导入改为登录后在「我的订阅」查看；客户端拉取不受影响）"],
] as const;

function SettingsForm({ value, saved }: { value: SubscriptionSettings; saved: () => void }) {
  const [s, setS] = useState<SubscriptionSettings>(value);
  const [ops, setOps] = useState<Operator[]>(value.default_operators ?? []);
  const set = (patch: Partial<SubscriptionSettings>) => setS({ ...s, ...patch });
  const save = useFormAction(() => api("/admin/subscriptions/settings", "PUT", { ...s, default_operators: ops }), {
    ok: "已保存",
    fields: { invalid_template: "template" },
    onSuccess: saved,
  });
  return (
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <SectionHeader title="默认设置" description="订阅组未单独设置的项使用这里的值。" />
      <FormGrid>
        <Field label="默认规则模板" name="template"><TemplatePicker value={s.template} onChange={(v) => set({ template: v ?? "" })} /></Field>
        <Field label="规则等级"><Segmented aria-label="规则等级" value={s.rule_level || "std"} onValueChange={(v) => set({ rule_level: v })} options={ruleLevels} /></Field>
        <Field label="手动节点前缀"><Input value={s.manual_prefix} onChange={(e) => set({ manual_prefix: e.target.value })} /></Field>
        <Field label="输出缓存（分钟）" hint="0 表示每次都实时拉取订阅源。"><Input inputMode="numeric" value={String(s.cache_minutes)} onChange={(e) => set({ cache_minutes: Number(e.target.value.replace(/\D/g, "")) || 0 })} /></Field>
        <Field label="客户端更新间隔（小时）"><Input inputMode="numeric" value={String(s.update_interval)} onChange={(e) => set({ update_interval: Number(e.target.value.replace(/\D/g, "")) || 24 })} /></Field>
      </FormGrid>
      <FormGrid>
        {switches.map(([k, label]) => (
          <SwitchField key={k} label={label} checked={s[k]} onCheckedChange={(v) => set({ [k]: v })} />
        ))}
      </FormGrid>
      <Field label="全局只保留（每行一条）"><Textarea rows={2} value={s.filter_include} onChange={(e) => set({ filter_include: e.target.value })} /></Field>
      <Field label="全局排除（每行一条）"><Textarea rows={2} value={s.filter_exclude} onChange={(e) => set({ filter_exclude: e.target.value })} /></Field>
      <Collapsible title={`默认处理链（${ops.length} 步；订阅组没有自己的处理链时使用）`}>
        <OperatorEditor value={ops} change={setOps} />
      </Collapsible>
      <div className="actions"><Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存默认设置</Button></div>
      {save.error && <Notice tone="bad">{save.error}</Notice>}
    </Form>
  );
}
