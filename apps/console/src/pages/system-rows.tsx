// 设置页的行：开关立刻保存，要填的项显示当前值并打开原有弹窗。
import { useState } from "react";
import { Notice, SwitchField, useConfirm } from "@weir/ui";
import { api, formError } from "../api";
import { useQuery } from "../lib/query";
import { useQueryClient } from "@tanstack/react-query";
import { pushAccountAppearance, readAppearance, writeAppearance } from "../appearance";
import { useLogo } from "../components/brand-mark";
import type { DshSettings } from "../components/dsh";

type Open = (key: string) => void;

function text(pending: boolean, error: unknown, value: string) {
  if (error) return "未知";
  if (pending) return "正在读取";
  return value;
}

function ValueRow({ label, detail, value, onOpen }: { label: string; detail?: string; value: string; onOpen: () => void }) {
  return <button type="button" className="weir-set-row settings-value" onClick={onOpen}>
    <span><strong>{label}</strong>{detail ? <small className="block text-[var(--weir-ink-3)]">{detail}</small> : null}</span>
    <b>{value}</b>
  </button>;
}

export function AccountRows({ open }: { open: Open }) {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api("/me") as Promise<{ totp_enrolled?: boolean }> });
  const keys = useQuery({ queryKey: ["self-passkeys"], queryFn: () => api("/security/passkeys") as Promise<{ items: { id: number }[] }> });
  const sessions = useQuery({ queryKey: ["self-sessions", "summary"], queryFn: () => api("/sessions?page=1&page_size=1") as Promise<{ all_total?: number }> });
  return <>
    <ValueRow label="密码" detail="修改后其他会话需要重新登录" value="已设置" onOpen={() => open("account")} />
    <ValueRow label="二次验证" value={text(me.isPending, me.error, me.data?.totp_enrolled ? "已绑定" : "未绑定")} onOpen={() => open("account")} />
    <ValueRow label="通行密钥" value={text(keys.isPending, keys.error, keys.data ? `${keys.data.items.length} 个` : "未知")} onOpen={() => open("account")} />
    <ValueRow label="已登录设备" value={text(sessions.isPending, sessions.error, sessions.data?.all_total == null ? "未知" : `${sessions.data.all_total} 个`)} onOpen={() => open("account")} />
  </>;
}

export function AppearanceRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const current = readAppearance();
  const theme = current.theme === "dark" ? "暗色" : current.theme === "light" ? "明亮" : "跟随系统";
  const motion = current.motion === "full" ? "完整" : current.motion === "lite" ? "省电" : "均衡";
  async function toggle(patch: Partial<ReturnType<typeof readAppearance>>) {
    const previous = readAppearance();
    const next = { ...previous, ...patch };
    setBusy(true);
    setError("");
    writeAppearance(next);
    const why = await pushAccountAppearance(next);
    setBusy(false);
    if (why) {
      writeAppearance(previous);
      setError(why);
    }
    void client.invalidateQueries({ queryKey: ["me-appearance"] });
  }
  return <>
    {error && <Notice tone="bad">{error}</Notice>}
    <ValueRow label="主题" value={theme} onOpen={() => open("appearance")} />
    <ValueRow label="动效档" value={motion} onOpen={() => open("appearance")} />
    <SwitchField placement="end" label="跟随系统的减少动态效果" description="系统开启时自动进入省电" checked={current.followReduce} disabled={busy} onCheckedChange={(on) => void toggle({ followReduce: on })} />
    <SwitchField placement="end" label="检测到卡顿时提示切换" description="同一浏览器 7 天内最多提示一次" checked={current.stutter} disabled={busy} onCheckedChange={(on) => void toggle({ stutter: on })} />
    <SwitchField placement="end" label="背景地球" description="右下角显示节点状态，手机上不显示" checked={current.globe} disabled={busy} onCheckedChange={(on) => void toggle({ globe: on })} />
  </>;
}

export function UpdateRows({ open }: { open: Open }) {
  const status = useQuery({ queryKey: ["self-update-status"], queryFn: () => api("/admin/system/update/status") as Promise<{ current?: string }> });
  const sources = useQuery({ queryKey: ["github-sources-admin"], queryFn: () => api("/admin/github-sources") as Promise<{ sources?: unknown[] }> });
  return <>
    <ValueRow label="当前版本" value={text(status.isPending, status.error, status.data?.current || "未知")} onOpen={() => open("update")} />
    <ValueRow label="下载来源" value={text(sources.isPending, sources.error, sources.data?.sources ? `${sources.data.sources.length} 项` : "未知")} onOpen={() => open("github")} />
  </>;
}

