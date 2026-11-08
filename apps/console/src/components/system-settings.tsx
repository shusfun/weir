// 系统设置卡片：安全策略、邮件服务、通知、WebDAV 备份、订阅备份导入导出、API 令牌。
// 设置保存直接生效；导入或恢复备份、创建 API 令牌要重新输入密码。
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useQuery } from "../lib/query";
import { Button, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, FilePicker, Form, FormGrid, Input, Notice, PasswordInput, SwitchField, Textarea, useConfirm, InsetGroup } from "@weir/ui";
import { Download, Trash2 } from "lucide-react";
import { api, reauthFields } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { ActionsWait } from "./wait";
import { saveBlob } from "../lib/download";
import { date } from "../lib/models";
import { State } from "./feedback";
import { useUnsavedChanges } from "../lib/dirty";

export type ReauthValue = { password: string; code: string };
export const emptyReauth: ReauthValue = { password: "", code: "" };

// Reauth 是各卡片共用的「当前密码 + 二次验证码」输入；放在 <Form> 里，服务端的验证错误标在对应字段下方。
// 只用于导入或恢复备份、创建 API 令牌；reason：为什么要验证（显示在字段下方）。
export function Reauth({ value, onChange, reason }: { value: ReauthValue; onChange: (v: ReauthValue) => void; reason?: string }) {
  return (
    <FormGrid>
      <Field label="当前密码" name="password" hint={reason}>
        <PasswordInput autoComplete="current-password" required value={value.password} onChange={(e) => onChange({ ...value, password: e.target.value })} />
      </Field>
      <Field label="二次验证码（如已开启）" name="code">
        <Input inputMode="numeric" autoComplete="one-time-code" value={value.code} onChange={(e) => onChange({ ...value, code: e.target.value })} />
      </Field>
    </FormGrid>
  );
}

// 需要重新验证身份的保存：在 useFormAction 的基础上把密码 / 验证码错误对应到 Reauth 的字段
export function useReauthAction<T>(fn: Parameters<typeof useFormAction<T>>[0], opts: Parameters<typeof useFormAction<T>>[1] = {}) {
  return useFormAction<T>(fn, { ...opts, fields: { ...reauthFields, ...opts.fields } });
}

export function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <InsetGroup title={title} description={description}>
      {children}
    </InsetGroup>
  );
}

const portError = (v: string) => (v && (Number(v) < 1 || Number(v) > 65535) ? "端口须在 1–65535 之间" : "");
const rangeError = (v: string, min: number, max: number, unit: string) => (v && (Number(v) < min || Number(v) > max) ? `须在 ${min}–${max} ${unit}之间` : "");

const securityFlags = [
  ["totp_enabled", "允许二次验证（TOTP）", ""],
  ["passkey_enabled", "允许通行密钥登录", ""],
  ["require_admin_mfa", "管理员必须开启二次验证", ""],
  ["registration_enabled", "允许自助注册", "注册用户没有节点，需管理员分配。"],
  ["email_verification", "注册时验证邮箱", "需要邮件服务。"],
] as const;

export function SecurityCard() {
  const q = useQuery({ queryKey: ["security-settings"], queryFn: () => api("/admin/settings/security") as Promise<Record<string, boolean>> });
  const smtp = useQuery({ queryKey: ["smtp-settings"], queryFn: () => api("/admin/settings/smtp") as Promise<{ configured: boolean }> });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api("/me") as Promise<{ totp_enrolled?: boolean }> });
  const confirm = useConfirm();
  const [draft, setDraft] = useState<Record<string, boolean> | null>(null);
  const v = draft ?? q.data;
  const dirty = !!q.data && !!draft && JSON.stringify(draft) !== JSON.stringify(q.data);
  useUnsavedChanges(dirty);
  const save = useFormAction(() => api("/admin/settings/security", "PUT", v), {
    ok: "已保存",
    onSuccess: () => {
      setDraft(null);
      void q.refetch();
    },
  });
  return (
    <Section title="安全策略" description="登录与注册方式。">
      {q.isPending || q.error || !v ? (
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      ) : (
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <div className="settings-group">
            {securityFlags.map(([k, label, description]) => {
              const disabled = (k === "email_verification" && (!smtp.data?.configured || !v.registration_enabled)) || (k === "require_admin_mfa" && !v.totp_enabled);
              const detail = k === "email_verification" && disabled ? (!smtp.data?.configured ? "需要先配置邮件服务。" : "需要先允许自助注册。") : k === "totp_enabled" ? "关闭后，已绑定用户仍可使用现有 TOTP，不能再绑定新设备。" : k === "passkey_enabled" ? "关闭后，已绑定的通行密钥仍可使用，但不能再新增。" : description;
              return <SwitchField placement="end" key={k} label={label} description={detail || undefined} disabled={disabled} checked={!!v[k]} onCheckedChange={async (on) => {
                if (k === "require_admin_mfa" && on && !me.data?.totp_enrolled && !(await confirm({ title: "当前管理员还没有绑定 TOTP", description: "保存后管理员需要先完成二次验证设置，未完成前不能继续管理后台。", confirmText: "继续" }))) return;
                setDraft({ ...v, [k]: on });
              }} />;
            })}
          </div>
          <div className="actions">
            <Button type="submit" disabled={save.busy || !dirty} busy={save.busy} busyText="正在保存…">保存</Button>
            {dirty && <Button type="button" variant="ghost" disabled={save.busy} onClick={() => setDraft(null)}>放弃修改</Button>}
          </div>
          {save.error && <Notice tone="bad">{save.error}</Notice>}
        </Form>
      )}
    </Section>
  );
}

