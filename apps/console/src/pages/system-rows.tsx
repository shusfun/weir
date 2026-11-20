// 设置页的行：开关立刻保存，要填的项打开同名弹窗。
// 行尾只写需要关注的状态（未配置的必要项、失败、待处理）；一切正常时只给一句灰字或不写，读取失败写明原因。
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Badge, Notice, SwitchField, useConfirm } from "@weir/ui";
import { useQueryClient } from "@tanstack/react-query";
import { api, clause, formError } from "../api";
import { useQuery } from "../lib/query";
import { pushAccountAppearance, readAppearance, writeAppearance } from "../appearance";
import { useLogo } from "../components/brand-mark";
import type { DshView } from "../components/dsh";
import { useCerts } from "../components/cert-manager";
import { useUpdateStatus } from "../components/update-watcher";
import { systemSettingItems } from "../shell/navigation";

type Open = (key: string) => void;
// 行尾状态：plain 为灰字，warn / bad 为需要关注的标签；没有值时行尾不显示
export type RowState = { text: string; tone?: "plain" | "warn" | "bad" | "ok" } | null;

const plain = (text: string): RowState => ({ text, tone: "plain" });
const warn = (text: string): RowState => ({ text, tone: "warn" });

// 统一查询的三种情况：读取中、读取失败（写明原因）、读到数据后由调用方给出状态
function read<T>(q: { isPending: boolean; error: unknown; data?: T }, ok: (data: T) => RowState): RowState {
  if (q.error) return { text: `读取失败：${clause(q.error)}`, tone: "bad" };
  if (q.isPending || q.data === undefined) return plain("正在读取");
  return ok(q.data);
}

// 行名与弹窗标题同源：传 setting 时行名取自 systemSettingItems
function title(setting: string) {
  return systemSettingItems.find((item) => item.key === setting)?.title ?? setting;
}

function ValueRow({ setting, label, detail, state, onOpen }: { setting?: string; label?: string; detail?: string; state: RowState; onOpen: () => void }) {
  return <button type="button" className="weir-set-row settings-value" onClick={onOpen}>
    <span className="min-w-0"><strong>{label ?? title(setting ?? "")}</strong>{detail ? <small className="block text-[var(--weir-ink-3)]">{detail}</small> : null}</span>
    {state && (state.tone === "plain"
      ? <span className="shrink-0 text-meta text-[var(--weir-ink-3)]">{state.text}</span>
      : <Badge variant="status" tone={state.tone}>{state.text}</Badge>)}
  </button>;
}

export function AccountRows({ open }: { open: Open }) {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api("/me") as Promise<{ totp_enrolled?: boolean }> });
  const keys = useQuery({ queryKey: ["self-passkeys"], queryFn: () => api("/security/passkeys") as Promise<{ items: { id: number }[] }> });
  const sessions = useQuery({ queryKey: ["self-sessions", "summary"], queryFn: () => api("/sessions?page=1&page_size=1") as Promise<{ all_total?: number }> });
  // 一行说清账户安全：先说要处理的，再说登录设备数
  const security = read(me, (data) => {
    if (!data.totp_enrolled && keys.data && keys.data.items.length === 0) return warn("未开启两步验证或通行密钥");
    if (!data.totp_enrolled) return warn("未开启两步验证");
    return null;
  });
  const devices = read(sessions, (data) => data.all_total == null ? null : plain(`${data.all_total} 台设备已登录`));
  return <>
    <ValueRow setting="account" detail="密码、两步验证、通行密钥与已登录设备" state={security ?? devices} onOpen={() => open("account")} />
    <AppearanceRows open={open} />
  </>;
}

function AppearanceRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const current = readAppearance();
  const theme = current.theme === "dark" ? "暗色" : current.theme === "light" ? "明亮" : "跟随系统";
  const motion = current.motion === "full" ? "完整动效" : current.motion === "lite" ? "省电" : "均衡";
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
    <ValueRow setting="appearance" state={plain(`${theme} · ${motion}`)} onOpen={() => open("appearance")} />
    <SwitchField placement="end" label="背景地球" description="右下角显示节点状态，手机上不显示" checked={current.globe} disabled={busy} onCheckedChange={(on) => void toggle({ globe: on })} />
  </>;
}

