// 订阅组：选订阅源与手动节点（或全部），配处理链与规则模板；预览、分配给用户、匿名分享。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Checkbox, CodeBlock, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Label, Notice, SectionHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch, Textarea, useToast } from "@weir/ui";
import { Eye, Plus, Share2, Trash2, Users } from "lucide-react";
import { api } from "../api";
import { date } from "../lib/models";
import { fromLocalInput, localInput, type UserItem } from "../lib/core";
import { ruleLevels, type BuiltinTemplate, type CustomTemplate, type Profile, type ProfileSettings } from "../lib/misub";
import { OperatorEditor, type Operator } from "./operator-editor";
import { useSources } from "./misub-sources";
import { useManualNodes } from "./misub-nodes";
import { State } from "./feedback";

export function useTemplates() {
  return useQuery({ queryKey: ["misub-templates"], queryFn: () => api("/admin/misub/templates") as Promise<{ builtin: BuiltinTemplate[]; custom: CustomTemplate[] }> });
}

// TemplatePicker 选择规则模板：跟随默认 / 不使用 / 内置 / 自定义 / 远程地址。
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
        <SelectTrigger aria-label="规则模板"><SelectValue>{options.find(([k]) => k === current)?.[1] ?? "请选择"}</SelectValue></SelectTrigger>
        <SelectContent>{options.map(([k, l]) => <SelectItem key={k || "_none"} value={k}>{l}</SelectItem>)}</SelectContent>
      </Select>
      {remote && <Input aria-label="远程模板地址" placeholder="https://example.com/template.ini" value={value!.slice(7)} onChange={(e) => onChange("remote:" + e.target.value.trim())} />}
    </div>
  );
}

