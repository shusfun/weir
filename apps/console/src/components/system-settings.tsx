// 系统设置卡片：安全策略、邮件服务、通知、WebDAV 备份、MiSub 备份导入导出、API 令牌。修改都需要重新输入密码。
import { useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, CardContent, CopyButton, Field, Form, FormGrid, Input, Notice, PasswordInput, SectionHeader, SwitchField, Textarea, useConfirm } from "@weir/ui";
import { Download, Trash2 } from "lucide-react";
import { api, reauthFields } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { saveBlob } from "../lib/download";
import { date } from "../lib/models";
import { State } from "./feedback";

export type ReauthValue = { password: string; code: string };
export const emptyReauth: ReauthValue = { password: "", code: "" };

// Reauth 是各卡片共用的「当前密码 + 二次验证码」输入；放在 <Form> 里，服务端的验证错误标在对应字段下方。
// reason：为什么要验证（显示在字段下方）。need=false 时这次改动不需要验证：字段保留占位但禁用，
// 改动在要 / 不要之间切换时表单高度不变。
export function Reauth({ value, onChange, need = true, reason }: { value: ReauthValue; onChange: (v: ReauthValue) => void; need?: boolean; reason?: string }) {
  const hint = need ? reason : "这次改动不需要验证身份。";
  return (
    <FormGrid>
      <Field label="当前密码" name="password" hint={hint}>
        <PasswordInput autoComplete="current-password" required={need} disabled={!need} value={need ? value.password : ""} onChange={(e) => onChange({ ...value, password: e.target.value })} />
      </Field>
      <Field label="二次验证码（如已开启）" name="code">
        <Input inputMode="numeric" autoComplete="one-time-code" disabled={!need} value={need ? value.code : ""} onChange={(e) => onChange({ ...value, code: e.target.value })} />
      </Field>
    </FormGrid>
  );
}

// 需要重新验证身份的保存：在 useFormAction 的基础上把密码 / 验证码错误对应到 Reauth 的字段
export function useReauthAction<T>(fn: () => Promise<T>, opts: Parameters<typeof useFormAction<T>>[1] = {}) {
  return useFormAction<T>(fn, { ...opts, fields: { ...reauthFields, ...opts.fields } });
}

