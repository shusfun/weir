// 订阅组：选订阅源与手动节点（或全部），配处理链与规则模板；预览、分配给用户、匿名分享。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, CheckboxField, CodeBlock, Collapsible, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, SectionHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Textarea, useToast, useConfirm, PasswordInput } from "@weir/ui";
import { Eye, Plus, Share2, Trash2, Users } from "lucide-react";
import { api, reauthFields } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { fromLocalInput, localInput, type UserItem } from "../lib/core";
import { ruleLevels, type BuiltinTemplate, type CustomTemplate, type Profile, type ProfileSettings } from "../lib/misub";
import { OperatorEditor, type Operator } from "./operator-editor";
import { useSources } from "./misub-sources";
import { useManualNodes } from "./misub-nodes";
import { State } from "./feedback";
import { SubCheckBadge } from "./sub-check";

export function useTemplates() {
  return useQuery({ queryKey: ["misub-templates"], queryFn: () => api("/admin/misub/templates") as Promise<{ builtin: BuiltinTemplate[]; custom: CustomTemplate[] }> });
}

// TemplatePicker 选择规则模板：跟随默认 / 不使用 / 内置 / 自定义 / 远程地址。放在 Field 里，标签即下拉框名称。
export function TemplatePicker({ value, onChange, inherit }: { value: string | undefined; onChange: (v: string | undefined) => void; inherit?: boolean }) {
  const t = useTemplates();
  const remote = value?.startsWith("remote:");
  const options: [string, string][] = [
    ...(inherit ? [["__inherit", "跟随默认设置"] as [string, string]] : []),
    ["", "不使用模板（内置生成器）"],
    ...(t.data?.builtin ?? []).map((b) => ["builtin:" + b.id, "内置：" + b.name] as [string, string]),
    ...(t.data?.custom ?? []).map((c) => ["custom:" + c.id, "自定义：" + c.name] as [string, string]),
    ["__remote", "远程模板地址…"],
  ];
  const current = value === undefined ? "__inherit" : remote ? "__remote" : value;
  return (
    <div className="stack">
      <Select value={current} onValueChange={(v) => onChange(v === "__inherit" ? undefined : v === "__remote" ? "remote:" : (v as string))}>
        <SelectTrigger><SelectValue>{options.find(([k]) => k === current)?.[1] ?? "请选择"}</SelectValue></SelectTrigger>
        <SelectContent>{options.map(([k, l]) => <SelectItem key={k || "_none"} value={k}>{l}</SelectItem>)}</SelectContent>
      </Select>
      {remote && <Input aria-label="远程模板地址" placeholder="https://example.com/template.ini" value={value!.slice(7)} onChange={(e) => onChange("remote:" + e.target.value.trim())} />}
    </div>
  );
}

export function ProfilesPanel() {
  const confirm = useConfirm();
  const toast = useToast();
  const q = useQuery({ queryKey: ["misub-profiles"], queryFn: async () => (await api("/admin/misub/profiles")).items as Profile[] });
  const [edit, setEdit] = useState<Profile | "new" | null>(null);
  const [preview, setPreview] = useState<Profile | null>(null);
  const [share, setShare] = useState<Profile | null>(null);
  const [grants, setGrants] = useState<Profile | null>(null);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  async function remove(p: Profile) {
    if (!(await confirm({ title: `删除订阅组「${p.name}」？`, description: "分配给用户的地址与分享地址都会失效。", confirmText: "删除", tone: "danger" }))) return;
    await run(p.id, () => api("/admin/misub/profiles/" + p.id, "DELETE"), "订阅组已删除");
  }
  const done = (msg: string) => { toast(msg, { tone: "ok" }); void q.refetch(); };
  return (
    <section className="stack">
      <SectionHeader title="订阅组" description="把订阅源与手动节点组合成一份订阅；可分配给用户（各自独立地址）或匿名分享。外部节点的流量不计入用户额度。" actions={<Button size="sm" onClick={() => setEdit("new")}><Plus />新建订阅组</Button>} />
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<SkeletonList rows={3} lines={2} className="stack" rowClassName="flex items-center justify-between gap-2 rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" />}
        />
      ) : q.data.length === 0 ? (
        <State empty="还没有订阅组" />
      ) : (
        <ul className="stack">
          {q.data.map((p) => (
            <li key={p.id} className="flex flex-wrap items-center justify-between gap-2 rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
              <span className="min-w-0">
                <strong>{p.name}</strong> {!p.enabled && <Badge variant="soft" tone="warn">停用</Badge>} {p.shared && <Badge variant="soft" tone="ok">已分享</Badge>}
                <small className="block text-[var(--mg-ink-3)]">
                  {p.all_sources ? "全部订阅源与手动节点" : `${p.source_ids?.length ?? 0} 个订阅源 · ${p.node_ids?.length ?? 0} 个手动节点`} · {p.user_ids.length} 个用户 · 下载 {p.downloads} 次{p.expires_at ? ` · 到期 ${date(p.expires_at)}` : ""}
                </small>
              </span>
              <span className="flex flex-wrap gap-1.5">
                <Button size="sm" variant="outline" onClick={() => setPreview(p)}><Eye />预览</Button>
                <Button size="sm" variant="outline" onClick={() => setGrants(p)}><Users />用户</Button>
                <Button size="sm" variant="outline" onClick={() => setShare(p)}><Share2 />分享</Button>
                <Button size="sm" variant="outline" onClick={() => setEdit(p)}>编辑</Button>
                <Button size="icon-sm" variant="destructive" aria-label={"删除 " + p.name} disabled={isBusy(p.id)} onClick={() => void remove(p)}><Trash2 /></Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {edit && <ProfileForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); done("已保存"); }} />}
      {preview && <ProfilePreview profile={preview} close={() => setPreview(null)} />}
      {share && <ShareDialog profile={share} close={() => setShare(null)} changed={() => void q.refetch()} />}
      {grants && <GrantDialog profile={grants} close={() => setGrants(null)} saved={() => { setGrants(null); done("用户分配已保存"); }} />}
    </section>
  );
}

