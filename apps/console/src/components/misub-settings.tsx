// 规则模板（6 个内置 + 自定义 ini / Clash YAML）与订阅管理的默认设置。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Label, Notice, SectionHeader, Segmented, Switch, Textarea, useToast } from "@weir/ui";
import { Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { ruleLevels, type CustomTemplate, type MisubSettings } from "../lib/misub";
import { OperatorEditor, type Operator } from "./operator-editor";
import { TemplatePicker, useTemplates } from "./misub-profiles";
import { State } from "./feedback";

export function TemplatesPanel() {
  const toast = useToast();
  const t = useTemplates();
  const [edit, setEdit] = useState<CustomTemplate | "new" | null>(null);
  const [busy, setBusy] = useState(0);
  async function remove(c: CustomTemplate) {
    if (!window.confirm(`删除模板「${c.name}」？`)) return;
    setBusy(c.id);
    try {
      await api("/admin/misub/templates/" + c.id, "DELETE");
      await t.refetch();
    } catch (e) {
      toast("删除失败", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
  }
  return (
    <section className="stack">
      <SectionHeader title="规则模板" description="决定策略组与分流规则。内置模板可直接使用；也可以粘贴 ini 模板或完整的 Clash YAML 配置。" actions={<Button size="sm" onClick={() => setEdit("new")}><Plus />新建模板</Button>} />
      {t.isPending || t.error ? (
        <State loading={t.isPending} error={t.error} onRetry={() => t.refetch()} />
      ) : (
        <>
          <ul className="grid gap-2 sm:grid-cols-2">
            {t.data.builtin.map((b) => (
              <li key={b.id} className="rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
                <strong>{b.name}</strong> <Badge variant="soft" tone="neutral">内置</Badge>
                <small className="block text-[var(--mg-ink-3)]">{b.description}</small>
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
                    <Button size="icon-sm" variant="destructive" aria-label={"删除 " + c.name} disabled={busy === c.id} onClick={() => void remove(c)}><Trash2 /></Button>
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
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const body = { name: name.trim(), format, content };
      await (c ? api("/admin/misub/templates/" + c.id, "PATCH", body) : api("/admin/misub/templates", "POST", body));
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="xl">
        <DialogTitle>{c ? "编辑模板" : "新建模板"}</DialogTitle>
        <DialogDescription>ini 模板用 custom_proxy_group= 与 ruleset= 定义；Clash YAML 需要包含 proxy-groups 与 rules，节点由系统填入。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="名称" htmlFor="tpl-name"><Input id="tpl-name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="格式"><Segmented aria-label="模板格式" value={format} onValueChange={setFormat} options={[["ini", "ini"], ["yaml", "Clash YAML"]] as const} /></Field>
          </div>
          <Field label="内容" htmlFor="tpl-content"><Textarea id="tpl-content" required rows={16} className="font-mono text-xs" value={content} onChange={(e) => setContent(e.target.value)} /></Field>
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function SettingsPanel() {
  const q = useQuery({ queryKey: ["misub-settings"], queryFn: () => api("/admin/misub/settings") as Promise<MisubSettings> });
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />;
  return <SettingsForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />;
}

function SettingsForm({ value, saved }: { value: MisubSettings; saved: () => void }) {
  const toast = useToast();
  const [s, setS] = useState<MisubSettings>(value);
  const [ops, setOps] = useState<Operator[]>(value.default_operators ?? []);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<MisubSettings>) => setS({ ...s, ...patch });
  async function submit() {
    setBusy(true);
    setError("");
    try {
      await api("/admin/misub/settings", "PUT", { ...s, default_operators: ops });
      toast("已保存", { tone: "ok" });
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const sw = (k: "flag_emoji" | "prepend_manual" | "prepend_sources" | "prepend_group_name" | "traffic_nodes" | "skip_cert_verify" | "enable_udp", label: string) => (
    <Label className="actions"><Switch aria-label={label} checked={s[k]} onCheckedChange={(v) => set({ [k]: v })} />{label}</Label>
  );
  return (
    <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <SectionHeader title="默认设置" description="订阅组未单独设置的项使用这里的值。" />
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="默认规则模板"><TemplatePicker value={s.template} onChange={(v) => set({ template: v ?? "" })} /></Field>
        <Field label="规则等级"><Segmented aria-label="规则等级" value={s.rule_level || "std"} onValueChange={(v) => set({ rule_level: v })} options={ruleLevels} /></Field>
        <Field label="手动节点前缀" htmlFor="ms-prefix"><Input id="ms-prefix" value={s.manual_prefix} onChange={(e) => set({ manual_prefix: e.target.value })} /></Field>
        <Field label="输出缓存（分钟）" htmlFor="ms-cache" hint="0 表示每次都实时拉取订阅源。"><Input id="ms-cache" inputMode="numeric" value={String(s.cache_minutes)} onChange={(e) => set({ cache_minutes: Number(e.target.value.replace(/\D/g, "")) || 0 })} /></Field>
        <Field label="客户端更新间隔（小时）" htmlFor="ms-interval"><Input id="ms-interval" inputMode="numeric" value={String(s.update_interval)} onChange={(e) => set({ update_interval: Number(e.target.value.replace(/\D/g, "")) || 24 })} /></Field>
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        {sw("flag_emoji", "地区旗帜")}
        {sw("prepend_manual", "手动节点加前缀")}
        {sw("prepend_sources", "订阅源节点加源名称")}
        {sw("prepend_group_name", "手动节点加分组名")}
        {sw("traffic_nodes", "置顶流量 / 到期提示节点")}
        {sw("enable_udp", "输出中开启 UDP")}
        {sw("skip_cert_verify", "输出中跳过证书校验（不推荐）")}
      </div>
      <Field label="全局只保留（每行一条）" htmlFor="ms-inc"><Textarea id="ms-inc" rows={2} value={s.filter_include} onChange={(e) => set({ filter_include: e.target.value })} /></Field>
      <Field label="全局排除（每行一条）" htmlFor="ms-exc"><Textarea id="ms-exc" rows={2} value={s.filter_exclude} onChange={(e) => set({ filter_exclude: e.target.value })} /></Field>
      <details>
        <summary className="cursor-pointer text-sm">默认处理链（{ops.length} 步；订阅组没有自己的处理链时使用）</summary>
        <div className="mt-2"><OperatorEditor value={ops} change={setOps} /></div>
      </details>
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="actions"><Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存默认设置"}</Button></div>
    </form>
  );
}