export function ProfilesPanel() {
  const toast = useToast();
  const q = useQuery({ queryKey: ["misub-profiles"], queryFn: async () => (await api("/admin/misub/profiles")).items as Profile[] });
  const [edit, setEdit] = useState<Profile | "new" | null>(null);
  const [preview, setPreview] = useState<Profile | null>(null);
  const [share, setShare] = useState<Profile | null>(null);
  const [grants, setGrants] = useState<Profile | null>(null);
  const [busy, setBusy] = useState(0);
  async function remove(p: Profile) {
    if (!window.confirm(`删除订阅组「${p.name}」？分配给用户的地址与分享地址都会失效。`)) return;
    setBusy(p.id);
    try {
      await api("/admin/misub/profiles/" + p.id, "DELETE");
      await q.refetch();
    } catch (e) {
      toast("删除失败", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
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
                <Button size="icon-sm" variant="destructive" aria-label={"删除 " + p.name} disabled={busy === p.id} onClick={() => void remove(p)}><Trash2 /></Button>
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
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const toggle = (list: number[], id: number, on: boolean) => (on ? [...list, id] : list.filter((x) => x !== id));
  const setS = (patch: Partial<ProfileSettings>) => {
    const next = { ...settings, ...patch };
    for (const k of Object.keys(patch) as (keyof ProfileSettings)[]) if (patch[k] === undefined) delete next[k];
    setSettings(next);
  };
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const body = { name: name.trim(), enabled, all_sources: all, source_ids: all ? [] : sourceIds, node_ids: all ? [] : nodeIds, settings, operators: ops, expires_at: fromLocalInput(expiry) };
      await (p ? api("/admin/misub/profiles/" + p.id, "PATCH", body) : api("/admin/misub/profiles", "POST", body));
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
        <DialogTitle>{p ? "编辑订阅组" : "新建订阅组"}</DialogTitle>
        <DialogDescription>节点按这里勾选的顺序输出：先手动节点，再各订阅源。未单独设置的项沿用默认设置。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="名称" htmlFor="pf-name" hint="也是客户端里显示的配置名。"><Input id="pf-name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="到期时间（可选）" htmlFor="pf-exp" hint="到期后只输出一个「订阅已到期」提示节点。"><Input id="pf-exp" type="datetime-local" value={expiry} onChange={(e) => setExpiry(e.target.value)} /></Field>
          </div>
          <Label className="actions"><Switch aria-label="包含全部" checked={all} onCheckedChange={setAll} />包含全部已启用的订阅源与手动节点</Label>
          {!all && (
            <div className="grid gap-3 sm:grid-cols-2">
              <fieldset className="stack mg-focus-gutter max-h-56 overflow-y-auto">
                <legend className="text-sm">订阅源</legend>
                {(sources.data?.items ?? []).map((s) => (
                  <Label key={s.id} className="actions"><Checkbox checked={sourceIds.includes(s.id)} onCheckedChange={(v) => setSourceIds(toggle(sourceIds, s.id, !!v))} />{s.name || s.url}</Label>
                ))}
                {sources.data?.items.length === 0 && <small>还没有订阅源。</small>}
              </fieldset>
              <fieldset className="stack mg-focus-gutter max-h-56 overflow-y-auto">
                <legend className="text-sm">手动节点</legend>
                {(nodes.data?.items ?? []).map((n) => (
                  <Label key={n.id} className="actions"><Checkbox checked={nodeIds.includes(n.id)} onCheckedChange={(v) => setNodeIds(toggle(nodeIds, n.id, !!v))} />{n.name || decodeURIComponent(n.url.split("#")[1] ?? "") || n.id}{n.group && <small className="text-[var(--mg-ink-3)]">（{n.group}）</small>}</Label>
                ))}
                {nodes.data?.items.length === 0 && <small>还没有手动节点。</small>}
              </fieldset>
            </div>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="规则模板"><TemplatePicker inherit value={settings.template} onChange={(v) => setS({ template: v })} /></Field>
            <Field label="规则等级">
              <Segmented aria-label="规则等级" value={settings.rule_level ?? "__inherit"} onValueChange={(v) => setS({ rule_level: v === "__inherit" ? undefined : v })}
                options={[["__inherit", "默认"], ...ruleLevels] as const} />
            </Field>
          </div>
          <Field label="只保留（每行一条）" htmlFor="pf-inc"><Textarea id="pf-inc" rows={2} value={settings.filter_include ?? ""} onChange={(e) => setS({ filter_include: e.target.value || undefined })} /></Field>
          <Field label="排除（每行一条）" htmlFor="pf-exc"><Textarea id="pf-exc" rows={2} value={settings.filter_exclude ?? ""} onChange={(e) => setS({ filter_exclude: e.target.value || undefined })} /></Field>
          <div className="grid gap-2 sm:grid-cols-2">
            {([["flag_emoji", "地区旗帜"], ["prepend_manual", "手动节点加前缀"], ["prepend_sources", "订阅源节点加源名称"], ["traffic_nodes", "置顶流量 / 到期提示"]] as const).map(([k, label]) => (
              <Field key={k} label={label}>
                <Segmented aria-label={label} value={settings[k] === undefined ? "inherit" : settings[k] ? "on" : "off"} onValueChange={(v) => setS({ [k]: v === "inherit" ? undefined : v === "on" })}
                  options={[["inherit", "默认"], ["on", "开"], ["off", "关"]] as const} />
              </Field>
            ))}
          </div>
          <details>
            <summary className="cursor-pointer text-sm">订阅组处理链（{ops.length} 步；为空时使用默认处理链）</summary>
            <div className="mt-2"><OperatorEditor value={ops} change={setOps} /></div>
          </details>
          {p && <Label className="actions"><Switch aria-label="启用" checked={enabled} onCheckedChange={setEnabled} />启用</Label>}
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

const targets = [["clash", "Clash"], ["singbox", "sing-box"], ["surge", "Surge"], ["loon", "Loon"], ["quanx", "QuanX"], ["egern", "Egern"], ["base64", "Base64"]] as const;

function ProfilePreview({ profile, close }: { profile: Profile; close: () => void }) {
  const [target, setTarget] = useState<(typeof targets)[number][0]>("clash");
  const q = useQuery({
    queryKey: ["misub-preview", profile.id, target],
    queryFn: () => api(`/admin/misub/profiles/${profile.id}/preview`, "POST", { target }) as Promise<{ nodes: string[]; sources: { SourceID: number; Nodes: number; FromCache: boolean; Error: string }[] | null; output?: string; output_error?: string }>,
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
            {q.data.output_error ? <Notice tone="bad">{q.data.output_error}</Notice> : <CodeBlock className="block max-h-[55vh] overflow-auto whitespace-pre text-xs">{q.data.output}</CodeBlock>}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ShareDialog({ profile, close, changed }: { profile: Profile; close: () => void; changed: () => void }) {
  const [days, setDays] = useState("7");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [url, setUrl] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      changed();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
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
          <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(async () => setUrl((await api(`/admin/misub/profiles/${profile.id}/share`, "POST", { days: Number(days), password, code })).url)); }}>
            {profile.shared && <Notice tone="warn">当前已分享，{profile.share_expires_at ? `到期 ${date(profile.share_expires_at)}` : ""}。重新生成会替换旧地址。</Notice>}
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="有效天数" htmlFor="sh-days"><Input id="sh-days" inputMode="numeric" required value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))} /></Field>
              <Field label="当前密码" htmlFor="sh-pass"><Input id="sh-pass" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
              <Field label="二次验证码" htmlFor="sh-code"><Input id="sh-code" inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} /></Field>
            </div>
            {error && <Notice tone="bad">{error}</Notice>}
            <DialogFooter>
              {profile.shared && <Button type="button" variant="destructive" disabled={busy} onClick={() => void run(async () => { await api(`/admin/misub/profiles/${profile.id}/share`, "DELETE"); close(); })}>撤下分享</Button>}
              <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
              <Button type="submit" disabled={busy}>{busy ? "正在生成…" : profile.shared ? "重新生成地址" : "生成分享地址"}</Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function GrantDialog({ profile, close, saved }: { profile: Profile; close: () => void; saved: () => void }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const [picked, setPicked] = useState<number[]>(profile.user_ids);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    setError("");
    try {
      await api(`/admin/misub/profiles/${profile.id}/grants`, "PUT", { user_ids: picked });
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>分配用户 · {profile.name}</DialogTitle>
        <DialogDescription>每个用户在「我的订阅」里看到自己独立的地址；取消勾选后该用户的地址立即失效。</DialogDescription>
        {users.isPending || users.error ? (
          <State loading={users.isPending} error={users.error} onRetry={() => users.refetch()} />
        ) : (
          <fieldset className="stack mg-focus-gutter max-h-80 overflow-y-auto">
            <legend className="sr-only">用户</legend>
            {users.data.map((u) => (
              <Label key={u.id} className="actions"><Checkbox checked={picked.includes(u.id)} onCheckedChange={(v) => setPicked(v ? [...picked, u.id] : picked.filter((x) => x !== u.id))} />{u.name || u.email}<small className="text-[var(--mg-ink-3)]">{u.email}</small></Label>
            ))}
          </fieldset>
        )}
        {error && <Notice tone="bad">{error}</Notice>}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
          <Button disabled={busy || users.isPending} onClick={() => void submit()}>{busy ? "正在保存…" : "保存"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