interface SMTPView { host: string; port: number; username: string; password_set: boolean; from: string; from_name: string; use_tls: boolean; configured: boolean }

export function SMTPCard() {
  const q = useQuery({ queryKey: ["smtp-settings"], queryFn: () => api("/admin/settings/smtp") as Promise<SMTPView> });
  if (q.isPending || q.error)
    return (
      <Section title="邮件服务" description="用于找回密码与注册验证。">
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      </Section>
    );
  return <SMTPForm value={q.data} saved={() => q.refetch()} />;
}

function SMTPForm({ value, saved }: { value: SMTPView; saved: () => void }) {
  const [f, setF] = useState({ host: value.host, port: String(value.port || 465), username: value.username, password: "", from: value.from, from_name: value.from_name, use_tls: value.use_tls || !value.host });
  useEffect(() => setF({ host: value.host, port: String(value.port || 465), username: value.username, password: "", from: value.from, from_name: value.from_name, use_tls: value.use_tls || !value.host }), [value]);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const dirty = f.host !== value.host || f.port !== String(value.port || 465) || f.username !== value.username || f.password !== "" || f.from !== value.from || f.from_name !== value.from_name || f.use_tls !== value.use_tls;
  useUnsavedChanges(dirty);
  const save = useFormAction(() => api("/admin/settings/smtp", "PUT", { ...f, port: Number(f.port) || 0 }), { ok: "已保存", onSuccess: saved });
  const { run, isBusy, waitFor } = useRowAction();
  return (
    <Section title="邮件服务" description={value.configured ? "已配置，可发送找回密码与注册验证邮件。" : "未配置：登录页不提供找回密码。"}>
      <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
        <FormGrid>
          <Field label="SMTP 服务器"><Input value={f.host} onChange={(e) => set({ host: e.target.value })} /></Field>
          <Field label="端口" customError={portError(f.port)}><Input inputMode="numeric" value={f.port} onChange={(e) => set({ port: e.target.value.replace(/\D/g, "") })} /></Field>
          <Field label="用户名"><Input autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} /></Field>
          <Field label="密码" hint={value.password_set ? "已保存；留空则沿用。" : undefined}><PasswordInput autoComplete="new-password" value={f.password} onChange={(e) => set({ password: e.target.value })} /></Field>
          <Field label="发件地址"><Input type="email" value={f.from} onChange={(e) => set({ from: e.target.value })} /></Field>
          <Field label="发件人名称"><Input value={f.from_name} onChange={(e) => set({ from_name: e.target.value })} /></Field>
        </FormGrid>
        <div className="settings-group">
          <SwitchField placement="end" label="使用 TLS" description="465 端口直接 TLS，其他端口 STARTTLS。" checked={f.use_tls} onCheckedChange={(use_tls) => set({ use_tls })} />
        </div>
        <div className="actions">
            <Button type="submit" disabled={save.busy || !dirty} busy={save.busy} busyText="正在保存…">保存</Button>
            {dirty && <Button type="button" variant="ghost" disabled={save.busy} onClick={() => setF({ host: value.host, port: String(value.port || 465), username: value.username, password: "", from: value.from, from_name: value.from_name, use_tls: value.use_tls || !value.host })}>放弃修改</Button>}
          {value.configured && (
            <Button type="button" variant="outline" disabled={isBusy() || dirty} busy={isBusy("test")} busyText="正在发送…" onClick={() => void run("test", () => api("/admin/settings/smtp/test", "POST", {}), "测试邮件已发出", { wait: { label: `正在经 ${value.host || "SMTP 服务器"} 发送测试邮件`, max: 25 } })}>
              发送测试邮件
            </Button>
          )}
          <ActionsWait wait={waitFor("test")} />
        </div>
        {dirty && <small className="text-[var(--weir-ink-3)]">未保存；先保存，再测试。</small>}
        {save.error && <Notice tone="bad">{save.error}</Notice>}
      </Form>
    </Section>
  );
}