export function LoginRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const confirm = useConfirm();
  const security = useQuery({ queryKey: ["security-settings"], queryFn: () => api("/admin/settings/security") as Promise<Record<string, boolean>> });
  const smtp = useQuery({ queryKey: ["smtp-settings"], queryFn: () => api("/admin/settings/smtp") as Promise<{ configured: boolean; host?: string }> });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api("/me") as Promise<{ totp_enrolled?: boolean }> });
  const turnstile = useQuery({ queryKey: ["turnstile-settings"], queryFn: () => api("/admin/settings/turnstile") as Promise<{ configured?: boolean }> });
  const policy = useQuery({ queryKey: ["account-policy"], queryFn: () => api("/admin/settings/account-policy") as Promise<{ revision?: string }> });
  const logo = useLogo();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const flags = security.data;
  async function toggle(key: string, on: boolean) {
    if (!flags) return;
    if (key === "require_admin_mfa" && on && !me.data?.totp_enrolled && !(await confirm({ title: "当前管理员还没有绑定二次验证", description: "保存后需要先完成二次验证，未完成前不能继续管理后台。", confirmText: "继续" }))) return;
    const next = { ...flags, [key]: on };
    setBusy(true);
    setError("");
    try {
      await api("/admin/settings/security", "PUT", next);
      await client.invalidateQueries({ queryKey: ["security-settings"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const mailOff = !smtp.data?.configured || !flags?.registration_enabled;
  return <>
    {error && <Notice tone="bad">{error}</Notice>}
    {([
      ["totp_enabled", "允许二次验证", "关闭后已绑定用户仍可使用现有验证器"],
      ["passkey_enabled", "允许通行密钥登录", "关闭后不能绑定或用通行密钥登录"],
      ["require_admin_mfa", "管理员必须开启二次验证", "需要先允许二次验证"],
      ["registration_enabled", "允许自助注册", "未附带套餐的新用户没有流量与节点"],
      ["email_verification", "注册时验证邮箱", mailOff ? (!smtp.data?.configured ? "需要先配置邮件服务" : "需要先允许自助注册") : "需要邮件服务"],
    ] as const).map(([key, label, detail]) => <SwitchField key={key} placement="end" label={label} description={detail} checked={!!flags?.[key]} disabled={busy || !flags || (key === "email_verification" && mailOff) || (key === "require_admin_mfa" && !flags.totp_enabled)} onCheckedChange={(on) => void toggle(key, on)} />)}
    <ValueRow label="邮件服务" value={text(smtp.isPending, smtp.error, smtp.data?.configured ? (smtp.data.host || "已配置") : "未配置")} onOpen={() => open("smtp")} />
    <ValueRow label="人机验证" value={text(turnstile.isPending, turnstile.error, turnstile.data?.configured ? "已配置" : "未配置")} onOpen={() => open("turnstile")} />
    <ValueRow label="账号策略" value={text(policy.isPending, policy.error, policy.data?.revision ? "已保存" : "未配置")} onOpen={() => open("accounts")} />
    <ValueRow label="登录页标识" value={text(logo.isPending, logo.error, logo.data?.configured ? "已上传" : "未配置")} onOpen={() => open("logo")} />
  </>;
}

export function NotifyRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const q = useQuery({ queryKey: ["notify-settings"], queryFn: () => api("/admin/settings/notify") as Promise<Record<string, unknown>> });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function toggle(key: "enabled" | "gate_offline", on: boolean) {
    if (!q.data) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/settings/notify", "PUT", { ...q.data, bot_token: "", [key]: on });
      await client.invalidateQueries({ queryKey: ["notify-settings"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const data = q.data;
  return <>
    {error && <Notice tone="bad">{error}</Notice>}
    <SwitchField placement="end" label="开启 Telegram 通知" description="提醒不含订阅地址或凭据" checked={!!data?.enabled} disabled={busy || !data} onCheckedChange={(on) => void toggle("enabled", on)} />
    <SwitchField placement="end" label="VPS 离线超过 5 分钟时提醒" checked={!!data?.gate_offline} disabled={busy || !data} onCheckedChange={(on) => void toggle("gate_offline", on)} />
    <ValueRow label="通知地址与模板" value={text(q.isPending, q.error, data?.token_set ? "已保存 Bot Token" : "未配置")} onOpen={() => open("notify")} />
  </>;
}

export function DefenseRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const q = useQuery({ queryKey: ["laya"], queryFn: () => api("/admin/defense/laya") as Promise<{ enabled: boolean; verified?: boolean; mode?: string; gate_id?: number; url: string; model: string; warmup_seconds?: number }> });
  const policy = useQuery({ queryKey: ["protection-policy"], queryFn: () => api("/admin/defense/policy") as Promise<{ revision?: string }> });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function toggle(on: boolean) {
    if (!q.data) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/defense/laya", "PUT", { mode: q.data.mode, gate_id: q.data.gate_id, enabled: on, url: q.data.url, model: q.data.model, warmup_seconds: q.data.warmup_seconds ?? 300, key: "" });
      await client.invalidateQueries({ queryKey: ["laya"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  return <>
    {error && <Notice tone="bad">{error}</Notice>}
    <SwitchField placement="end" label="启用 Laya 判断" description={q.data && !q.data.verified && !q.data.enabled ? "需要先通过实测" : "缺条件时保存会失败并说明原因"} checked={!!q.data?.enabled} disabled={busy || !q.data || (!q.data.enabled && !q.data.verified)} onCheckedChange={(on) => void toggle(on)} />
    <ValueRow label="防御策略" value={text(policy.isPending, policy.error, policy.data?.revision ? "已保存" : "未配置")} onOpen={() => open("protection")} />
  </>;
}

export function AIRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const q = useQuery({ queryKey: ["dsh"], queryFn: () => api("/admin/dsh") as Promise<{ settings: DshSettings }> });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function toggle(patch: Partial<DshSettings>) {
    if (!q.data) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/dsh/settings", "PUT", { ...q.data.settings, ...patch });
      await client.invalidateQueries({ queryKey: ["dsh"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const settings = q.data?.settings;
  return <>
    {error && <Notice tone="bad">{error}</Notice>}
    <SwitchField placement="end" label="启用 dsh" description={settings?.domain ? settings.domain : "需要先在弹窗里填写地址"} checked={!!settings?.enabled} disabled={busy || !settings} onCheckedChange={(on) => void toggle({ enabled: on })} />
    <SwitchField placement="end" label="可疑事件自动交给 dsh 分析" checked={!!settings?.auto_analyze} disabled={busy || !settings} onCheckedChange={(on) => void toggle({ auto_analyze: on })} />
    <ValueRow label="dsh 地址与版本" value={text(q.isPending, q.error, settings?.domain || "未配置")} onOpen={() => open("dsh")} />
    <ValueRow label="AI 偏好" value="在弹窗中查看" onOpen={() => open("ai-profile")} />
  </>;
}

export function CertRows({ open }: { open: Open }) {
  const q = useQuery({ queryKey: ["node-certs"], queryFn: () => api("/admin/node-certs") as Promise<{ edge_domain?: string }> });
  return <ValueRow label="节点证书" detail="证书来源、申请与续签" value={text(q.isPending, q.error, q.data?.edge_domain || "未配置")} onOpen={() => open("certs")} />;
}

export function DataRows({ open }: { open: Open }) {
  const retention = useQuery({ queryKey: ["retention-policy"], queryFn: () => api("/admin/settings/retention") as Promise<{ policy?: { observation_days?: number; audit_days?: number } }> });
  const webdav = useQuery({ queryKey: ["webdav"], queryFn: () => api("/admin/backup/webdav") as Promise<{ url?: string; auto?: boolean }> });
  const tokens = useQuery({ queryKey: ["api-tokens"], queryFn: async () => (await api("/admin/api-tokens")).items as unknown[] });
  const days = retention.data?.policy?.observation_days;
  return <>
    <ValueRow label="数据保留" value={text(retention.isPending, retention.error, days == null ? "未知" : `观测 ${days} 天${retention.data?.policy?.audit_days ? "" : "，审计保留"}`)} onOpen={() => open("retention")} />
    <ValueRow label="备份与恢复" value={text(webdav.isPending, webdav.error, webdav.data?.url ? (webdav.data.auto ? "WebDAV 自动备份" : "已配置 WebDAV") : "未配置")} onOpen={() => open("backup")} />
    <ValueRow label="API 令牌" value={text(tokens.isPending, tokens.error, tokens.data ? `${tokens.data.length} 个` : "未知")} onOpen={() => open("api")} />
  </>;
}
