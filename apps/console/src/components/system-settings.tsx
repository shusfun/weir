// 系统设置卡片：安全策略、邮件服务、通知、WebDAV 备份、MiSub 备份导入导出、API 令牌。修改都需要重新输入密码。
import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, CardContent, CopyButton, Field, Input, Label, Notice, SectionHeader, Switch, Textarea, useToast } from "@migate/ui";
import { Download, Trash2 } from "lucide-react";
import { api } from "../api";
import { date } from "../lib/models";
import { State } from "./feedback";

// Reauth 是各卡片共用的「当前密码 + 二次验证码」输入。
function Reauth({ value, onChange }: { value: { password: string; code: string }; onChange: (v: { password: string; code: string }) => void }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label="当前密码" htmlFor={"re-pass-" + value.password.length}><Input type="password" autoComplete="current-password" aria-label="当前密码" required value={value.password} onChange={(e) => onChange({ ...value, password: e.target.value })} /></Field>
      <Field label="二次验证码（如已开启）"><Input inputMode="numeric" autoComplete="one-time-code" aria-label="二次验证码" value={value.code} onChange={(e) => onChange({ ...value, code: e.target.value })} /></Field>
    </div>
  );
}

function useSave() {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function run(fn: () => Promise<unknown>, ok: string, after?: () => void) {
    setBusy(true);
    setError("");
    try {
      await fn();
      toast(ok, { tone: "ok" });
      after?.();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return { busy, error, run };
}

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return <Card><CardContent className="stack"><SectionHeader title={title} description={description} />{children}</CardContent></Card>;
}

export function SecurityCard() {
  const q = useQuery({ queryKey: ["security-settings"], queryFn: () => api("/admin/settings/security") as Promise<Record<string, boolean>> });
  const [draft, setDraft] = useState<Record<string, boolean> | null>(null);
  const [re, setRe] = useState({ password: "", code: "" });
  const { busy, error, run } = useSave();
  const v = draft ?? q.data;
  const flags: [string, string][] = [["totp_enabled", "允许二次验证（TOTP）"], ["passkey_enabled", "允许通行密钥登录"], ["require_admin_mfa", "管理员必须开启二次验证"], ["registration_enabled", "允许自助注册（注册用户没有节点，需管理员分配）"], ["email_verification", "注册时验证邮箱（需要邮件服务）"]];
  return (
    <Section title="安全策略" description="登录与注册方式。">
      {q.isPending || q.error || !v ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(() => api("/admin/settings/security", "PUT", { ...v, ...re }), "已保存", () => { setDraft(null); setRe({ password: "", code: "" }); void q.refetch(); }); }}>
          <div className="grid gap-2 sm:grid-cols-2">
            {flags.map(([k, label]) => <Label key={k} className="actions"><Switch aria-label={label} checked={!!v[k]} onCheckedChange={(on) => setDraft({ ...v, [k]: on })} />{label}</Label>)}
          </div>
          <Reauth value={re} onChange={setRe} />
          {error && <Notice tone="bad">{error}</Notice>}
          <div className="actions"><Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button></div>
        </form>
      )}
    </Section>
  );
}

interface SMTPView { host: string; port: number; username: string; password_set: boolean; from: string; from_name: string; use_tls: boolean; configured: boolean }

export function SMTPCard() {
  const q = useQuery({ queryKey: ["smtp-settings"], queryFn: () => api("/admin/settings/smtp") as Promise<SMTPView> });
  if (q.isPending || q.error) return <Section title="邮件服务" description="用于找回密码与注册验证。"><State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /></Section>;
  return <SMTPForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />;
}