function ProfileForm({ value, close, saved }: { value: Profile | "new"; close: () => void; saved: () => void }) {
  const p = value === "new" ? undefined : value;
  const sources = useSources();
  const nodes = useManualNodes();
  const [name, setName] = useState(p?.name ?? "");
  const [enabled, setEnabled] = useState(p?.enabled ?? true);
  const [all, setAll] = useState(p?.all_sources ?? false);
  const [sourceIds, setSourceIds] = useState<number[]>(p?.source_ids ?? []);
  const [nodeIds, setNodeIds] = useState<number[]>(p?.node_ids ?? []);
  const [settings, setSettings] = useState<ProfileSettings>(p?.settings ?? {});
  const [ops, setOps] = useState<Operator[]>(p?.operators ?? []);
  const [expiry, setExpiry] = useState(localInput(p?.expires_at));
  const toggle = (list: number[], id: number, on: boolean) => (on ? [...list, id] : list.filter((x) => x !== id));
  const setS = (patch: Partial<ProfileSettings>) => {
    const next = { ...settings, ...patch };
    for (const k of Object.keys(patch) as (keyof ProfileSettings)[]) if (patch[k] === undefined) delete next[k];
    setSettings(next);
  };
  const save = useFormAction(
    () => {
      const body = { name: name.trim(), enabled, all_sources: all, source_ids: all ? [] : sourceIds, node_ids: all ? [] : nodeIds, settings, operators: ops, expires_at: fromLocalInput(expiry) };
      return p ? api("/admin/misub/profiles/" + p.id, "PATCH", body) : api("/admin/misub/profiles", "POST", body);
    },
    { fields: { invalid_template: "template" }, onSuccess: saved },
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="xl">
        <DialogTitle>{p ? "编辑订阅组" : "新建订阅组"}</DialogTitle>
        <DialogDescription>节点按这里勾选的顺序输出：先手动节点，再各订阅源。未单独设置的项沿用默认设置。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="名称" hint="也是客户端里显示的配置名。"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="到期时间（可选）" hint="到期后只输出一个「订阅已到期」提示节点。"><Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} /></Field>
          </FormGrid>
          <SwitchField label="包含全部已启用的订阅源与手动节点" checked={all} onCheckedChange={setAll} />
          {!all && (
            <FormGrid>
              <fieldset className="mg-focus-gutter grid max-h-56 content-start gap-2 overflow-y-auto">
                <legend className="mb-1 text-[12.5px] font-medium text-[var(--mg-ink-2)]">订阅源</legend>
                {(sources.data?.items ?? []).map((s) => (
                  <CheckboxField key={s.id} label={s.name || s.url} checked={sourceIds.includes(s.id)} onCheckedChange={(v) => setSourceIds(toggle(sourceIds, s.id, v))} />
                ))}
                {sources.data?.items.length === 0 && <small>还没有订阅源。</small>}
              </fieldset>
              <fieldset className="mg-focus-gutter grid max-h-56 content-start gap-2 overflow-y-auto">
                <legend className="mb-1 text-[12.5px] font-medium text-[var(--mg-ink-2)]">手动节点</legend>
                {(nodes.data?.items ?? []).map((n) => (
                  <CheckboxField
                    key={n.id}
                    label={<>{n.name || decodeURIComponent(n.url.split("#")[1] ?? "") || n.id}{n.group && <small className="text-[var(--mg-ink-3)]">（{n.group}）</small>}</>}
                    checked={nodeIds.includes(n.id)}
                    onCheckedChange={(v) => setNodeIds(toggle(nodeIds, n.id, v))}
                  />
                ))}
                {nodes.data?.items.length === 0 && <small>还没有手动节点。</small>}
              </fieldset>
            </FormGrid>
          )}
          <FormGrid>
            <Field label="规则模板" name="template"><TemplatePicker inherit value={settings.template} onChange={(v) => setS({ template: v })} /></Field>
            <Field label="规则等级">
              <Segmented aria-label="规则等级" value={settings.rule_level ?? "__inherit"} onValueChange={(v) => setS({ rule_level: v === "__inherit" ? undefined : v })}
                options={[["__inherit", "默认"], ...ruleLevels] as const} />
            </Field>
          </FormGrid>
          <Field label="只保留（每行一条）"><Textarea rows={2} value={settings.filter_include ?? ""} onChange={(e) => setS({ filter_include: e.target.value || undefined })} /></Field>
          <Field label="排除（每行一条）"><Textarea rows={2} value={settings.filter_exclude ?? ""} onChange={(e) => setS({ filter_exclude: e.target.value || undefined })} /></Field>
          <FormGrid>
            {([["flag_emoji", "地区旗帜"], ["prepend_manual", "手动节点加前缀"], ["prepend_sources", "订阅源节点加源名称"], ["traffic_nodes", "置顶流量 / 到期提示"]] as const).map(([k, label]) => (
              <Field key={k} label={label}>
                <Segmented aria-label={label} value={settings[k] === undefined ? "inherit" : settings[k] ? "on" : "off"} onValueChange={(v) => setS({ [k]: v === "inherit" ? undefined : v === "on" })}
                  options={[["inherit", "默认"], ["on", "开"], ["off", "关"]] as const} />
              </Field>
            ))}
          </FormGrid>
          <Collapsible title={`订阅组处理链（${ops.length} 步；为空时使用默认处理链）`}>
            <OperatorEditor value={ops} change={setOps} />
          </Collapsible>
          {p && <SwitchField label="启用" checked={enabled} onCheckedChange={setEnabled} />}
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy}>{save.busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

const targets = [["clash", "Clash"], ["singbox", "sing-box"], ["surge", "Surge"], ["loon", "Loon"], ["quanx", "QuanX"], ["egern", "Egern"], ["base64", "Base64"]] as const;

function ProfilePreview({ profile, close }: { profile: Profile; close: () => void }) {
  const [target, setTarget] = useState<(typeof targets)[number][0]>("clash");
  const q = useQuery({
    queryKey: ["misub-preview", profile.id, target],
    queryFn: () => api(`/admin/misub/profiles/${profile.id}/preview`, "POST", { target }) as Promise<{ nodes: string[]; sources: { SourceID: number; Nodes: number; FromCache: boolean; Error: string }[] | null; output?: string; output_error?: string; check?: { status: "passed" | "failed" | "unknown"; kernel: string; version?: string; error?: string } }>,
  });
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="xl">
        <DialogTitle>预览 · {profile.name}</DialogTitle>
        <DialogDescription>实时拉取订阅源（不使用输出缓存）并按所选格式生成。</DialogDescription>
        <Segmented aria-label="输出格式" value={target} onValueChange={setTarget} options={targets} />
        {q.isPending || q.error ? (
          <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
        ) : (
          <div className="stack">
            <small>{q.data.nodes.length} 个节点{(q.data.sources ?? []).filter((s) => s.Error).map((s) => ` · 源 #${s.SourceID}：${s.Error}${s.FromCache ? "（用了缓存）" : ""}`).join("")}</small>
            {q.data.check && (
              <div className="flex flex-wrap items-center gap-2">
                <SubCheckBadge check={q.data.check} />
                {q.data.check.error && <small className={q.data.check.status === "failed" ? "text-[var(--mg-bad-ink)]" : "text-[var(--mg-ink-3)]"}>{q.data.check.error}</small>}
              </div>
            )}
            {q.data.output_error ? <Notice tone="bad">{q.data.output_error}</Notice> : <CodeBlock className="block max-h-[55vh] overflow-auto whitespace-pre text-xs">{q.data.output}</CodeBlock>}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ShareDialog({ profile, close, changed }: { profile: Profile; close: () => void; changed: () => void }) {
  const confirm = useConfirm();
  const [days, setDays] = useState("7");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [url, setUrl] = useState("");
  const share = useFormAction(() => api(`/admin/misub/profiles/${profile.id}/share`, "POST", { days: Number(days), password, code }) as Promise<{ url: string }>, {
    fields: { ...reauthFields, invalid_share_days: "days" },
    onSuccess: (r) => {
      setUrl(r.url);
      changed();
    },
  });
  const revoke = useFormAction(() => api(`/admin/misub/profiles/${profile.id}/share`, "DELETE"), {
    ok: "分享已撤下",
    onSuccess: () => {
      changed();
      close();
    },
  });
  const busy = share.busy || revoke.busy;
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>匿名分享 · {profile.name}</DialogTitle>
        <DialogDescription>任何拿到地址的人都能下载这份订阅。重新生成会让旧地址失效；已下载到客户端的节点无法随撤下收回。</DialogDescription>
        {url ? (
          <div className="stack">
            <div className="subscription-url"><Input readOnly className="font-mono text-xs" aria-label="分享地址" value={url} /><CopyButton value={url}>复制</CopyButton></div>
            <Notice role="status">地址只显示这一次，请现在复制。</Notice>
            <DialogFooter><Button onClick={close}>完成</Button></DialogFooter>
          </div>
        ) : (
          <Form busy={busy} errors={share.errors} onSubmit={() => void share.submit()}>
            {profile.shared && <Notice tone="warn">当前已分享，{profile.share_expires_at ? `到期 ${date(profile.share_expires_at)}` : ""}。重新生成会替换旧地址。</Notice>}
            <FormGrid cols={3}>
              <Field label="有效天数" name="days" customError={days && (Number(days) < 1 || Number(days) > 365) ? "有效期须为 1–365 天" : ""}>
                <Input inputMode="numeric" required value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))} />
              </Field>
              <Field label="当前密码" name="password"><PasswordInput autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
              <Field label="二次验证码" name="code"><Input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} /></Field>
            </FormGrid>
            {(share.error || revoke.error) && <Notice tone="bad">{share.error || revoke.error}</Notice>}
            <DialogFooter>
              {profile.shared && (
                <Button
                  type="button"
                  variant="destructive"
                  disabled={busy}
                  onClick={async () => (await confirm({ title: "撤下分享？", description: "分享地址立即失效；已下载到客户端的节点无法收回。", confirmText: "撤下", tone: "danger" })) && void revoke.submit()}
                >
                  撤下分享
                </Button>
              )}
              <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
              <Button type="submit" disabled={busy}>{share.busy ? "正在生成…" : profile.shared ? "重新生成地址" : "生成分享地址"}</Button>
            </DialogFooter>
          </Form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function GrantDialog({ profile, close, saved }: { profile: Profile; close: () => void; saved: () => void }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const [picked, setPicked] = useState<number[]>(profile.user_ids);
  const save = useFormAction(() => api(`/admin/misub/profiles/${profile.id}/grants`, "PUT", { user_ids: picked }), { onSuccess: saved });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>分配用户 · {profile.name}</DialogTitle>
        <DialogDescription>每个用户在「我的订阅」里看到自己独立的地址；取消勾选后该用户的地址立即失效。</DialogDescription>
        <Form busy={save.busy} onSubmit={() => void save.submit()}>
          {users.isPending || users.error ? (
            <State loading={users.isPending} error={users.error} onRetry={() => users.refetch()} />
          ) : users.data.length === 0 ? (
            <State empty="还没有用户" />
          ) : (
            <fieldset className="mg-focus-gutter grid max-h-80 content-start gap-2 overflow-y-auto">
              <legend className="sr-only">用户</legend>
              {users.data.map((u) => (
                <CheckboxField
                  key={u.id}
                  label={u.name || u.email}
                  description={u.name ? u.email : undefined}
                  checked={picked.includes(u.id)}
                  onCheckedChange={(v) => setPicked(v ? [...picked, u.id] : picked.filter((x) => x !== u.id))}
                />
              ))}
            </fieldset>
          )}
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy || users.isPending || !!users.error}>{save.busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
