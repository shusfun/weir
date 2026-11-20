import { useFormGuard } from "../lib/dirty";
// 订阅组：选来源、挑选条件与规则模板，编辑时实时预览节点；分配给用户、匿名分享。
import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { Badge, Button, CheckboxField, Chip, CodeBlock, Collapsible, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, FormSection, Input, InsetGroup, InsetRow, InsetRows, Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger, Notice, PageState, Readout, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SortableList, StatusLamp, SwitchField, Textarea, useToast, useConfirm, ScrollArea } from "@weir/ui";
import { Activity, Ellipsis, FileCode, Pencil, Plus, Share2, Trash2, Users } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { fromLocalInput, localInput, type UserItem } from "../lib/core";
import { ruleLevels, type BuiltinTemplate, type CustomTemplate, type Profile, type ProfileNodePreview, type ProfilePick, type ProfileSettings } from "../lib/subscriptions";
import { OperatorEditor, type Operator } from "./operator-editor";
import { useSources } from "../lib/subscription-sources";
import { useManualNodes, nodeName } from "./subscription-nodes";
import { State } from "./feedback";
import { SubCheckBadge } from "./sub-check";
import { dataObject, ObjectLink, useDialogParam } from "./object-link";
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
  const detail = useDialogParam("profile_detail", q.data, q.isSuccess);
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
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-meta text-[var(--weir-ink-3)]">{q.data ? `共 ${q.data.length} 个订阅组` : "正在读取订阅组…"}</span>
        <Button size="sm" onClick={() => setEdit("new")}><Plus />新建订阅组</Button>
      </div>
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<SkeletonList rows={3} lines={2} className="stack" />}
        />
      ) : q.data.length === 0 ? (
        <State empty="还没有订阅组" detail="点右上角「新建订阅组」，选好来源和挑选条件后分给用户。" />
      ) : (
        <InsetGroup>
          <InsetRows>
            {q.data.map((p) => (
              <InsetRow key={p.id} {...dataObject({ type: "profile", id: p.id })} className="flex flex-wrap items-center justify-between gap-3">
                <span className="min-w-0">
                  <strong className="flex flex-wrap items-center gap-1.5"><ObjectLink to={{ type: "profile", id: p.id, name: p.name }} /> {!p.enabled && <Badge variant="soft" tone="warn">停用</Badge>}</strong>
                  <small className="mt-0.5 block text-[var(--weir-ink-3)]">
                    <ProfileNodeCount profile={p} /> · {p.user_ids.length} 个用户 · {shareState(p)}{p.expires_at ? ` · 到期 ${date(p.expires_at)}` : ""}
                  </small>
                </span>
                <span className="flex flex-wrap items-center gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => setGrants(p)}><Users />用户</Button>
                  <Button size="sm" variant="outline" onClick={() => setShare(p)}><Share2 />分享</Button>
                  <Menu>
                    <MenuTrigger render={<Button size="icon-sm" variant="ghost" aria-label={"更多操作：" + p.name} disabled={isBusy()} busy={isBusy(p.id)} busyText="正在删除"><Ellipsis /></Button>} />
                    <MenuContent align="end">
                      <MenuItem onClick={() => setEdit(p)}><Pencil />编辑</MenuItem>
                      <MenuItem onClick={() => setPreview(p)}><FileCode />查看生成的配置</MenuItem>
                      <MenuItem onClick={() => setProbe(p)}><Activity />实测</MenuItem>
                      <MenuSeparator />
                      <MenuItem onClick={() => void remove(p)}><Trash2 />删除</MenuItem>
                    </MenuContent>
                  </Menu>
                </span>
              </InsetRow>
            ))}
          </InsetRows>
        </InsetGroup>
      )}
      {detail.current && <Dialog open onOpenChange={o => !o && detail.close()}><DialogContent size="xl"><DialogTitle>{detail.current.name}</DialogTitle><DialogDescription>管理订阅组的用户、分享、配置与实测。</DialogDescription><ProfileDetail key={detail.current.id} id={String(detail.current.id)} tab={params.get("detail_tab") || "overview"} /></DialogContent></Dialog>}
      {edit && <ProfileForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); done("已保存"); }} />}
      {preview && <ProfilePreview profile={preview} close={() => setPreview(null)} />}
      {probe && <ProfileProbe profile={probe} close={() => setProbe(null)} />}
      {share && <ShareDialog profile={share} close={() => setShare(null)} changed={() => void q.refetch()} />}
      {grants && <GrantDialog profile={grants} close={() => setGrants(null)} saved={() => { setGrants(null); done("用户分配已保存"); }} />}
    </section>
  );
}