interface NotifyView { enabled: boolean; token_set: boolean; chat_id: string; api_base: string; gate_offline: boolean; user_quota_percent: number; user_expiry_days: number; source_expiry_days: number }

export function NotifyCard() {
  const q = useQuery({ queryKey: ["notify-settings"], queryFn: () => api("/admin/settings/notify") as Promise<NotifyView> });
  if (q.isPending || q.error)
    return (
        <Section title="Telegram 通知" description="Telegram 提醒只包含名称与数值，不含订阅地址或凭据。">
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      </Section>
    );
  return <NotifyForm value={q.data} saved={() => q.refetch()} />;
}

function NotifyForm({ value, saved }: { value: NotifyView; saved: () => void }) {
  const [f, setF] = useState({ ...value, bot_token: "" });
  useEffect(() => setF({ ...value, bot_token: "" }), [value]);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const dirty = f.enabled !== value.enabled || f.chat_id !== value.chat_id || f.api_base !== value.api_base || f.gate_offline !== value.gate_offline || f.user_quota_percent !== value.user_quota_percent || f.user_expiry_days !== value.user_expiry_days || f.source_expiry_days !== value.source_expiry_days || f.bot_token !== "";
  useUnsavedChanges(dirty);
  const save = useFormAction(() => api("/admin/settings/notify", "PUT", f), { ok: "已保存", onSuccess: saved });
  const { run, isBusy, waitFor } = useRowAction();
  const num = (k: "user_quota_percent" | "user_expiry_days" | "source_expiry_days", label: string, hint: string, max?: number) => (
    <Field label={label} hint={hint} customError={String(f[k]) === "" ? "请输入数值" : max !== undefined && Number(f[k]) > max ? `不能大于 ${max}` : ""}>
      <Input inputMode="numeric" value={String(f[k])} disabled={!f.enabled} onChange={(e) => { const raw = e.target.value.replace(/\D/g, ""); set({ [k]: (raw === "" ? "" : Number(raw)) } as Partial<typeof f>); }} />
    </Field>
  );
  return (
    <Section title="Telegram 通知" description="提醒只包含名称与数值，不含订阅地址或凭据；同一事件 24 小时内只提醒一次。">
      <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
        <div className="settings-group">
          <SwitchField placement="end" label="开启 Telegram 通知" checked={f.enabled} onCheckedChange={(enabled) => set({ enabled })} />
        </div>
        <FormGrid>
          <Field label="Bot Token" hint={value.token_set ? "已保存；留空则沿用。" : undefined}><PasswordInput autoComplete="off" disabled={!f.enabled} value={f.bot_token} onChange={(e) => set({ bot_token: e.target.value })} /></Field>
          <Field label="Chat ID"><Input disabled={!f.enabled} value={f.chat_id} onChange={(e) => set({ chat_id: e.target.value })} /></Field>
          <Field label="接口地址" hint="默认 https://api.telegram.org，可填自建反代。"><Input disabled={!f.enabled} value={f.api_base} onChange={(e) => set({ api_base: e.target.value })} /></Field>
          {num("user_quota_percent", "用户用量提醒（%）", "0 表示不提醒。", 100)}
          {num("user_expiry_days", "用户到期前提醒（天）", "0 表示不提醒。")}
          {num("source_expiry_days", "订阅源到期前提醒（天）", "按机场报告的到期时间。")}
        </FormGrid>
        <div className="settings-group">
          <SwitchField placement="end" label="VPS 离线超过 5 分钟时提醒" checked={f.gate_offline} onCheckedChange={(gate_offline) => set({ gate_offline })} />
        </div>
        <div className="actions">
          <Button type="submit" disabled={save.busy || !dirty} busy={save.busy} busyText="正在保存…">保存</Button>
          {dirty && <Button type="button" variant="ghost" disabled={save.busy} onClick={() => setF({ ...value, bot_token: "" })}>放弃修改</Button>}
          {value.enabled && value.token_set && (
            <Button type="button" variant="outline" disabled={isBusy() || dirty} busy={isBusy("test")} busyText="正在发送…" onClick={() => void run("test", () => api("/admin/settings/notify/test", "POST", {}), "测试通知已发出", { wait: { label: "正在发送测试通知", max: 15 } })}>
              发送测试通知
            </Button>
          )}
          <ActionsWait wait={waitFor("test")} />
        </div>
        {dirty && <small className="text-[var(--weir-ink-3)]">未保存；先保存，再测试。</small>}
        {save.error && <Notice tone="bad">{save.error}</Notice>}
      </Form>
    </Section>
  );
}