export function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title={title} description={description} />
        {children}
      </CardContent>
    </Card>
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
  const [draft, setDraft] = useState<Record<string, boolean> | null>(null);
  const [re, setRe] = useState(emptyReauth);
  const v = draft ?? q.data;
  const save = useReauthAction(() => api("/admin/settings/security", "PUT", { ...v, ...re }), {
    ok: "已保存",
    onSuccess: () => {
      setDraft(null);
      setRe(emptyReauth);
      void q.refetch();
    },
  });
  return (
    <Section title="安全策略" description="登录与注册方式。">
      {q.isPending || q.error || !v ? (
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      ) : (
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            {securityFlags.map(([k, label, description]) => (
              <SwitchField key={k} label={label} description={description || undefined} checked={!!v[k]} onCheckedChange={(on) => setDraft({ ...v, [k]: on })} />
            ))}
          </FormGrid>
          <Reauth value={re} onChange={setRe} />
          <div className="actions">
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
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
  return <SMTPForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />;
}

function SMTPForm({ value, saved }: { value: SMTPView; saved: () => void }) {
  const [f, setF] = useState({ host: value.host, port: String(value.port || 465), username: value.username, password: "", from: value.from, from_name: value.from_name, use_tls: value.use_tls || !value.host });
  const [re, setRe] = useState(emptyReauth);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const save = useReauthAction(() => api("/admin/settings/smtp", "PUT", { ...f, port: Number(f.port) || 0, account_password: re.password, code: re.code }), { ok: "已保存", onSuccess: saved });
  const { run, isBusy } = useRowAction();
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
        <SwitchField label="使用 TLS" description="465 端口直接 TLS，其他端口 STARTTLS。" checked={f.use_tls} onCheckedChange={(use_tls) => set({ use_tls })} />
        <Reauth value={re} onChange={setRe} />
        <div className="actions">
          <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          {value.configured && (
            <Button type="button" variant="outline" disabled={isBusy()} onClick={() => void run("test", () => api("/admin/settings/smtp/test", "POST", {}), "测试邮件已发出")}>
              发送测试邮件
            </Button>
          )}
        </div>
        {save.error && <Notice tone="bad">{save.error}</Notice>}
      </Form>
    </Section>
  );
}

interface NotifyView { enabled: boolean; token_set: boolean; chat_id: string; api_base: string; gate_offline: boolean; user_quota_percent: number; user_expiry_days: number; source_expiry_days: number; source_refresh_hours: number }

export function NotifyCard() {
  const q = useQuery({ queryKey: ["notify-settings"], queryFn: () => api("/admin/settings/notify") as Promise<NotifyView> });
  if (q.isPending || q.error)
    return (
      <Section title="通知与定时任务" description="Telegram 提醒与订阅源定时刷新。">
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      </Section>
    );
  return <NotifyForm key={q.dataUpdatedAt} value={q.data} saved={() => q.refetch()} />;
}

function NotifyForm({ value, saved }: { value: NotifyView; saved: () => void }) {
  const [f, setF] = useState({ ...value, bot_token: "" });
  const [re, setRe] = useState(emptyReauth);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const save = useReauthAction(() => api("/admin/settings/notify", "PUT", { ...f, ...re }), { ok: "已保存", onSuccess: saved });
  const { run, isBusy } = useRowAction();
  const num = (k: "user_quota_percent" | "user_expiry_days" | "source_expiry_days" | "source_refresh_hours", label: string, hint: string, max?: number) => (
    <Field label={label} hint={hint} customError={max !== undefined && f[k] > max ? `不能大于 ${max}` : ""}>
      <Input inputMode="numeric" value={String(f[k])} onChange={(e) => set({ [k]: Number(e.target.value.replace(/\D/g, "")) || 0 })} />
    </Field>
  );
  return (
    <Section title="通知与定时任务" description="提醒只包含名称与数值，不含订阅地址或凭据；同一事件 24 小时内只提醒一次。">
      <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
        <SwitchField label="开启 Telegram 通知" checked={f.enabled} onCheckedChange={(enabled) => set({ enabled })} />
        <FormGrid>
          <Field label="Bot Token" hint={value.token_set ? "已保存；留空则沿用。" : undefined}><PasswordInput autoComplete="off" value={f.bot_token} onChange={(e) => set({ bot_token: e.target.value })} /></Field>
          <Field label="Chat ID"><Input value={f.chat_id} onChange={(e) => set({ chat_id: e.target.value })} /></Field>
          <Field label="接口地址" hint="默认 https://api.telegram.org，可填自建反代。"><Input value={f.api_base} onChange={(e) => set({ api_base: e.target.value })} /></Field>
          {num("user_quota_percent", "用户用量提醒（%）", "0 表示不提醒。", 100)}
          {num("user_expiry_days", "用户到期前提醒（天）", "0 表示不提醒。")}
          {num("source_expiry_days", "订阅源到期前提醒（天）", "按机场报告的到期时间。")}
          {num("source_refresh_hours", "订阅源定时刷新（小时）", "0 表示只在访问订阅组时按缓存刷新。")}
        </FormGrid>
        <SwitchField label="VPS 离线超过 5 分钟时提醒" checked={f.gate_offline} onCheckedChange={(gate_offline) => set({ gate_offline })} />
        <Reauth value={re} onChange={setRe} />
        <div className="actions">
          <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          {value.enabled && value.token_set && (
            <Button type="button" variant="outline" disabled={isBusy()} onClick={() => void run("test", () => api("/admin/settings/notify/test", "POST", {}), "测试通知已发出")}>
              发送测试通知
            </Button>
          )}
        </div>
        {save.error && <Notice tone="bad">{save.error}</Notice>}
      </Form>
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
  const name = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") ?? "")?.[1] ?? "misub-backup.json";
  saveBlob(await res.blob(), name);
}

function ImportExport() {
  const [content, setContent] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const [stats, setStats] = useState<ImportStats | null>(null);
  const save = useReauthAction(() => api("/admin/backup/import", "POST", { content, ...re }) as Promise<ImportStats>, {
    ok: "导入完成",
    fields: { invalid_backup: "content", invalid_inventory: "content" },
    onSuccess: setStats,
  });
  const { run, isBusy } = useRowAction();
  return (
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <div className="actions">
        <Button type="button" variant="outline" disabled={isBusy()} onClick={() => void run("export", exportBackup, "已导出", { fail: "导出失败" })}>
          <Download />
          导出备份
        </Button>
      </div>
      <Field label="导入 MiSub 备份" name="content" hint="粘贴备份文件内容，或在下方选择文件。" customError={content && !content.trim() ? "请输入备份内容" : ""}>
        <Textarea required rows={4} className="font-mono text-xs" value={content} onChange={(e) => setContent(e.target.value)} />
      </Field>
      <Field label="选择备份文件">
        <Input
          type="file"
          accept="application/json,.json"
          onChange={async (e) => {
            const file = e.target.files?.[0];
            if (file) setContent(await file.text());
          }}
        />
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
  const form = useRef<HTMLFormElement>(null);
  const [f, setF] = useState({ url: value.url, username: value.username, password: "", keep: String(value.keep), auto: value.auto, hours: String(value.hours) });
  const [re, setRe] = useState(emptyReauth);
  const [files, setFiles] = useState<string[] | null>(null);
  const [stats, setStats] = useState<ImportStats | null>(null);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const save = useReauthAction(() => api("/admin/backup/webdav", "PUT", { ...f, keep: Number(f.keep) || 10, hours: Number(f.hours) || 24, account_password: re.password, code: re.code }), {
    ok: "已保存",
    fields: { invalid_webdav_url: "url" },
    onSuccess: saved,
  });
  // 要导入的文件：用 ref 传给请求，避免读到上一次渲染的值；state 只用于按钮文案
  const target = useRef("");
  const [restoring, setRestoring] = useState("");
  const restore = useReauthAction(() => api("/admin/backup/webdav/restore", "POST", { file: target.current, ...re }) as Promise<ImportStats>, { ok: "导入完成", onSuccess: setStats });
  const { run, isBusy } = useRowAction();
  const busy = save.busy || restore.busy;
  return (
    <Form ref={form} busy={busy} errors={save.errors ?? restore.errors} onSubmit={() => void save.submit()}>
      <h3 className="text-sm font-semibold">WebDAV 备份</h3>
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
      <SwitchField label="自动备份" checked={f.auto} onCheckedChange={(auto) => set({ auto })} />
      <Reauth value={re} onChange={setRe} />
      {(save.error || restore.error) && <Notice tone="bad">{save.error || restore.error}</Notice>}
      {stats && <StatsNotice s={stats} />}
      <div className="actions">
        <Button type="submit" disabled={busy} busy={save.busy} busyText="正在保存…">保存</Button>
        {value.url && (
          <Button type="button" variant="outline" disabled={isBusy()} onClick={() => void run("run", () => api("/admin/backup/webdav/run", "POST", {}), "备份已上传").then((ok) => ok && saved())}>
            立即备份
          </Button>
        )}
        {value.url && (
          <Button type="button" variant="outline" disabled={isBusy()} onClick={() => void run("files", async () => setFiles((await api("/admin/backup/webdav/files")).files))}>
            查看备份
          </Button>
        )}
      </div>
      {files &&
        (files.length === 0 ? (
          <State empty="目录里还没有备份" />
        ) : (
          <ul className="stack">
            {files.map((name) => (
              <li key={name} className="flex items-center justify-between gap-2">
                <code className="text-xs">{name}</code>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={async () => {
                    // 导入需要当前密码：未填写时在密码栏下提示，不弹确认框
                    if (!form.current?.reportValidity()) return;
                    if (!(await confirm({ title: `从 ${name} 导入？`, description: "只新增，不删除现有数据。", confirmText: "导入" }))) return;
                    target.current = name;
                    setRestoring(name);
                    void restore.submit();
                  }}
                  busy={restore.busy && restoring === name}
                  busyText="正在导入…"
                >
                  导入这份
                </Button>
              </li>
            ))}
          </ul>
        ))}
    </Form>
  );
}

interface APIToken { id: number; name: string; created_at: string; last_used_at?: string }

export function APITokensCard() {
  const confirm = useConfirm();
  const q = useQuery({ queryKey: ["api-tokens"], queryFn: async () => (await api("/admin/api-tokens")).items as APIToken[] });
  const [name, setName] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const [created, setCreated] = useState("");
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  const create = useReauthAction(() => api("/admin/api-tokens", "POST", { name: name.trim(), ...re }) as Promise<{ token: string }>, {
    ok: "已创建",
    refetch: q.refetch,
    onSuccess: (r) => {
      setCreated(r.token);
      setName("");
    },
  });
  return (
    <Section title="外部接口" description="脚本可用 Authorization: Bearer <令牌> 调用 /api/ext/v1 管理订阅源、手动节点与订阅组；不能访问用户、VPS 与系统设置。">
      {q.isPending || q.error ? (
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
      ) : q.data.length === 0 ? (
        <State empty="还没有 API 令牌" />
      ) : (
        <ul className="stack">
          {q.data.map((t) => (
            <li key={t.id} className="flex items-center justify-between gap-2">
              <span>
                {t.name}
                <small className="block text-[var(--weir-ink-3)]">创建 {date(t.created_at)} · 最近使用 {date(t.last_used_at)}</small>
              </span>
              <Button
                size="icon-sm"
                variant="destructive"
                aria-label={"撤销 " + t.name}
                disabled={isBusy(t.id)}
                onClick={async () => (await confirm({ title: `撤销令牌「${t.name}」？`, description: "使用它的脚本会立即失效。", confirmText: "撤销", tone: "danger" })) && run(t.id, () => api("/admin/api-tokens/" + t.id, "DELETE"), "已撤销")}
              >
                <Trash2 />
              </Button>
            </li>
          ))}
        </ul>
      )}
      {created && (
        <div className="subscription-url">
          <Input readOnly className="font-mono text-xs" aria-label="新令牌" value={created} />
          <CopyButton value={created}>复制</CopyButton>
        </div>
      )}
      {created && <Notice role="status">令牌只显示这一次，请现在复制保存。</Notice>}
      <Form busy={create.busy} errors={create.errors} onSubmit={() => void create.submit()}>
        <Field label="新令牌名称" customError={name && !name.trim() ? "请输入新令牌名称" : ""}>
          <Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Reauth value={re} onChange={setRe} />
        <div className="actions">
          <Button type="submit" disabled={create.busy} busy={create.busy} busyText="正在创建…">创建令牌</Button>
        </div>
        {create.error && <Notice tone="bad">{create.error}</Notice>}
      </Form>
    </Section>
  );
}