export function LoginRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const security = useQuery({ queryKey: ["security-settings"], queryFn: () => api("/admin/settings/security") as Promise<Record<string, boolean>> });
  const smtp = useQuery({ queryKey: ["smtp-settings"], queryFn: () => api("/admin/settings/smtp") as Promise<{ configured: boolean; host?: string }> });
  const logo = useLogo();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const flags = security.data;
  async function toggle(key: string, on: boolean) {
    if (!flags) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/settings/security", "PUT", { ...flags, [key]: on });
      await client.invalidateQueries({ queryKey: ["security-settings"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const mailReason = !smtp.data?.configured ? "需要先配置邮件服务" : !flags?.registration_enabled ? "需要先允许自助注册" : "";
  // 邮件服务只在需要它的功能打开时才算缺口
  const mail = read(smtp, (data) => data.configured ? null : flags?.email_verification ? warn("未配置，邮箱验证无法发码") : plain("未配置，登录页不提供找回密码"));
  return <>
    {security.error ? <Notice tone="bad">读取登录与注册开关失败：{clause(security.error)}</Notice> : null}
    {error && <Notice tone="bad">{error}</Notice>}
    <SwitchField placement="end" label="允许自助注册" description="未附带套餐的新用户没有流量与节点" checked={!!flags?.registration_enabled} disabled={busy || !flags} onCheckedChange={(on) => void toggle("registration_enabled", on)} />
    <SwitchField placement="end" label="注册时验证邮箱" description={mailReason || "注册时向邮箱发送验证码"} checked={!!flags?.email_verification} disabled={busy || !flags || !!mailReason} onCheckedChange={(on) => void toggle("email_verification", on)} />
    <ValueRow setting="accounts" detail="邀请码、兑换码、邮箱后缀与发信限额" state={null} onOpen={() => open("accounts")} />
    <ValueRow setting="smtp" state={mail} onOpen={() => open("smtp")} />
    <ValueRow setting="logo" state={read(logo, (data) => data.configured ? null : plain("使用默认标识"))} onOpen={() => open("logo")} />
  </>;
}

export function DefenseRows({ open }: { open: Open }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const confirm = useConfirm();
  const security = useQuery({ queryKey: ["security-settings"], queryFn: () => api("/admin/settings/security") as Promise<Record<string, boolean>> });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api("/me") as Promise<{ totp_enrolled?: boolean }> });
  const turnstile = useQuery({ queryKey: ["turnstile-settings"], queryFn: () => api("/admin/settings/turnstile") as Promise<{ configured?: boolean }> });
  const policy = useQuery({ queryKey: ["protection-policy"], queryFn: () => api("/admin/defense/policy") as Promise<{ policy?: { effective?: { enabled?: boolean } }; hourly_used?: number; hourly_limit?: number }> });
  const events = useQuery({ queryKey: ["defense-events"], queryFn: () => api("/admin/defense/events") as Promise<{ events: { status: string }[] }> });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const flags = security.data;
  async function toggle(key: string, on: boolean) {
    if (!flags) return;
    if (key === "require_admin_mfa" && on && !me.data?.totp_enrolled && !(await confirm({ title: "当前管理员还没有绑定两步验证", description: "保存后需要先完成两步验证，未完成前不能继续管理后台。", confirmText: "继续" }))) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/settings/security", "PUT", { ...flags, [key]: on });
      await client.invalidateQueries({ queryKey: ["security-settings"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const pending = read(events, (data) => {
    const n = data.events.filter((e) => e.status === "new").length;
    return n > 0 ? warn(`${n} 条待处理`) : null;
  });
  const guardrail = read(policy, (data) => !data.policy ? plain("未配置，只生成草稿") : data.policy.effective?.enabled
    ? data.hourly_used != null && data.hourly_limit != null && data.hourly_used >= data.hourly_limit ? warn(`本小时已用满 ${data.hourly_limit} 次`) : null
    : plain("只生成草稿"));
  return <>
    {security.error ? <Notice tone="bad">读取登录方式开关失败：{clause(security.error)}</Notice> : null}
    {error && <Notice tone="bad">{error}</Notice>}
    <SwitchField placement="end" label="允许两步验证" description="关闭后已绑定用户仍可使用现有验证器" checked={!!flags?.totp_enabled} disabled={busy || !flags} onCheckedChange={(on) => void toggle("totp_enabled", on)} />
    <SwitchField placement="end" label="允许通行密钥登录" description="关闭后不能绑定或用通行密钥登录" checked={!!flags?.passkey_enabled} disabled={busy || !flags} onCheckedChange={(on) => void toggle("passkey_enabled", on)} />
    <SwitchField placement="end" label="管理员必须开启两步验证" description={flags && !flags.totp_enabled ? "需要先允许两步验证" : undefined} checked={!!flags?.require_admin_mfa} disabled={busy || !flags || !flags.totp_enabled} onCheckedChange={(on) => void toggle("require_admin_mfa", on)} />
    <ValueRow setting="turnstile" detail="注册、发码、找回密码与连续登录失败时要求验证" state={read(turnstile, (data) => data.configured ? null : plain("未配置"))} onOpen={() => open("turnstile")} />
    <ValueRow setting="protection" detail="临时收紧上限、保护网段与域名" state={guardrail} onOpen={() => open("protection")} />
    <ValueRow label="安全事件与封禁名单" state={pending} onOpen={() => navigate("/ops/observe?tab=security")} />
  </>;
}

export function NodeRows({ open }: { open: Open }) {
  const certs = useCerts();
  const sources = useQuery({ queryKey: ["github-sources-admin"], queryFn: () => api("/admin/github-sources") as Promise<{ sources?: unknown[] }> });
  // 证书只说是否正常：有过期或续签失败的写数量，其次是待续签；全部正常不写
  const certState = read(certs, (items) => {
    const bad = items.filter((c) => c.health === "failed" || c.health === "expired").length;
    if (bad) return { text: `${bad} 张过期或续签失败`, tone: "bad" };
    const unknown = items.filter((c) => !c.health || c.health === "unknown").length;
    if (unknown) return warn(`${unknown} 张状态未知`);
    const renewing = items.filter((c) => c.health === "renewing").length;
    if (renewing) return plain(`${renewing} 张待续签`);
    return items.length ? { text: `${items.length} 张正常`, tone: "ok" } : plain("还没有证书");
  });
  return <>
    <ValueRow setting="certs" state={certState} onOpen={() => open("certs")} />
    <ValueRow setting="github" state={read(sources, (data) => data.sources?.length ? null : plain("使用默认来源"))} onOpen={() => open("github")} />
  </>;
}

export function AIRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const q = useQuery({ queryKey: ["dsh"], queryFn: () => api("/admin/dsh") as Promise<DshView> });
  const laya = useQuery({ queryKey: ["laya"], queryFn: () => api("/admin/defense/laya") as Promise<{ enabled: boolean; verified?: boolean; mode?: string; gate_id?: number; url: string; model: string; warmup_seconds?: number }> });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function toggleDsh(patch: Partial<DshView["settings"]>) {
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
  async function toggleLaya(on: boolean) {
    if (!laya.data) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/defense/laya", "PUT", { mode: laya.data.mode, gate_id: laya.data.gate_id, enabled: on, url: laya.data.url, model: laya.data.model, warmup_seconds: laya.data.warmup_seconds ?? 300, key: "" });
      await client.invalidateQueries({ queryKey: ["laya"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const settings = q.data?.settings;
  // 开关已说明开没开，行尾只写开了却没跑起来的情况
  const dshState = read(q, (data) => data.settings.enabled && !data.ready
    ? data.status.state === "failed" ? { text: "启动失败", tone: "bad" } : plain("正在启动")
    : !data.settings.version ? plain("未下载") : null);
  const layaState = read(laya, (data) => data.enabled || data.verified ? null : plain("未实测"));
  const dshBlocked = settings && !settings.enabled && !settings.domain ? "先在「dsh 服务与版本」里下载并填写地址" : "";
  const layaBlocked = laya.data && !laya.data.enabled && !laya.data.verified ? "先在「Laya 推理与部署」保存并通过实测" : "";
  return <>
    {q.error ? <Notice tone="bad">读取 dsh 状态失败：{clause(q.error)}</Notice> : null}
    {laya.error ? <Notice tone="bad">读取 Laya 状态失败：{clause(laya.error)}</Notice> : null}
    {error && <Notice tone="bad">{error}</Notice>}
    <SwitchField placement="end" label="启用 AI 助手 dsh" description={dshBlocked || "在控制台里对话、分析事件并生成待确认草稿"} checked={!!settings?.enabled} disabled={busy || !settings || !!dshBlocked} onCheckedChange={(on) => void toggleDsh({ enabled: on })} />
    <ValueRow setting="dsh" state={dshState} onOpen={() => open("dsh")} />
    <SwitchField placement="end" label="启用 Laya 自动初筛" description={layaBlocked || "初筛安全事件与 VPS 运维异常"} checked={!!laya.data?.enabled} disabled={busy || !laya.data || !!layaBlocked} onCheckedChange={(on) => void toggleLaya(on)} />
    <ValueRow setting="laya" state={layaState} onOpen={() => open("laya")} />
    <SwitchField placement="end" label="Laya 判为可疑的事件自动交给 dsh 分析" checked={!!settings?.auto_analyze} disabled={busy || !settings || !settings.enabled} onCheckedChange={(on) => void toggleDsh({ auto_analyze: on })} />
    <ValueRow setting="ai-profile" state={null} onOpen={() => open("ai-profile")} />
  </>;
}

export function DataRows({ open }: { open: Open }) {
  const retention = useQuery({ queryKey: ["retention-policy"], queryFn: () => api("/admin/settings/retention") as Promise<{ policy?: { observation_days?: number } }> });
  const webdav = useQuery({ queryKey: ["webdav"], queryFn: () => api("/admin/backup/webdav") as Promise<{ url?: string; auto?: boolean; last_error?: string }> });
  const tokens = useQuery({ queryKey: ["api-tokens"], queryFn: async () => (await api("/admin/api-tokens")).items as unknown[] });
  const backup = read(webdav, (data) => data.last_error ? { text: "上次自动备份失败", tone: "bad" } : !data.url ? plain("只能手动导出") : !data.auto ? plain("未开启自动备份") : null);
  return <>
    <ValueRow setting="backup" state={backup} onOpen={() => open("backup")} />
    <ValueRow setting="retention" state={read(retention, (data) => data.policy?.observation_days == null ? null : plain(`观测保留 ${data.policy.observation_days} 天`))} onOpen={() => open("retention")} />
    <ValueRow setting="api" state={read(tokens, (items) => items.length ? plain(`${items.length} 个`) : null)} onOpen={() => open("api")} />
  </>;
}

export function UpdateRows({ open }: { open: Open }) {
  const client = useQueryClient();
  const status = useUpdateStatus();
  const notify = useQuery({ queryKey: ["notify-settings"], queryFn: () => api("/admin/settings/notify") as Promise<Record<string, unknown>> });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function toggle(on: boolean) {
    if (!notify.data) return;
    setBusy(true);
    setError("");
    try {
      await api("/admin/settings/notify", "PUT", { ...notify.data, bot_token: "", gate_offline: on });
      await client.invalidateQueries({ queryKey: ["notify-settings"] });
    } catch (e) {
      setError(formError(e, {}).message);
    } finally {
      setBusy(false);
    }
  }
  const version = read(status, (data) => {
    const current = data.current || "版本未知";
    if (data.update_status === "available") return warn(`${current} → ${data.latest?.version ?? "新版本"}`);
    if (data.update_status === "failed") return { text: `${current} · 检查更新失败`, tone: "bad" };
    if (data.update_status === "stale") return warn(`${current} · 检查结果已过期`);
    if (data.update_status === "unavailable") return warn(`${current} · 新发布不可用`);
    return plain(current);
  });
  const data = notify.data;
  const notifyState = read(notify, (d) => !d.enabled ? plain("未开启") : !d.token_set ? warn("缺少 Bot Token") : null);
  return <>
    {error && <Notice tone="bad">{error}</Notice>}
    <ValueRow setting="update" state={version} onOpen={() => open("update")} />
    <ValueRow setting="notify" detail="提醒不含订阅地址或凭据" state={notifyState} onOpen={() => open("notify")} />
    <SwitchField placement="end" label="VPS 离线超过 5 分钟时提醒" description={data && !data.enabled ? "需要先开启 Telegram 通知" : undefined} checked={!!data?.gate_offline} disabled={busy || !data || !data.enabled} onCheckedChange={(on) => void toggle(on)} />
  </>;
}