interface WebdavView { url: string; username: string; password_set: boolean; keep: number; auto: boolean; hours: number; last_run_at?: string | null; last_error?: string }
interface WebdavFile { name: string; size: number; modified?: string }
interface ImportStats { sources: number; nodes: number; profiles: number; templates: number; skipped: string[] | null }

export function BackupCard() {
  const q = useQuery({ queryKey: ["webdav"], queryFn: () => api("/admin/backup/webdav") as Promise<WebdavView> });
  return (
    <Section title="备份与迁移" description="订阅源、手动节点、订阅组与规则模板。导入只新增，不删除现有数据；用户与 VPS 不在备份里。">
      <ImportExport />
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <WebdavForm value={q.data} saved={() => q.refetch()} />}
    </Section>
  );
}

function StatsNotice({ s }: { s: ImportStats }) {
  return (
    <Notice tone="ok">
      已导入：{s.sources} 个订阅源、{s.nodes} 个手动节点、{s.profiles} 个订阅组、{s.templates} 个模板。
      {(s.skipped ?? []).length > 0 && (
        <>
          <br />跳过 {s.skipped!.length} 项：{s.skipped!.slice(0, 5).join("；")}
          {s.skipped!.length > 5 ? "…" : ""}
        </>
      )}
    </Notice>
  );
}

async function exportBackup() {
  const res = await fetch("/api/v1/admin/backup/export", { credentials: "same-origin" });
  if (!res.ok) throw new Error("导出失败，请重试。");
  const name = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") ?? "")?.[1] ?? "weir-subscriptions.json";
  saveBlob(await res.blob(), name);
}

function ImportExport() {
  const [content, setContent] = useState("");
  const [fileError, setFileError] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const [stats, setStats] = useState<ImportStats | null>(null);
  const save = useReauthAction(() => api("/admin/backup/import", "POST", { content, ...re }) as Promise<ImportStats>, {
    ok: "导入完成",
    fields: { invalid_backup: "content", invalid_inventory: "content" },
    onSuccess: setStats,
  });
  const { run, isBusy, waitFor } = useRowAction();
  return (
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <div className="actions">
        <Button type="button" variant="outline" disabled={isBusy()} busy={isBusy("export")} busyText="正在导出…" onClick={() => void run("export", exportBackup, "已导出", { fail: "导出失败" })}>
          <Download />
          导出备份
        </Button>
      </div>
      <Field label="导入备份" name="content" hint="粘贴备份文件内容，或在下方选择文件。" customError={content && !content.trim() ? "请输入备份内容" : ""}>
        <Textarea required rows={4} className="font-mono text-meta" value={content} onChange={(e) => setContent(e.target.value)} />
      </Field>
      <Field label="选择备份文件" error={fileError}>
        <FilePicker accept="application/json,.json" onError={setFileError} onFile={async (file) => { setFileError(""); setContent(await file.text()); }}>
          选择备份文件（.json），或拖到这里
        </FilePicker>
      </Field>
      <Reauth value={re} onChange={setRe} />
      <div className="actions">
        <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在导入…">导入</Button>
      </div>
      {save.error && <Notice tone="bad">{save.error}</Notice>}
      {stats && <StatsNotice s={stats} />}
    </Form>
  );
}