function SMTPForm({ value, saved }: { value: SMTPView; saved: () => void }) {
  const [f, setF] = useState({ host: value.host, port: String(value.port || 465), username: value.username, password: "", from: value.from, from_name: value.from_name, use_tls: value.use_tls || !value.host });
  const [re, setRe] = useState({ password: "", code: "" });
  const { busy, error, run } = useSave();
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  return (
    <Section title="邮件服务" description={value.configured ? "已配置，可发送找回密码与注册验证邮件。" : "未配置：登录页不提供找回密码。"}>
      <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(() => api("/admin/settings/smtp", "PUT", { ...f, port: Number(f.port) || 0, account_password: re.password, code: re.code }), "已保存", saved); }}>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="SMTP 服务器" htmlFor="smtp-host"><Input id="smtp-host" value={f.host} onChange={(e) => set({ host: e.target.value })} /></Field>
          <Field label="端口" htmlFor="smtp-port"><Input id="smtp-port" inputMode="numeric" value={f.port} onChange={(e) => set({ port: e.target.value.replace(/\D/g, "") })} /></Field>
          <Field label="用户名" htmlFor="smtp-user"><Input id="smtp-user" autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} /></Field>
          <Field label="密码" htmlFor="smtp-pass" hint={value.password_set ? "已保存；留空则沿用。" : undefined}><Input id="smtp-pass" type="password" autoComplete="new-password" value={f.password} onChange={(e) => set({ password: e.target.value })} /></Field>
          <Field label="发件地址" htmlFor="smtp-from"><Input id="smtp-from" type="email" value={f.from} onChange={(e) => set({ from: e.target.value })} /></Field>
          <Field label="发件人名称" htmlFor="smtp-name"><Input id="smtp-name" value={f.from_name} onChange={(e) => set({ from_name: e.target.value })} /></Field>
        </div>
        <Label className="actions"><Switch aria-label="使用 TLS" checked={f.use_tls} onCheckedChange={(use_tls) => set({ use_tls })} />使用 TLS（465 端口直接 TLS，其他端口 STARTTLS）</Label>
        <Reauth value={re} onChange={setRe} />
        {error && <Notice tone="bad">{error}</Notice>}
        <div className="actions">
          <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
          {value.configured && <Button type="button" variant="outline" disabled={busy} onClick={() => void run(() => api("/admin/settings/smtp/test", "POST", {}), "测试邮件已发出")}>发送测试邮件</Button>}
        </div>
      </form>
    </Section>
  );
}

interface NotifyView { enabled: boolean; token_set: boolean; chat_id: string; api_base: string; gate_offline: boolean; user_quota_percent: number; user_expiry_days: number; source_expiry_days: number; source_refresh_hours: number }

export function NotifyCard() {
  const q = useQuery({ queryKey: ["notify-settings"], queryFn: () => api("/admin/settings/notify") as Promise<NotifyView> });
  if (q.isPending || q.error) return <Section title="通知与定时任务" description="Telegram 提醒与订阅源定时刷新。"><State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /></Section>;
  return <NotifyForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />;
}

function NotifyForm({ value, saved }: { value: NotifyView; saved: () => void }) {
  const [f, setF] = useState({ ...value, bot_token: "" });
  const [re, setRe] = useState({ password: "", code: "" });
  const { busy, error, run } = useSave();
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const num = (k: "user_quota_percent" | "user_expiry_days" | "source_expiry_days" | "source_refresh_hours", label: string, hint: string) => (
    <Field label={label} hint={hint}><Input aria-label={label} inputMode="numeric" value={String(f[k])} onChange={(e) => set({ [k]: Number(e.target.value.replace(/\D/g, "")) || 0 })} /></Field>
  );
  return (
    <Section title="通知与定时任务" description="提醒只包含名称与数值，不含订阅地址或凭据；同一事件 24 小时内只提醒一次。">
      <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(() => api("/admin/settings/notify", "PUT", { ...f, ...re }), "已保存", saved); }}>
        <Label className="actions"><Switch aria-label="开启 Telegram 通知" checked={f.enabled} onCheckedChange={(enabled) => set({ enabled })} />开启 Telegram 通知</Label>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Bot Token" hint={value.token_set ? "已保存；留空则沿用。" : undefined}><Input aria-label="Bot Token" type="password" autoComplete="off" value={f.bot_token} onChange={(e) => set({ bot_token: e.target.value })} /></Field>
          <Field label="Chat ID"><Input aria-label="Chat ID" value={f.chat_id} onChange={(e) => set({ chat_id: e.target.value })} /></Field>
          <Field label="接口地址" hint="默认 https://api.telegram.org，可填自建反代。"><Input aria-label="接口地址" value={f.api_base} onChange={(e) => set({ api_base: e.target.value })} /></Field>
          {num("user_quota_percent", "用户用量提醒（%）", "0 表示不提醒。")}
          {num("user_expiry_days", "用户到期前提醒（天）", "0 表示不提醒。")}
          {num("source_expiry_days", "订阅源到期前提醒（天）", "按机场报告的到期时间。")}
          {num("source_refresh_hours", "订阅源定时刷新（小时）", "0 表示只在访问订阅组时按缓存刷新。")}
        </div>
        <Label className="actions"><Switch aria-label="VPS 离线提醒" checked={f.gate_offline} onCheckedChange={(gate_offline) => set({ gate_offline })} />VPS 离线超过 5 分钟时提醒</Label>
        <Reauth value={re} onChange={setRe} />
        {error && <Notice tone="bad">{error}</Notice>}
        <div className="actions">
          <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
          {value.enabled && value.token_set && <Button type="button" variant="outline" disabled={busy} onClick={() => void run(() => api("/admin/settings/notify/test", "POST", {}), "测试通知已发出")}>发送测试通知</Button>}
        </div>
      </form>
    </Section>
  );
}

