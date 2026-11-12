// 订阅组：选订阅源与手动节点（或全部），配处理链与规则模板；预览、分配给用户、匿名分享。
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { Badge, Button, CheckboxField, CodeBlock, Collapsible, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, PageState, Readout, SectionHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SortableList, SwitchField, Textarea, useToast, useConfirm, ScrollArea } from "@weir/ui";
import { Eye, Plus, Share2, Trash2, Users } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { fromLocalInput, localInput, type UserItem } from "../lib/core";
import { ruleLevels, type BuiltinTemplate, type CustomTemplate, type Profile, type ProfileSettings } from "../lib/subscriptions";
import { OperatorEditor, type Operator } from "./operator-editor";
import { useSources } from "./subscription-sources";
import { useManualNodes, nodeName } from "./subscription-nodes";
import { State } from "./feedback";
import { SubCheckBadge } from "./sub-check";
import { dataObject, useDialogParam } from "./object-link";
import { field, useConsoleContext } from "../lib/console-context";
import { RenderingSettingsFields, renderingErrorFields } from "../features/subscriptions/settings/rendering-settings";
import { ProfileProbe } from "../features/subscriptions/probes/profile-probe";

type Member = NonNullable<Profile["members"]>[number];

export function useTemplates() {
  return useQuery({ queryKey: ["subscription-templates"], queryFn: () => api("/admin/subscriptions/templates") as Promise<{ builtin: BuiltinTemplate[]; custom: CustomTemplate[] }> });
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
  const q = useQuery({ queryKey: ["subscription-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as Profile[] });
  const [params, setParams] = useSearchParams();
  const editParam = useDialogParam("profile", q.data, !q.isFetching && !q.error);
  const edit = params.get("create") === "profile" ? "new" : editParam.current;
  const setEdit = (value: Profile | "new" | null) => setParams((next) => {
    next.delete("profile"); next.delete("create");
    if (value === "new") next.set("create", "profile");
    else if (value) next.set("profile", String(value.id));
    return next;
  }, { replace: value === null });
  const [preview, setPreview] = useState<Profile | null>(null);
  const [probe, setProbe] = useState<Profile | null>(null);
  const [share, setShare] = useState<Profile | null>(null);
  const [grants, setGrants] = useState<Profile | null>(null);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  async function remove(p: Profile) {
    if (!(await confirm({ title: `删除订阅组「${p.name}」？`, description: "分配给用户的地址与分享地址都会失效。", confirmText: "删除", tone: "danger" }))) return;
    await run(p.id, () => api("/admin/subscriptions/profiles/" + p.id, "DELETE"), "订阅组已删除");
  }
  const done = (msg: string) => { toast(msg, { tone: "ok" }); void q.refetch(); };
  useConsoleContext({ tab: "订阅组", list: { total: q.data?.length ?? 0, items: (q.data ?? []).slice(0, 20).map((p) => ({ type: "profile", name: p.name, id: p.id })) } });
  return (
    <section className="stack">
      <SectionHeader title="订阅组" description="把订阅源与手动节点组合成一份订阅；可分配给用户（各自独立地址）或匿名分享。外部节点的流量不计入用户额度。" actions={<Button size="sm" onClick={() => setEdit("new")}><Plus />新建订阅组</Button>} />
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<SkeletonList rows={3} lines={2} className="stack" rowClassName="flex items-center justify-between gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3" />}
        />
      ) : q.data.length === 0 ? (
        <State empty="还没有订阅组" detail="订阅组把订阅源和手动节点组合、筛选后分给用户或公开分享。" />
      ) : (
        <ul className="stack">
          {q.data.map((p) => (
            <li key={p.id} {...dataObject({ type: "profile", id: p.id })} className="flex flex-wrap items-center justify-between gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
              <span className="min-w-0">
                <strong>{p.name}</strong> {!p.enabled && <Badge variant="soft" tone="warn">停用</Badge>} {p.shared && <Badge variant="soft" tone="ok">已分享</Badge>}
                <small className="block text-[var(--weir-ink-3)]">
                  {p.all_sources ? "全部订阅源与手动节点" : p.members ? `${p.members.length} 个有序成员（含 ${p.members.filter((m) => m.kind === "profile").length} 个子组）` : `${p.source_ids?.length ?? 0} 个订阅源 · ${p.node_ids?.length ?? 0} 个手动节点`} · {p.user_ids.length} 个用户 · 下载 {p.downloads} 次{p.expires_at ? ` · 到期 ${date(p.expires_at)}` : ""}
                </small>
              </span>
              <span className="flex flex-wrap gap-1.5">
                <Button size="sm" variant="outline" onClick={() => setPreview(p)}><Eye />预览</Button>
                <Button size="sm" variant="outline" onClick={() => setProbe(p)}>实测</Button>
                <Button size="sm" variant="outline" onClick={() => setGrants(p)}><Users />用户</Button>
                <Button size="sm" variant="outline" onClick={() => setShare(p)}><Share2 />分享</Button>
                <Button size="sm" variant="outline" onClick={() => setEdit(p)}>编辑</Button>
                <Button size="icon-sm" variant="destructive" aria-label={"删除 " + p.name} disabled={isBusy()} busy={isBusy(p.id)} busyText="正在删除" onClick={() => void remove(p)}><Trash2 /></Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {edit && <ProfileForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); done("已保存"); }} />}
      {preview && <ProfilePreview profile={preview} close={() => setPreview(null)} />}
      {probe && <ProfileProbe profile={probe} close={() => setProbe(null)} />}
      {share && <ShareDialog profile={share} close={() => setShare(null)} changed={() => void q.refetch()} />}
      {grants && <GrantDialog profile={grants} close={() => setGrants(null)} saved={() => { setGrants(null); done("用户分配已保存"); }} />}
    </section>
  );
}

export function ProfileDetail({ id, tab }: { id: string; tab: string }) {
  const navigate = useNavigate();
  const [, setParams] = useSearchParams();
  const confirm = useConfirm();
  const toast = useToast();
  const q = useQuery({ queryKey: ["subscription-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as Profile[] });
  const [edit, setEdit] = useState(false);
  const [preview, setPreview] = useState(false);
  const [share, setShare] = useState(false);
  const [grants, setGrants] = useState(false);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  const item = q.data?.find((p) => String(p.id) === id);
  if (q.isPending) return <State loading />;
  if (q.error) return <State error={q.error} onRetry={() => void q.refetch()} />;
  if (!item) return <PageState kind="empty" title="没有这个订阅组" detail="它可能已被删除，或你没有权限查看。" action={<Link to="/subscriptions?tab=profiles">返回订阅组</Link>} />;
  async function remove() {
    if (!(await confirm({ title: `删除订阅组「${item!.name}」？`, description: "分配给用户的地址与分享地址都会失效。", confirmText: "删除", tone: "danger" }))) return;
    await run(item!.id, () => api("/admin/subscriptions/profiles/" + item!.id, "DELETE"), "订阅组已删除");
    navigate("/subscriptions?tab=profiles");
  }
  const members = item.members?.length ?? ((item.source_ids?.length ?? 0) + (item.node_ids?.length ?? 0));
  return (
    <section className="stack">
      <div className="flex flex-wrap gap-1.5">
        <Button size="sm" variant="outline" onClick={() => setPreview(true)}><Eye />预览</Button>
        <Button size="sm" variant="outline" onClick={() => setGrants(true)}><Users />用户</Button>
        <Button size="sm" variant="outline" onClick={() => setShare(true)}><Share2 />分享</Button>
        <Button size="sm" variant="outline" onClick={() => setEdit(true)}>编辑</Button>
        <Button size="sm" variant="destructive" disabled={isBusy()} busy={isBusy(item.id)} busyText="正在删除" onClick={() => void remove()}><Trash2 />删除</Button>
      </div>
      {tab === "probe" ? <ProfileProbe profile={item} close={() => setParams((p) => { p.set("tab", "overview"); return p; }, { replace: true })} /> : (
        <div className="grid gap-3 sm:grid-cols-3">
          <Readout label="状态" value={item.enabled ? "启用" : "已停用"} />
          <Readout label="成员" value={item.all_sources ? "全部订阅源与手动节点" : members} />
          <Readout label="用户" value={item.user_ids?.length ?? null} />
          <Readout label="下载" value={item.downloads ?? null} />
          <Readout label="到期" value={item.expires_at ? date(item.expires_at) : null} />
          <Readout label="分享" value={item.shared ? (item.share_expires_at ? `到期 ${date(item.share_expires_at)}` : "已分享") : "未分享"} />
        </div>
      )}
      {edit && <ProfileForm value={item} close={() => setEdit(false)} saved={() => { setEdit(false); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {preview && <ProfilePreview profile={item} close={() => setPreview(false)} />}
      {share && <ShareDialog profile={item} close={() => setShare(false)} changed={() => void q.refetch()} />}
      {grants && <GrantDialog profile={item} close={() => setGrants(false)} saved={() => { setGrants(false); toast("用户分配已保存", { tone: "ok" }); void q.refetch(); }} />}
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
  const profiles = useQuery({ queryKey: ["subscription-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as Profile[] });
  const [members, setMembers] = useState<Member[]>(p?.members ?? [...(p?.node_ids ?? []).map((id) => ({ kind: "node" as const, id })), ...(p?.source_ids ?? []).map((id) => ({ kind: "source" as const, id }))]);
  const [settings, setSettings] = useState<ProfileSettings>(p?.settings ?? {});
  const [ops, setOps] = useState<Operator[]>(p?.operators ?? []);
  const [expiry, setExpiry] = useState(localInput(p?.expires_at));
  const loadingMembers = sources.isPending || nodes.isPending || profiles.isPending;
  const membersError = sources.error || nodes.error || profiles.error;
  const toggle = (kind: Member["kind"], id: number, on: boolean) => setMembers((current) => on ? [...current, { kind, id }] : current.filter((m) => m.kind !== kind || m.id !== id));
  const chosen = (kind: Member["kind"], id: number) => members.some((m) => m.kind === kind && m.id === id);
  const memberName = (m: Member) => {
    if (m.kind === "profile") return profiles.data?.find((p) => p.id === m.id)?.name ?? `订阅组 #${m.id}`;
    if (m.kind === "source") return sources.data?.items.find((s) => s.id === m.id)?.name || `订阅源 #${m.id}`;
    const node = nodes.data?.items.find((n) => n.id === m.id);
    return node ? nodeName(node) : `手动节点 #${m.id}`;
  };
  const setS = (patch: Partial<ProfileSettings>) => {
    const next = { ...settings, ...patch };
    for (const k of Object.keys(patch) as (keyof ProfileSettings)[]) if (patch[k] === undefined) delete next[k];
    setSettings(next);
  };
  const save = useFormAction(
    () => {
      const body = { name: name.trim(), enabled, all_sources: all, members: all ? null : members, settings, operators: ops, expires_at: fromLocalInput(expiry) };
      return p ? api("/admin/subscriptions/profiles/" + p.id, "PATCH", body) : api("/admin/subscriptions/profiles", "POST", body);
    },
    { fields: { invalid_template: "template", invalid_profile_members: "members", ...renderingErrorFields }, onSuccess: saved },
  );
  useConsoleContext({ dialog: { title: p ? "编辑订阅组" : "新建订阅组" }, object: p ? { type: "profile", name: p.name, id: p.id } : undefined, form: [field("name", "名称", name), field("enabled", "启用", enabled ? "是" : "否"), field("all_sources", "全部订阅源", all ? "是" : "否"), field("members", "有序成员", members.map(memberName).join("、")), field("expires_at", "到期", expiry || "长期")] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="xl">
        <DialogTitle>{p ? "编辑订阅组" : "新建订阅组"}</DialogTitle>
        <DialogDescription>勾选订阅源、手动节点或其他订阅组，再拖动调整顺序；子组最多嵌套三层。未单独设置的项沿用默认设置。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="名称" name="name" required hint="也是客户端里显示的配置名。"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="到期时间（可选）" hint="到期后只输出一个「订阅已到期」提示节点。"><Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} /></Field>
          </FormGrid>
          <SwitchField label="包含全部已启用的订阅源与手动节点" checked={all} onCheckedChange={setAll} />
          {!all && (loadingMembers || membersError) && <State loading={loadingMembers} error={membersError} onRetry={()=>{ void sources.refetch(); void nodes.refetch(); void profiles.refetch(); }} />}
          {!all && (
            <FormGrid>
              <fieldset className="grid min-w-0 content-start gap-2">
                <legend className="mb-1 text-meta font-medium text-[var(--weir-ink-2)]">订阅源</legend>
                <ScrollArea className="max-h-56" viewportClassName="weir-focus-gutter" contentClassName="grid content-start gap-2">
                  {(sources.data?.items ?? []).map((s) => (
                    <CheckboxField key={s.id} label={s.name || s.url} disabled={!s.enabled && !chosen("source", s.id)} checked={chosen("source", s.id)} onCheckedChange={(v) => toggle("source", s.id, v)} />
                  ))}
                  {sources.data?.items.length === 0 && <small>还没有订阅源。</small>}
                </ScrollArea>
              </fieldset>
              <fieldset className="grid min-w-0 content-start gap-2">
                <legend className="mb-1 text-meta font-medium text-[var(--weir-ink-2)]">手动节点</legend>
                <ScrollArea className="max-h-56" viewportClassName="weir-focus-gutter" contentClassName="grid content-start gap-2">
                  {(nodes.data?.items ?? []).map((n) => (
                    <CheckboxField
                      key={n.id}
                      label={<>{nodeName(n)}{n.group && <small className="text-[var(--weir-ink-3)]">（{n.group}）</small>}</>}
                      disabled={!n.enabled && !chosen("node", n.id)} checked={chosen("node", n.id)}
                      onCheckedChange={(v) => toggle("node", n.id, v)}
                    />
                  ))}
                  {nodes.data?.items.length === 0 && <small>还没有手动节点。</small>}
                </ScrollArea>
              </fieldset>
            </FormGrid>
          )}
          {!all && <fieldset className="grid min-w-0 gap-2"><legend className="text-meta font-medium text-[var(--weir-ink-2)]">引用其他订阅组</legend>
            {(profiles.data ?? []).filter((item) => item.id !== p?.id).map((item) => <CheckboxField key={item.id} label={item.name} disabled={!item.enabled && !chosen("profile", item.id)} checked={chosen("profile", item.id)} onCheckedChange={(v) => toggle("profile", item.id, v)} />)}
          </fieldset>}
          {!all && <Field label="输出顺序" name="members"><SortableList label="订阅组成员顺序" items={members} onReorder={setMembers} getId={(m) => `${m.kind}:${m.id}`} itemLabel={(m) => memberName(m)} renderItem={(m, { reorder }) => <div className="flex items-center justify-between gap-2"><span>{memberName(m)}</span>{reorder}</div>} /></Field>}
          <FormGrid>
            <Field label="规则模板" name="template"><TemplatePicker inherit value={settings.template} onChange={(v) => setS({ template: v })} /></Field>
            <Field label="规则等级">
              <Segmented aria-label="规则等级" value={settings.rule_level ?? "__inherit"} onValueChange={(v) => setS({ rule_level: v === "__inherit" ? undefined : v })}
                options={[["__inherit", "默认"], ...ruleLevels] as const} />
            </Field>
          </FormGrid>
          <Field label="转换引擎" name="engine"><Segmented aria-label="转换引擎" value={settings.engine ?? "__inherit"} onValueChange={(v) => setS({ engine: v === "__inherit" ? undefined : v })} options={[["__inherit", "默认"], ["builtin", "内置"], ["external", "外部"]] as const} /></Field>
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
          <RenderingSettingsFields inherit value={settings} change={setS} />
          <Collapsible title={`订阅组处理链（${ops.length} 步；为空时使用默认处理链）`}>
            <OperatorEditor value={ops} change={setOps} />
          </Collapsible>
          {p && <SwitchField label="启用" checked={enabled} onCheckedChange={setEnabled} />}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy || (!all && (loadingMembers || !!membersError))} busy={save.busy} busyText="正在保存…">保存</Button>
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
    queryKey: ["subscription-preview", profile.id, target],
    queryFn: () => api(`/admin/subscriptions/profiles/${profile.id}/preview`, "POST", { target }) as Promise<{ nodes: string[]; sources: { SourceID: number; Nodes: number; FromCache: boolean; Error: string }[] | null; output?: string; output_error?: string; check?: { status: "passed" | "failed" | "unknown"; kernel: string; version?: string; error?: string } }>,
  });
  useConsoleContext({ object: { type: "profile", name: profile.name, id: profile.id }, dialog: { title: "预览订阅组", step: target } });
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
                {q.data.check.error && <small className={q.data.check.status === "failed" ? "text-[var(--weir-bad-ink)]" : "text-[var(--weir-ink-3)]"}>{q.data.check.error}</small>}
              </div>
            )}
            {q.data.output_error ? <Notice tone="bad">{q.data.output_error}</Notice> : <CodeBlock className="block max-h-[55vh] overflow-auto whitespace-pre text-meta">{q.data.output}</CodeBlock>}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ShareDialog({ profile, close, changed }: { profile: Profile; close: () => void; changed: () => void }) {
  const confirm = useConfirm();
  const [days, setDays] = useState("7");
  const [url, setUrl] = useState("");
  const share = useFormAction(() => api(`/admin/subscriptions/profiles/${profile.id}/share`, "POST", { days: Number(days) }) as Promise<{ url: string }>, {
    fields: { invalid_share_days: "days" },
    onSuccess: (r) => {
      setUrl(r.url);
      changed();
    },
  });
  const revoke = useFormAction(() => api(`/admin/subscriptions/profiles/${profile.id}/share`, "DELETE"), {
    ok: "分享已撤下",
    onSuccess: () => {
      changed();
      close();
    },
  });
  const busy = share.busy || revoke.busy;
  useConsoleContext({ object: { type: "profile", name: profile.name, id: profile.id }, dialog: { title: "匿名分享" }, form: [field("days", "有效天数", days)] });
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>匿名分享 · {profile.name}</DialogTitle>
        <DialogDescription>任何拿到地址的人都能下载这份订阅。重新生成会让旧地址失效；已下载到客户端的节点无法随撤下收回。</DialogDescription>
        {url ? (
          <div className="stack">
            <CopyField label="分享地址" value={url} />
            <Notice role="status">地址只显示这一次，请现在复制。</Notice>
            <DialogFooter><Button onClick={close}>完成</Button></DialogFooter>
          </div>
        ) : (
          <Form busy={busy} errors={share.errors} onSubmit={() => void share.submit()}>
            {profile.shared && <Notice tone="warn">当前已分享，{profile.share_expires_at ? `到期 ${date(profile.share_expires_at)}` : ""}。重新生成会替换旧地址。</Notice>}
            <Field label="有效天数" name="days" customError={days && (Number(days) < 1 || Number(days) > 365) ? "有效期须为 1–365 天" : ""}>
              <Input inputMode="numeric" required value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))} />
            </Field>
            {(share.error || revoke.error) && <Notice tone="bad">{share.error || revoke.error}</Notice>}
            <DialogFooter>
              {profile.shared && (
                <Button
                  type="button"
                  variant="destructive"
                  disabled={busy}
                  busy={revoke.busy}
                  busyText="正在撤下…"
                  onClick={async () => (await confirm({ title: "撤下分享？", description: "分享地址立即失效；已下载到客户端的节点无法收回。", confirmText: "撤下", tone: "danger" })) && void revoke.submit()}
                >
                  撤下分享
                </Button>
              )}
              <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
              <Button type="submit" disabled={busy} busy={share.busy} busyText="正在生成…">{profile.shared ? "重新生成地址" : "生成分享地址"}</Button>
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
  const save = useFormAction(() => api(`/admin/subscriptions/profiles/${profile.id}/grants`, "PUT", { user_ids: picked }), { onSuccess: saved });
  useConsoleContext({ object: { type: "profile", name: profile.name, id: profile.id }, dialog: { title: "分配用户" }, form: [field("user_ids", "勾选的用户", (users.data ?? []).filter((u) => picked.includes(u.id)).map((u) => u.email).join("、"))] });
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
            <fieldset className="grid min-w-0 content-start gap-2">
              <legend className="sr-only">用户</legend>
              <ScrollArea className="max-h-80" viewportClassName="weir-focus-gutter" contentClassName="grid content-start gap-2">
                {users.data.map((u) => (
                  <CheckboxField
                    key={u.id}
                    label={u.name || u.email}
                    description={u.name ? u.email : undefined}
                    checked={picked.includes(u.id)}
                    onCheckedChange={(v) => setPicked(v ? [...picked, u.id] : picked.filter((x) => x !== u.id))}
                  />
                ))}
              </ScrollArea>
            </fieldset>
          )}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy || users.isPending || !!users.error} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