function WebdavForm({ value, saved }: { value: WebdavView; saved: () => void }) {
  const confirm = useConfirm();
  // 从 WebDAV 导入备份单独一个表单，要求当前密码；保存设置不需要
  const restoreForm = useRef<HTMLFormElement>(null);
  const [f, setF] = useState({ url: value.url, username: value.username, password: "", keep: String(value.keep), auto: value.auto, hours: String(value.hours) });
  const [re, setRe] = useState(emptyReauth);
  const [files, setFiles] = useState<WebdavFile[] | null>(null);
  const [stats, setStats] = useState<ImportStats | null>(null);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const save = useFormAction(() => api("/admin/backup/webdav", "PUT", { ...f, keep: Number(f.keep) || 10, hours: Number(f.hours) || 24 }), {
    ok: "已保存",
    fields: { invalid_webdav_url: "url" },
    onSuccess: saved,
  });
  // 要导入的文件：用 ref 传给请求，避免读到上一次渲染的值；state 只用于按钮文案
  const target = useRef("");
  const [restoring, setRestoring] = useState("");
  const restore = useReauthAction(() => api("/admin/backup/webdav/restore", "POST", { file: target.current, ...re }) as Promise<ImportStats>, { ok: "导入完成", onSuccess: setStats });
  const { run, isBusy, waitFor } = useRowAction();
  const busy = save.busy || restore.busy;
  return (
    <>
      <Form busy={busy} errors={save.errors} onSubmit={() => void save.submit()}>
        <h3 className="text-body font-semibold">WebDAV 备份</h3>
        <small className="text-[var(--weir-ink-3)]">
          最近一次：{date(value.last_run_at ?? undefined)}
          {value.last_error ? ` · 失败：${value.last_error}` : ""}
        </small>
        <FormGrid>
          <Field label="目录地址" name="url" hint="例如 https://dav.example.com/backup"><Input type="url" value={f.url} onChange={(e) => set({ url: e.target.value })} /></Field>
          <Field label="用户名"><Input autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} /></Field>
          <Field label="密码" hint={value.password_set ? "已保存；留空则沿用。" : undefined}><PasswordInput autoComplete="new-password" value={f.password} onChange={(e) => set({ password: e.target.value })} /></Field>
          <Field label="保留份数" customError={rangeError(f.keep, 1, 100, "份")}><Input inputMode="numeric" value={f.keep} onChange={(e) => set({ keep: e.target.value.replace(/\D/g, "") })} /></Field>
          <Field label="自动备份间隔（小时）" customError={rangeError(f.hours, 1, 720, "小时")}><Input inputMode="numeric" value={f.hours} onChange={(e) => set({ hours: e.target.value.replace(/\D/g, "") })} /></Field>
        </FormGrid>
        <div className="settings-group">
          <SwitchField placement="end" label="自动备份" checked={f.auto} onCheckedChange={(auto) => set({ auto })} />
        </div>
        {save.error && <Notice tone="bad">{save.error}</Notice>}
        <div className="actions">
          <Button type="submit" disabled={busy} busy={save.busy} busyText="正在保存…">保存</Button>
          {value.url && (
            <Button type="button" variant="outline" disabled={isBusy()} busy={isBusy("run")} busyText="正在备份…" onClick={() => void run("run", (track) => api("/admin/backup/webdav/run", "POST", {}, track), "备份已上传", { wait: { label: "正在上传备份到 WebDAV", max: 30 } }).then((ok) => ok && saved())}>
              立即备份
            </Button>
          )}
          {value.url && (
            <Button type="button" variant="outline" disabled={isBusy()} busy={isBusy("files")} busyText="正在读取…" onClick={() => void run("files", async () => setFiles((await api("/admin/backup/webdav/files")).files), undefined, { wait: { label: "正在读取 WebDAV 目录", max: 30 } })}>
              查看备份
            </Button>
          )}
          <ActionsWait wait={waitFor("run") || waitFor("files")} />
        </div>
      </Form>
      {files &&
        (files.length === 0 ? (
          <State empty="目录里还没有备份" />
        ) : (
          <Form ref={restoreForm} busy={restore.busy} errors={restore.errors} onSubmit={() => undefined}>
            <Reauth value={re} onChange={setRe} reason="从备份导入要验证管理员身份。" />
            <ul className="stack">
              {files.map((file) => (
                <li key={file.name} className="flex items-center justify-between gap-2">
                  <span className="min-w-0"><code className="text-meta">{file.name}</code><small className="block text-[var(--weir-ink-3)]">{file.modified || "时间未知"} · {file.size ? `${Math.round(file.size / 1024)} KiB` : "大小未知"}</small></span>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={async () => {
                      // 导入需要当前密码：未填写时在密码栏下提示，不弹确认框
                      if (!restoreForm.current?.reportValidity()) return;
                      if (!(await confirm({ title: `从 ${file.name} 导入？`, description: "只新增，不删除现有数据。", confirmText: "导入" }))) return;
                      target.current = file.name;
                      setRestoring(file.name);
                      void restore.submit();
                    }}
                    busy={restore.busy && restoring === file.name}
                    busyText="正在导入…"
                  >
                    导入这份
                  </Button>
                </li>
              ))}
            </ul>
            {restore.error && <Notice tone="bad">{restore.error}</Notice>}
            {stats && <StatsNotice s={stats} />}
          </Form>
        ))}
    </>
  );
}