interface WebdavView { url: string; username: string; password_set: boolean; keep: number; auto: boolean; hours: number; last_run_at?: string | null; last_error?: string }
interface ImportStats { sources: number; nodes: number; profiles: number; templates: number; skipped: string[] | null }

export function BackupCard() {
  const q = useQuery({ queryKey: ["webdav"], queryFn: () => api("/admin/backup/webdav") as Promise<WebdavView> });
  return (
    <Section title="备份与迁移" description="格式与 MiSub 备份一致：订阅源、手动节点、订阅组与规则模板。导入只新增，不删除现有数据；用户与 VPS 不在备份里。">
      <ImportExport />
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <WebdavForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />}
    </Section>
  );
}

function StatsNotice({ s }: { s: ImportStats }) {
  return (
    <Notice tone="ok">
      已导入：{s.sources} 个订阅源、{s.nodes} 个手动节点、{s.profiles} 个订阅组、{s.templates} 个模板。
      {(s.skipped ?? []).length > 0 && <><br />跳过 {s.skipped!.length} 项：{s.skipped!.slice(0, 5).join("；")}{s.skipped!.length > 5 ? "…" : ""}</>}
    </Notice>
  );
}

function ImportExport() {
  const [content, setContent] = useState("");
  const [re, setRe] = useState({ password: "", code: "" });
  const [stats, setStats] = useState<ImportStats | null>(null);
  const { busy, error, run } = useSave();
  async function exportFile() {
    const res = await fetch("/api/v1/admin/backup/export", { credentials: "same-origin" });
    if (!res.ok) throw new Error("导出失败");
    const blob = await res.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") ?? "")?.[1] ?? "misub-backup.json";
    a.click();
    URL.revokeObjectURL(a.href);
  }
  return (
    <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(async () => setStats(await api("/admin/backup/import", "POST", { content, ...re })), "导入完成"); }}>
      <div className="actions"><Button type="button" variant="outline" disabled={busy} onClick={() => void run(exportFile, "已导出")}><Download />导出备份</Button></div>
      <Field label="导入 MiSub 备份" htmlFor="import-backup" hint="粘贴备份文件内容，或选择文件。">
        <Textarea id="import-backup" rows={4} className="font-mono text-xs" value={content} onChange={(e) => setContent(e.target.value)} />
      </Field>
      <Input type="file" accept="application/json,.json" aria-label="选择备份文件" onChange={async (e) => { const f = e.target.files?.[0]; if (f) setContent(await f.text()); }} />
      <Reauth value={re} onChange={setRe} />
      {error && <Notice tone="bad">{error}</Notice>}
      {stats && <StatsNotice s={stats} />}
      <div className="actions"><Button type="submit" disabled={busy || !content.trim()}>{busy ? "正在导入…" : "导入"}</Button></div>
    </form>
  );
}