function shareState(p: Profile) {
  if (!p.shared) return "未分享";
  return p.share_expires_at ? `分享到 ${date(p.share_expires_at)}` : "已分享";
}

// 节点数按各源最新缓存现算：订阅组不保存固定节点清单。
function ProfileNodeCount({ profile }: { profile: Profile }) {
  const q = useQuery({
    queryKey: ["subscription-profile-nodes", profile.id, profile.settings, profile.members, profile.source_ids, profile.node_ids, profile.all_sources, profile.operators],
    queryFn: async () => ((await api("/admin/subscriptions/profiles/preview-nodes", "POST", { id: profile.id })) as ProfileNodePreview).nodes.length,
    staleTime: 60_000,
  });
  if (q.isPending) return <span>节点数统计中</span>;
  if (q.error || q.data === undefined) return <span>节点数未知</span>;
  return <span>{q.data} 个节点</span>;
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
  useConsoleContext({ object: item ? { type: "profile", id: item.id, name: item.name } : undefined, dialog: { title: "订阅组详情" } });
  if (q.isPending) return <State loading />;
  if (q.error) return <State error={q.error} onRetry={() => void q.refetch()} />;
  if (!item) return <PageState kind="empty" title="没有这个订阅组" detail="它可能已被删除，或你没有权限查看。" action={<Link to="/groups?tab=profiles">返回订阅组</Link>} />;
  async function remove() {
    if (!(await confirm({ title: `删除订阅组「${item!.name}」？`, description: "分配给用户的地址与分享地址都会失效。", confirmText: "删除", tone: "danger" }))) return;
    const removed = await run(item!.id, () => api("/admin/subscriptions/profiles/" + item!.id, "DELETE"), "订阅组已删除");
    if (removed) navigate("/groups?tab=profiles");
  }
  const members = item.members?.length ?? ((item.source_ids?.length ?? 0) + (item.node_ids?.length ?? 0));
  return (
    <section className="stack">
      <div className="flex flex-wrap gap-1.5">
        <Button size="sm" variant="outline" onClick={() => setGrants(true)}><Users />用户</Button>
        <Button size="sm" variant="outline" onClick={() => setShare(true)}><Share2 />分享</Button>
        <Button size="sm" variant="outline" onClick={() => setEdit(true)}><Pencil />编辑</Button>
        <Button size="sm" variant="ghost" onClick={() => setPreview(true)}><FileCode />查看生成的配置</Button>
        <Button size="sm" variant="ghost" onClick={() => setParams(p => { p.set("detail_tab", "probe"); return p; }, { replace: true })}><Activity />实测</Button>
        <Button size="sm" variant="destructive" disabled={isBusy()} busy={isBusy(item.id)} busyText="正在删除" onClick={() => void remove()}><Trash2 />删除</Button>
      </div>
      {tab === "probe" ? <ProfileProbe profile={item} close={() => setParams((p) => { p.set("detail_tab", "overview"); return p; }, { replace: true })} /> : (
        <div className="grid gap-3 sm:grid-cols-3">
          <Readout label="状态" value={item.enabled ? "启用" : "已停用"} />
          <Readout label="成员" value={item.all_sources ? "全部订阅源与手动节点" : members} />
          <Readout label="用户" value={item.user_ids?.length ?? null} />
          <Readout label="下载" value={item.downloads ?? null} />
          <Readout label="到期" value={item.expires_at ? date(item.expires_at) : null} />
          <Readout label="分享" value={shareState(item)} />
        </div>
      )}
      {edit && <ProfileForm value={item} close={() => setEdit(false)} saved={() => { setEdit(false); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {preview && <ProfilePreview profile={item} close={() => setPreview(false)} />}
      {share && <ShareDialog profile={item} close={() => setShare(false)} changed={() => void q.refetch()} />}
      {grants && <GrantDialog profile={item} close={() => setGrants(false)} saved={() => { setGrants(false); toast("用户分配已保存", { tone: "ok" }); void q.refetch(); }} />}
    </section>
  );
}

// 编辑订阅组：左侧依次是来源、挑选条件、模板，右侧实时显示按各源已有缓存组合出的节点；
// 处理链、正则筛选、命名与 DNS 等收进「高级」。
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
  const [showOutput, setShowOutput] = useState(false);
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
  const pick = settings.pick ?? {};
  const setPick = (patch: Partial<ProfilePick>) => {
    const next: ProfilePick = { ...pick, ...patch };
    if (!next.regions?.length) delete next.regions;
    if (!next.keywords?.length) delete next.keywords;
    if (next.max_multiplier == null) { delete next.max_multiplier; delete next.include_unlabeled_multiplier; }
    if (!next.include_unlabeled_multiplier) delete next.include_unlabeled_multiplier;
    if (!next.only_alive) delete next.only_alive;
    setS({ pick: Object.keys(next).length ? next : undefined });
  };
  const draft = { id: p?.id, ...(name.trim() ? { name: name.trim() } : {}), all_sources: all, members: all ? null : members, settings, operators: ops };
  const preview = useDraftPreview(draft);
  const guard = useFormGuard({ name, enabled, all, members, settings, ops, expiry }, close);
  const save = useFormAction(
    () => {
      const body = { name: name.trim(), enabled, all_sources: all, members: all ? null : members, settings, operators: ops, expires_at: fromLocalInput(expiry) };
      return p ? api("/admin/subscriptions/profiles/" + p.id, "PATCH", body) : api("/admin/subscriptions/profiles", "POST", body);
    },
    { fields: { invalid_template: "template", invalid_profile_members: "members", invalid_pick: "pick", ...renderingErrorFields }, onSuccess: () => { guard.commit(); saved(); } },
  );
  useConsoleContext({
    dialog: { title: p ? "编辑订阅组" : "新建订阅组" }, object: p ? { type: "profile", name: p.name, id: p.id } : undefined,
    form: [field("name", "名称", name), field("enabled", "启用", enabled ? "是" : "否"), field("all_sources", "全部订阅源", all ? "是" : "否"), field("members", "有序成员", members.map(memberName).join("、")),
      field("pick", "挑选条件", pickSummary(pick)), field("expires_at", "到期", expiry || "长期"), field("preview", "预览节点数", preview.data ? String(preview.data.nodes.length) : "未知")],
  });
  const advancedCount = ops.length + (settings.filter_include ? 1 : 0) + (settings.filter_exclude ? 1 : 0);
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && void guard.close()}>
      <DialogContent size="xl">
        <DialogTitle>{p ? "编辑订阅组" : "新建订阅组"}</DialogTitle>
        <DialogDescription>订阅组只保存来源和挑选条件，每次按最新节点重新挑选。右侧用各订阅源已有的缓存实时预览，不会触发拉取。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <div className="weir-profile-editor">
            <div className="stack min-w-0">
              <FormGrid>
                <Field label="名称" name="name" required hint="也是客户端里显示的配置名。"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
                <Field label="到期时间（可选）" hint="到期后拒绝下载并返回 403，请先续期。"><Input type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} /></Field>
              </FormGrid>
              <FormSection title="来源" description="勾选订阅源、手动节点或其他订阅组，拖动调整顺序；子组最多嵌套三层。">
                <SwitchField label="包含全部已启用的订阅源与手动节点" checked={all} onCheckedChange={setAll} />
                {!all && (loadingMembers || membersError) && <State loading={loadingMembers} error={membersError} onRetry={() => { void sources.refetch(); void nodes.refetch(); void profiles.refetch(); }} />}
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
                {!all && (profiles.data ?? []).some((item) => item.id !== p?.id) && <fieldset className="grid min-w-0 gap-2"><legend className="text-meta font-medium text-[var(--weir-ink-2)]">引用其他订阅组</legend>
                  {(profiles.data ?? []).filter((item) => item.id !== p?.id).map((item) => <CheckboxField key={item.id} label={item.name} disabled={!item.enabled && !chosen("profile", item.id)} checked={chosen("profile", item.id)} onCheckedChange={(v) => toggle("profile", item.id, v)} />)}
                </fieldset>}
                {!all && members.length > 1 && <Field label="输出顺序" name="members"><SortableList label="订阅组成员顺序" items={members} onReorder={setMembers} getId={(m) => `${m.kind}:${m.id}`} itemLabel={(m) => memberName(m)} renderItem={(m, { reorder }) => <div className="flex items-center justify-between gap-2"><span>{memberName(m)}</span>{reorder}</div>} /></Field>}
              </FormSection>
              <FormSection title="挑选条件" description="按节点名里的地区、倍率和关键词挑选；不设就是全部保留。">
                <PickFields pick={pick} change={setPick} available={preview.data?.available} />
              </FormSection>
              <FormSection title="模板">
                <FormGrid>
                  <Field label="规则模板" name="template"><TemplatePicker inherit value={settings.template} onChange={(v) => setS({ template: v })} /></Field>
                  <Field label="规则等级">
                    <Segmented aria-label="规则等级" value={settings.rule_level ?? "__inherit"} onValueChange={(v) => setS({ rule_level: v === "__inherit" ? undefined : v })}
                      options={[["__inherit", "默认"], ...ruleLevels] as const} />
                  </Field>
                </FormGrid>
              </FormSection>
              <Collapsible variant="section" title="高级" summary={advancedCount ? `${advancedCount} 项自定义` : "沿用默认"} defaultOpen={settings.engine === "external"}>
                <div className="stack">
                  <Field label="转换引擎" name="engine">{settings.engine === "external" && <Notice tone="warn">外部转换已移除；请明确选择内置转换或继承默认值后保存。</Notice>}<Segmented aria-label="转换引擎" value={settings.engine ?? "__inherit"} onValueChange={(v) => setS({ engine: v === "__inherit" ? undefined : v })} options={[["__inherit", "默认"], ["builtin", "内置"]] as const} /></Field>
                  <Field label="正则只保留（每行一条）" name="filter_include"><Textarea rows={2} value={settings.filter_include ?? ""} onChange={(e) => setS({ filter_include: e.target.value || undefined })} /></Field>
                  <Field label="正则排除（每行一条）" name="filter_exclude"><Textarea rows={2} value={settings.filter_exclude ?? ""} onChange={(e) => setS({ filter_exclude: e.target.value || undefined })} /></Field>
                  <FormGrid>
                    {([["flag_emoji", "地区旗帜"], ["prepend_manual", "手动节点加前缀"], ["prepend_sources", "订阅源节点加源名称"], ["traffic_nodes", "置顶流量 / 到期提示"]] as const).map(([k, label]) => (
                      <Field key={k} label={label}>
                        <Segmented aria-label={label} value={settings[k] === undefined ? "inherit" : settings[k] ? "on" : "off"} onValueChange={(v) => setS({ [k]: v === "inherit" ? undefined : v === "on" })}
                          options={[["inherit", "默认"], ["on", "开"], ["off", "关"]] as const} />
                      </Field>
                    ))}
                  </FormGrid>
                  <RenderingSettingsFields inherit value={settings} change={setS} />
                  <Collapsible title={`处理链（${ops.length} 步；为空时使用默认处理链）`}>
                    <OperatorEditor value={ops} change={setOps} />
                  </Collapsible>
                </div>
              </Collapsible>
              {p && <SwitchField label="启用" checked={enabled} onCheckedChange={setEnabled} />}
            </div>
            <DraftPreviewPanel state={preview} onShowOutput={p ? () => setShowOutput(true) : undefined} />
          </div>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={() => { if (!save.busy) void guard.close(); }}>取消</Button>
            <Button type="submit" disabled={save.busy || (!all && (loadingMembers || !!membersError))} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
        {showOutput && p && <ProfilePreview profile={p} close={() => setShowOutput(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function pickSummary(pick: ProfilePick) {
  const parts = [
    pick.regions?.length ? `地区 ${pick.regions.join("、")}` : "",
    pick.max_multiplier != null ? `倍率不超过 ${pick.max_multiplier}${pick.include_unlabeled_multiplier ? "（含未标注）" : ""}` : "",
    pick.keywords?.length ? `关键词 ${pick.keywords.join("、")}` : "",
    pick.only_alive ? "只要可用" : "",
  ].filter(Boolean);
  return parts.length ? parts.join("；") : "不限";
}

function PickFields({ pick, change, available }: { pick: ProfilePick; change: (patch: Partial<ProfilePick>) => void; available?: ProfileNodePreview["available"] }) {
  const [limit, setLimit] = useState(pick.max_multiplier == null ? "" : String(pick.max_multiplier));
  const [words, setWords] = useState((pick.keywords ?? []).join(" "));
  const selected = pick.regions ?? [];
  const regions = [...(available?.regions ?? [])];
  for (const r of selected) if (!regions.some((x) => x.region === r)) regions.push({ region: r, count: 0 });
  const limitValue = limit.trim() === "" ? null : Number(limit);
  const limitError = limitValue !== null && (!Number.isFinite(limitValue) || limitValue < 0.01 || limitValue > 100) ? "倍率上限须在 0.01–100 之间" : "";
  const toggleRegion = (region: string) => change({ regions: selected.includes(region) ? selected.filter((r) => r !== region) : [...selected, region] });
  return (
    <div className="stack">
      <Field label="地区" name="pick" hint={regions.length ? "不选就是全部地区；数字是当前缓存里的节点数。" : undefined}>
        {regions.length ? (
          <div className="flex flex-wrap gap-[var(--weir-gap-9)]" role="group" aria-label="地区">
            {regions.map((r) => <Chip key={r.region} selected={selected.includes(r.region)} aria-pressed={selected.includes(r.region)} meta={r.count || undefined} onClick={() => toggleRegion(r.region)}>{r.region}</Chip>)}
          </div>
        ) : <small className="text-meta text-[var(--weir-ink-3)]">{available ? "来源里还没有节点缓存，选好来源并刷新订阅源后这里列出地区。" : "正在汇总来源里的地区"}</small>}
      </Field>
      <FormGrid>
        <Field label="倍率上限" hint={available?.multipliers.length ? `当前有 ${available.multipliers.map((m) => m + "×").join("、")}；留空不限` : "留空不限"} customError={limitError}>
          <Input inputMode="decimal" placeholder="不限" value={limit} onChange={(e) => {
            const text = e.target.value.replace(/[^\d.]/g, "");
            setLimit(text);
            const v = text.trim() === "" ? null : Number(text);
            if (v === null || (Number.isFinite(v) && v >= 0.01 && v <= 100)) change({ max_multiplier: v });
          }} />
        </Field>
        <Field label="关键词" hint="名字包含任一关键词才保留，用空格或逗号分隔。">
          <Input placeholder="例如 IPLC 专线" value={words} onChange={(e) => { setWords(e.target.value); change({ keywords: e.target.value.split(/[\s,，]+/).filter(Boolean) }); }} />
        </Field>
      </FormGrid>
      <CheckboxField label="保留没标倍率的节点" description={available ? `当前有 ${available.unlabeled_multiplier} 个节点没标倍率` : undefined} disabled={pick.max_multiplier == null} checked={!!pick.include_unlabeled_multiplier} onCheckedChange={(v) => change({ include_unlabeled_multiplier: v })} />
      <SwitchField label="只要可用" description="去掉订阅源检测为不可用的节点；还没检测过的节点保留。" checked={!!pick.only_alive} onCheckedChange={(v) => change({ only_alive: v })} />
    </div>
  );
}

type DraftPreview = { data?: ProfileNodePreview; error?: unknown; loading: boolean };

// 编辑时的实时预览：停止输入 500ms 后用各源已有缓存组合一次；只保留最后一次请求的结果。
function useDraftPreview(draft: unknown): DraftPreview {
  const key = JSON.stringify(draft);
  const [state, setState] = useState<DraftPreview>({ loading: true });
  useEffect(() => {
    const controller = new AbortController();
    setState((s) => ({ ...s, loading: true }));
    const timer = setTimeout(() => {
      api("/admin/subscriptions/profiles/preview-nodes", "POST", JSON.parse(key), { signal: controller.signal })
        .then((data) => setState({ data: data as ProfileNodePreview, loading: false }))
        .catch((error) => { if (!controller.signal.aborted) setState((s) => ({ data: s.data, error, loading: false })); });
    }, 500);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [key]);
  return state;
}

const stepLabels: Record<string, string> = { dedupe: "去重", pick_region: "按地区", pick_multiplier: "按倍率", pick_keyword: "按关键词", pick_alive: "去掉不可用", operators: "处理链", filter: "正则筛选" };

const sourceProblem = (error: string) => error === "source_not_cached" ? "还没有缓存，刷新订阅源后才会出现" : error;

function DraftPreviewPanel({ state, onShowOutput }: { state: DraftPreview; onShowOutput?: () => void }) {
  const data = state.data;
  const steps = (data?.steps ?? []).filter((s) => s.removed !== 0 || s.step.startsWith("pick_"));
  return (
    <aside className="weir-profile-preview stack min-w-0" aria-label="节点预览" aria-busy={state.loading}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <strong className="text-body">{data ? `${data.nodes.length} 个节点` : "节点预览"}</strong>
        <small className="text-meta text-[var(--weir-ink-3)]">{state.loading ? "正在更新" : data ? `合并后 ${data.merged} 个` : ""}</small>
      </div>
      {state.error ? <Notice tone="bad">{state.error instanceof Error ? state.error.message : String(state.error)}</Notice> : null}
      {!data ? (!state.error && <SkeletonList rows={4} lines={1} className="stack" />) : (
        <>
          {steps.length > 0 && (
            <ul className="weir-profile-steps m-0 grid list-none gap-[var(--weir-gap-5)] p-0">
              {steps.map((s) => <li key={s.step} className="flex justify-between gap-2 text-meta"><span className="text-[var(--weir-ink-2)]">{stepLabels[s.step] ?? s.step}</span><span className="font-[family-name:var(--weir-mono)]">{s.removed > 0 ? `去掉 ${s.removed}` : s.removed < 0 ? `增加 ${-s.removed}` : "没有去掉"}</span></li>)}
            </ul>
          )}
          {data.sources.filter((s) => s.error).map((s) => <Notice key={s.source_id} tone="warn">{s.name || `订阅源 #${s.source_id}`}：{sourceProblem(s.error!)}</Notice>)}
          {data.nodes.length === 0 ? <PageState kind="empty" title="没有节点" detail="放宽挑选条件，或先选择来源。" /> : (
            <ScrollArea className="max-h-[55vh]" viewportClassName="weir-focus-gutter" contentClassName="grid content-start">
              {data.nodes.map((n, i) => (
                <div key={i} className="weir-set-row min-w-0">
                  <span className="flex min-w-0 items-center gap-2">
                    <StatusLamp size="sm" tone={n.status === "alive" ? "ok" : n.status === "failed" ? "bad" : "unknown"} label={n.status === "alive" ? "可用" : n.status === "failed" ? "不可用" : "未检测"} />
                    <span className="min-w-0">
                      <span className="block truncate">{n.name}</span>
                      <small className="block truncate text-[var(--weir-ink-3)]">{n.region} · {n.multiplier == null ? "倍率未标注" : `${n.multiplier}×`} · {n.kind === "source" ? (n.source_name || `订阅源 #${n.source_id}`) : n.kind === "profile" ? "子订阅组" : "手动节点"}</small>
                    </span>
                  </span>
                  <small className="shrink-0 font-[family-name:var(--weir-mono)] text-[var(--weir-ink-3)]">{n.latency_ms == null ? (n.status === "failed" ? "不可用" : "未检测") : `${n.latency_ms} ms`}</small>
                </div>
              ))}
            </ScrollArea>
          )}
        </>
      )}
      {onShowOutput && <Button type="button" size="sm" variant="ghost" onClick={onShowOutput}><FileCode />查看已保存版本生成的配置</Button>}
    </aside>
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
        <DialogTitle>生成的配置 · {profile.name}</DialogTitle>
        <DialogDescription>按已保存的设置实时拉取订阅源（不使用输出缓存），生成所选格式的完整配置。</DialogDescription>
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
  useConsoleContext({ object: { type: "profile", name: profile.name, id: profile.id }, dialog: { title: "分配用户" }, form: [field("user_ids", "勾选的用户", (users.data ?? []).filter((u) => picked.includes(u.id)).map((u) => u.name || u.email).join("、"))] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>分配用户 · {profile.name}</DialogTitle>
        <DialogDescription>每个用户在「我的订阅」里看到自己独立的地址；已分配用户的地址保持不变，取消勾选后该用户的地址立即失效。</DialogDescription>
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