interface APIToken { id: number; name: string; created_at: string; last_used_at?: string }

export function APITokensCard() {
  const confirm = useConfirm();
  const q = useQuery({ queryKey: ["api-tokens"], queryFn: async () => (await api("/admin/api-tokens")).items as APIToken[] });
  const [name, setName] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const [created, setCreated] = useState("");
  const [open, setOpen] = useState(false);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  const create = useReauthAction(() => api("/admin/api-tokens", "POST", { name: name.trim(), ...re }) as Promise<{ token: string }>, {
    ok: "已创建",
    refetch: q.refetch,
    onSuccess: (r) => {
      setCreated(r.token);
      setName("");
      setOpen(true);
    },
  });
  return (
    <Section title="外部接口" description="脚本可用 Authorization: Bearer <令牌> 调用 /api/ext/v1 管理订阅源、手动节点与订阅组；不能访问用户、VPS 与系统设置。">
      {q.isPending || q.error ? (
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      ) : q.data.length === 0 ? (
        <State empty="还没有 API 令牌" detail="给脚本或其他程序调用 Weir 接口时才需要。" />
      ) : (
        <ul className="stack">
          {q.data.map((t) => (
            <li key={t.id} className="flex items-center justify-between gap-2">
              <span>
                {t.name}
                <small className="block text-[var(--weir-ink-3)]">创建 {date(t.created_at)} · 最近使用 {t.last_used_at ? date(t.last_used_at) : "从未使用"}</small>
              </span>
              <Button
                size="icon-sm"
                variant="destructive"
                aria-label={"撤销 " + t.name}
                disabled={isBusy()}
                busy={isBusy(t.id)}
                busyText="正在撤销"
                onClick={async () => (await confirm({ title: `撤销令牌「${t.name}」？`, description: "使用它的脚本会立即失效。", confirmText: "撤销", tone: "danger" })) && run(t.id, () => api("/admin/api-tokens/" + t.id, "DELETE"), "已撤销")}
              >
                <Trash2 />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Button onClick={() => setOpen(true)}>新建令牌</Button>
      <Dialog open={open} onOpenChange={(v) => !v && !create.busy && setOpen(false)}>
        <DialogContent>
          <DialogTitle>{created ? "令牌已创建" : "新建令牌"}</DialogTitle>
          <DialogDescription>{created ? "令牌只显示这一次，请复制保存后确认关闭。" : "令牌只能访问订阅数据接口。"}</DialogDescription>
          {created ? <CopyField label="新令牌" value={created} /> : <Form busy={create.busy} errors={create.errors} onSubmit={() => void create.submit()}>
            <Field label="新令牌名称" customError={name && !name.trim() ? "请输入新令牌名称" : ""}><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Reauth value={re} onChange={setRe} />
            <DialogFooter error={create.error}><Button type="button" variant="outline" disabled={create.busy} onClick={() => setOpen(false)}>取消</Button><Button type="submit" disabled={create.busy} busy={create.busy} busyText="正在创建…">创建令牌</Button></DialogFooter>
          </Form>}
          {created && <DialogFooter><Button onClick={() => { setCreated(""); setRe(emptyReauth); setOpen(false); }}>我已保存</Button></DialogFooter>}
        </DialogContent>
      </Dialog>
    </Section>
  );
}