function WebdavForm({ value, saved }: { value: WebdavView; saved: () => void }) {
  const [f, setF] = useState({ url: value.url, username: value.username, password: "", keep: String(value.keep), auto: value.auto, hours: String(value.hours) });
  const [re, setRe] = useState({ password: "", code: "" });
  const [files, setFiles] = useState<string[] | null>(null);
  const [stats, setStats] = useState<ImportStats | null>(null);
  const { busy, error, run } = useSave();
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  return (
    <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(() => api("/admin/backup/webdav", "PUT", { ...f, keep: Number(f.keep) || 10, hours: Number(f.hours) || 24, account_password: re.password, code: re.code }), "已保存", saved); }}>
      <h3 className="text-sm font-semibold">WebDAV 备份</h3>
      <small className="text-[var(--mg-ink-3)]">最近一次：{date(value.last_run_at ?? undefined)}{value.last_error ? ` · 失败：${value.last_error}` : ""}</small>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="目录地址" hint="例如 https://dav.example.com/backup"><Input aria-label="WebDAV 目录地址" value={f.url} onChange={(e) => set({ url: e.target.value })} /></Field>
        <Field label="用户名"><Input aria-label="WebDAV 用户名" autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} /></Field>
        <Field label="密码" hint={value.password_set ? "已保存；留空则沿用。" : undefined}><Input aria-label="WebDAV 密码" type="password" autoComplete="new-password" value={f.password} onChange={(e) => set({ password: e.target.value })} /></Field>
        <Field label="保留份数"><Input aria-label="保留份数" inputMode="numeric" value={f.keep} onChange={(e) => set({ keep: e.target.value.replace(/\D/g, "") })} /></Field>
        <Field label="自动备份间隔（小时）"><Input aria-label="自动备份间隔" inputMode="numeric" value={f.hours} onChange={(e) => set({ hours: e.target.value.replace(/\D/g, "") })} /></Field>
      </div>
      <Label className="actions"><Switch aria-label="自动备份" checked={f.auto} onCheckedChange={(auto) => set({ auto })} />自动备份</Label>
      <Reauth value={re} onChange={setRe} />
      {error && <Notice tone="bad">{error}</Notice>}
      {stats && <StatsNotice s={stats} />}
      <div className="actions">
        <Button type="submit" disabled={busy}>{busy ? "正在处理…" : "保存"}</Button>
        {value.url && <Button type="button" variant="outline" disabled={busy} onClick={() => void run(() => api("/admin/backup/webdav/run", "POST", {}), "备份已上传", saved)}>立即备份</Button>}
        {value.url && <Button type="button" variant="outline" disabled={busy} onClick={() => void run(async () => setFiles((await api("/admin/backup/webdav/files")).files), "已读取备份列表")}>查看备份</Button>}
      </div>
      {files && (files.length === 0 ? <State empty="目录里还没有备份" /> : (
        <ul className="stack">
          {files.map((name) => (
            <li key={name} className="flex items-center justify-between gap-2">
              <code className="text-xs">{name}</code>
              <Button type="button" size="sm" variant="outline" disabled={busy || !re.password} onClick={() => window.confirm(`从 ${name} 导入？只新增，不删除现有数据。`) && run(async () => setStats(await api("/admin/backup/webdav/restore", "POST", { file: name, ...re })), "导入完成")}>导入这份</Button>
            </li>
          ))}
        </ul>
      ))}
    </form>
  );
}

interface APIToken { id: number; name: string; created_at: string; last_used_at?: string }

export function APITokensCard() {
  const q = useQuery({ queryKey: ["api-tokens"], queryFn: async () => (await api("/admin/api-tokens")).items as APIToken[] });
  const [name, setName] = useState("");
  const [re, setRe] = useState({ password: "", code: "" });
  const [created, setCreated] = useState("");
  const { busy, error, run } = useSave();
  return (
    <Section title="外部接口" description="脚本可用 Authorization: Bearer <令牌> 调用 /api/ext/v1 管理订阅源、手动节点与订阅组；不能访问用户、VPS 与系统设置。">
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : q.data.length === 0 ? <State empty="还没有 API 令牌" /> : (
        <ul className="stack">
          {q.data.map((t) => (
            <li key={t.id} className="flex items-center justify-between gap-2">
              <span>{t.name}<small className="block text-[var(--mg-ink-3)]">创建 {date(t.created_at)} · 最近使用 {date(t.last_used_at)}</small></span>
              <Button size="icon-sm" variant="destructive" aria-label={"撤销 " + t.name} disabled={busy} onClick={() => window.confirm(`撤销令牌「${t.name}」？使用它的脚本会立即失效。`) && run(() => api("/admin/api-tokens/" + t.id, "DELETE"), "已撤销", () => void q.refetch())}><Trash2 /></Button>
            </li>
          ))}
        </ul>
      )}
      {created && <div className="subscription-url"><Input readOnly className="font-mono text-xs" aria-label="新令牌" value={created} /><CopyButton value={created}>复制</CopyButton></div>}
      {created && <Notice role="status">令牌只显示这一次，请现在复制保存。</Notice>}
      <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(async () => { setCreated((await api("/admin/api-tokens", "POST", { name: name.trim(), ...re })).token); setName(""); }, "已创建", () => void q.refetch()); }}>
        <Field label="新令牌名称"><Input aria-label="新令牌名称" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
        <Reauth value={re} onChange={setRe} />
        {error && <Notice tone="bad">{error}</Notice>}
        <div className="actions"><Button type="submit" disabled={busy || !name.trim()}>{busy ? "正在创建…" : "创建令牌"}</Button></div>
      </form>
    </Section>
  );
}
