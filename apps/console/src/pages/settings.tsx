import { ReleaseUpdateCard } from "../components/release-update";
import { ThemeControl } from "../theme";
import {SubscriptionNamingEditor} from "../components/subscription-naming";
import {SubscriptionDefaultsEditor} from "../components/subscription-defaults";
import {AnnouncementEditor} from "../components/global-announcement";
import { ClientCatalogEditor } from "../components/client-catalog";
import { useId, useState, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Button,
  Badge,
  Card,
  CardContent,
  Label,
  Switch,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@migate/ui";
import {
  ShieldCheck,
  Mail,
  Bot,
  Inbox,
  Save,
  RefreshCw,
  ArrowRight,
} from "lucide-react";
import { api } from "../api";
import { Field } from "../components/form-field";
import { State, Status } from "../components/feedback";
import { date, type Resource } from "../lib/models";

type Security = {
  registration_enabled: boolean;
  email_verification: boolean;
  totp_enabled: boolean;
  passkey_enabled: boolean;
  require_admin_mfa: boolean;
};
type SMTP = {
  Host: string;
  Port: number;
  Username: string;
  Password: string;
  From: string;
  FromName: string;
  UseTLS: boolean;
};
type Laya = { url: string; key: string; model: string; enabled: boolean };
type Settings = {
  security: Security;
  smtp: SMTP;
  laya: Laya;
  smtp_password_set: boolean;
  laya_key_set: boolean;
  settings_revision: string;
};
type Draft = {
  security: Security;
  smtp: SMTP;
  laya: Laya;
  clear_smtp_password: boolean;
  clear_laya_key: boolean;
};
const sections = [
  {key:"release",label:"版本发布",icon:RefreshCw,description:"校验 GitHub Release 并推送 Gate"},
  {key:"subscription",label:"订阅默认处理",icon:Inbox,description:"默认处理链与订阅组覆盖"},
  {key:"announcement",label:"全局公告",icon:Inbox,description:"通知内容、发布与显示受众"},
  {key:"clients",label:"客户端目录",icon:Inbox,description:"下载、格式、平台和显示顺序"},
  {
    key: "security",
    label: "账户与安全",
    icon: ShieldCheck,
    description: "注册、验证方式与管理员策略",
  },
  {
    key: "smtp",
    label: "邮件服务",
    icon: Mail,
    description: "SMTP 连接、发件人与测试",
  },
  {
    key: "laya",
    label: "Laya 观察",
    icon: Bot,
    description: "远程模型与旁路分类",
  },
  {
    key: "mail",
    label: "邮件队列",
    icon: Inbox,
    description: "发送结果、失败与重试",
  },
];
const policyFields: [keyof Security, string, string][] = [
  [
    "registration_enabled",
    "开放注册",
    "默认关闭。新用户不会自动获得节点或订阅授权。",
  ],
  [
    "email_verification",
    "要求邮箱验证",
    "注册与邮箱恢复需要已配置的邮件服务。",
  ],
  ["totp_enabled", "允许身份验证器", "账户可绑定 TOTP 并生成一次性恢复码。"],
  [
    "passkey_enabled",
    "允许通行密钥",
    "使用支持 WebAuthn 的设备或安全密钥登录。",
  ],
  [
    "require_admin_mfa",
    "管理员强制二次验证",
    "管理员需完成二次验证；尚未绑定的账号会进入绑定流程。",
  ],
];
const message = (e: unknown) =>
  e instanceof Error ? e.message : "操作失败，请稍后重试。";
function draftOf(v: Settings): Draft {
  return {
    security: { ...v.security },
    smtp: { ...v.smtp, Password: "" },
    laya: { ...v.laya, key: "" },
    clear_smtp_password: false,
    clear_laya_key: false,
  };
}

export function SettingsPage({ loginRequired }: { loginRequired: () => void }) {
  const query = useQuery({
    queryKey: ["system-settings"],
    queryFn: () => api("/admin/settings") as Promise<Settings>,
    refetchOnWindowFocus: false,
  });
  if (query.isPending) return <State loading />;
  if (!query.data)
    return <State error={query.error} onRetry={() => query.refetch()} />;
  return (
    <>
      <div className="page-intro">
        <div>
          <p className="eyebrow">SYSTEM SETTINGS</p>
          <h2>系统设置</h2>
          <p>统一管理账户策略、邮件服务与安全观察。</p>
        </div>
        <ThemeControl />
      </div>
      {query.error && (
        <State error={query.error} onRetry={() => query.refetch()} />
      )}
      <SettingsEditor initial={query.data} loginRequired={loginRequired} />
    </>
  );
}
function SettingsEditor({
  initial,
  loginRequired,
}: {
  initial: Settings;
  loginRequired: () => void;
}) {
  const cache = useQueryClient(),
    id = useId(),
    [params, SP] = useSearchParams();
  const tab = sections.some((s) => s.key === params.get("tab"))
    ? params.get("tab")!
    : "security";
  const [d, D] = useState<Draft>(() => draftOf(initial)),
    [busy, B] = useState(false),
    [notice, N] = useState(""),
    [error, E] = useState("");
  const [confirm, CF] = useState(false),
    [reload, RL] = useState(false),
    [password, P] = useState(""),
    [code, C] = useState(""),
    [recipient, R] = useState("");
  const self = useQuery({
    queryKey: ["self-account"],
    queryFn: () => api("/me"),
  });
  useEffect(() => {
    D(draftOf(initial));
  }, [initial.settings_revision]);
  const dirty = JSON.stringify(d) !== JSON.stringify(draftOf(initial)),
    securityDirty =
      JSON.stringify(d.security) !== JSON.stringify(initial.security);
  const smtpDirty =
    JSON.stringify(d.smtp) !==
      JSON.stringify({ ...initial.smtp, Password: "" }) ||
    d.clear_smtp_password;
  const layaDirty =
    JSON.stringify(d.laya) !== JSON.stringify({ ...initial.laya, key: "" }) ||
    d.clear_laya_key;
  function change(next: Draft) {
    D(next);
    E("");
    N("");
  }
  function smtp<K extends keyof SMTP>(key: K, value: SMTP[K]) {
    change({ ...d, smtp: { ...d.smtp, [key]: value } });
  }
  function laya<K extends keyof Laya>(key: K, value: Laya[K]) {
    change({ ...d, laya: { ...d.laya, [key]: value } });
  }
  async function test(which: "smtp" | "laya") {
    B(true);
    E("");
    N("");
    try {
      await api(
        which === "smtp" ? "/admin/smtp/test" : "/admin/laya/test",
        "POST",
        which === "smtp" ? { to: recipient } : undefined,
      );
      N(
        which === "smtp"
          ? "测试邮件已入队，请到邮件队列查看发送结果。"
          : "Laya 协议测试成功。模型仍仅用于旁路观察。",
      );
      if (which === "smtp")
        await cache.invalidateQueries({ queryKey: ["settings-mail"] });
    } catch (e) {
      E(message(e));
    } finally {
      B(false);
    }
  }
  return (
    <div className="settings-workspace">
      <nav className="settings-sections" aria-label="设置分组">
        {sections.map((s) => (
          <Button
            key={s.key}
            variant={tab === s.key ? "secondary" : "ghost"}
            aria-current={tab === s.key ? "page" : undefined}
            onClick={() => {
              SP({ tab: s.key });
              E("");
              N("");
            }}
          >
            <s.icon size={18} />
            <span>
              <strong>{s.label}</strong>
              <small>{s.description}</small>
            </span>
            <ArrowRight size={14} />
          </Button>
        ))}
      </nav>
      <div className="settings-body stack">
        {notice && (
          <p className="notice" role="status">
            {notice}
          </p>
        )}
        {error && (
          <p className="notice error-message" role="alert">
            {error}
          </p>
        )}
        {tab === "release" && <ReleaseUpdateCard />}
        {tab === "security" && (
          <Card>
            <CardContent className="stack">
              <div className="section-heading">
                <div>
                  <h3>账户与安全策略</h3>
                  <p>策略变更后所有现有会话撤销，需要重新登录。</p>
                </div>
                <ShieldCheck size={22} />
              </div>
              {policyFields.map(([key, label, description]) => (
                <Label key={key} className="settings-policy">
                  <span>
                    <strong>{label}</strong>
                    <small>{description}</small>
                  </span>
                  <Switch
                    checked={!!d.security[key]}
                    onCheckedChange={(value) =>
                      change({
                        ...d,
                        security: { ...d.security, [key]: value },
                      })
                    }
                    aria-label={label}
                  />
                </Label>
              ))}
            </CardContent>
          </Card>
        )}
        {tab === "smtp" && (
          <>
            <Card>
              <CardContent className="stack">
                <div className="section-heading">
                  <div>
                    <h3>SMTP 邮件服务</h3>
                    <p>
                      验证邮件和恢复邮件通过已保存配置发送。空服务器表示停用。
                    </p>
                  </div>
                  <Badge variant="outline">
                    {initial.smtp.Host ? "已配置" : "未配置"}
                  </Badge>
                </div>
                <div className="settings-field-grid">
                  <Field
                    label="SMTP 服务器"
                    name={id + "host"}
                    value={d.smtp.Host}
                    onChange={(v) => smtp("Host", v)}
                    required={false}
                  />
                  <Field
                    label="SMTP 端口"
                    name={id + "port"}
                    value={d.smtp.Port || ""}
                    onChange={(v) => smtp("Port", Number(v))}
                    type="number"
                    required={false}
                  />
                  <Field
                    label="SMTP 账号"
                    name={id + "username"}
                    value={d.smtp.Username}
                    onChange={(v) => smtp("Username", v)}
                    required={false}
                  />
                  {!d.clear_smtp_password && (
                    <Field
                      label={
                        initial.smtp_password_set
                          ? "SMTP 密码（留空保留）"
                          : "SMTP 密码"
                      }
                      name={id + "smtp-password"}
                      value={d.smtp.Password}
                      onChange={(v) => smtp("Password", v)}
                      type="password"
                      required={false}
                    />
                  )}
                  <Field
                    label="发件邮箱"
                    name={id + "from"}
                    value={d.smtp.From}
                    onChange={(v) => smtp("From", v)}
                    type="email"
                    required={false}
                  />
                  <Field
                    label="发件名称"
                    name={id + "from-name"}
                    value={d.smtp.FromName}
                    onChange={(v) => smtp("FromName", v)}
                    required={false}
                  />
                </div>
                <Label className="settings-policy">
                  <span>
                    <strong>隐式 TLS</strong>
                    <small>
                      通常使用 465；关闭后要求 STARTTLS，通常使用
                      587。始终验证服务器证书。
                    </small>
                  </span>
                  <Switch
                    checked={!!d.smtp.UseTLS}
                    onCheckedChange={(v) => smtp("UseTLS", v)}
                    aria-label="SMTP 隐式 TLS"
                  />
                </Label>
                {initial.smtp_password_set && (
                  <div className="actions">
                    <Button
                      variant="ghost"
                      onClick={() =>
                        change({
                          ...d,
                          smtp: { ...d.smtp, Password: "" },
                          clear_smtp_password: !d.clear_smtp_password,
                        })
                      }
                    >
                      {d.clear_smtp_password
                        ? "保留已保存的 SMTP 密码"
                        : "清除已保存的 SMTP 密码"}
                    </Button>
                    {d.clear_smtp_password && (
                      <small>保存时清除；使用邮件服务前需重新填写密码。</small>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardContent className="stack">
                <div>
                  <h3>发送测试邮件</h3>
                  <p>
                    使用服务器上已保存的配置；入队成功后还需检查实际发送结果。
                  </p>
                </div>
                <form
                  className="settings-test"
                  onSubmit={(e) => {
                    e.preventDefault();
                    void test("smtp");
                  }}
                >
                  <Field
                    label="测试收件邮箱"
                    name={id + "recipient"}
                    value={recipient}
                    onChange={R}
                    type="email"
                  />
                  <Button
                    type="submit"
                    variant="outline"
                    disabled={busy || smtpDirty || !initial.smtp.Host}
                  >
                    发送测试邮件
                  </Button>
                </form>
                {smtpDirty ? (
                  <small>请先保存邮件配置，再发送测试邮件。</small>
                ) : (
                  !initial.smtp.Host && (
                    <small>配置 SMTP 后可以发送测试邮件。</small>
                  )
                )}
              </CardContent>
            </Card>
          </>
        )}
        {tab === "laya" && (
          <Card>
            <CardContent className="stack">
              <div className="section-heading">
                <div>
                  <h3>Laya 远程观察</h3>
                  <p>仅将脱敏事件发送到远程服务，用于分类与告警排序。</p>
                </div>
                <Badge variant="outline">
                  {initial.laya.enabled ? "已启用" : "已关闭"}
                </Badge>
              </div>
              <Field
                label="Laya 服务地址"
                name={id + "model-url"}
                value={d.laya.url}
                onChange={(v) => laya("url", v)}
                type="url"
                required={false}
              />
              <small>
                HTTPS 基础地址或原生 /v1/systemone 接口。密钥只在中心使用。
              </small>
              <div className="settings-field-grid">
                <Field
                  label="模型名称"
                  name={id + "model-name"}
                  value={d.laya.model}
                  onChange={(v) => laya("model", v)}
                  required={false}
                />
                {!d.clear_laya_key && (
                  <Field
                    label={
                      initial.laya_key_set
                        ? "Laya API Key（留空保留）"
                        : "Laya API Key"
                    }
                    name={id + "model-key"}
                    value={d.laya.key}
                    onChange={(v) => laya("key", v)}
                    type="password"
                    required={false}
                  />
                )}
              </div>
              <Label className="settings-policy">
                <span>
                  <strong>启用远程旁路观察</strong>
                  <small>
                    模型不能授权、封禁或执行设备操作；调用失败时基础规则继续运行。
                  </small>
                </span>
                <Switch
                  checked={!!d.laya.enabled}
                  onCheckedChange={(v) => laya("enabled", v)}
                  aria-label="启用 Laya 远程观察"
                />
              </Label>
              {initial.laya_key_set && (
                <Button
                  variant="ghost"
                  onClick={() =>
                    change({
                      ...d,
                      laya: { ...d.laya, key: "" },
                      clear_laya_key: !d.clear_laya_key,
                    })
                  }
                >
                  {d.clear_laya_key
                    ? "保留已保存的 Laya 密钥"
                    : "清除已保存的 Laya 密钥"}
                </Button>
              )}
              <div className="actions">
                <Button
                  variant="outline"
                  disabled={busy || layaDirty || !initial.laya.enabled}
                  onClick={() => test("laya")}
                >
                  测试 Laya 已保存配置
                </Button>
                <small>
                  {layaDirty
                    ? "请先保存配置，再测试连接。"
                    : "测试只检查原生协议，不代表分类效果已经评估。"}
                </small>
              </div>
            </CardContent>
          </Card>
        )}
        {tab === "subscription" ? <div className="stack"><SubscriptionDefaultsEditor/><SubscriptionNamingEditor/></div> : tab === "announcement" ? <AnnouncementEditor /> : tab === "clients" ? <ClientCatalogEditor /> : tab === "mail" ? (
          <MailQueue />
        ) : (
          <div className="settings-save">
            <span>{dirty ? "有未保存的更改" : "配置已与服务器同步"}</span>
            <div className="actions">
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => RL(true)}
              >
                <RefreshCw size={15} />
                重新读取配置
              </Button>
              <Button
                disabled={busy || !dirty}
                onClick={() => {
                  P("");
                  C("");
                  E("");
                  CF(true);
                }}
              >
                <Save size={15} />
                保存设置
              </Button>
            </div>
          </div>
        )}
      </div>
      <Dialog open={confirm} onOpenChange={(v) => !busy && CF(v)}>
        <DialogContent className="business-dialog">
          <DialogTitle>确认保存系统设置</DialogTitle>
          <DialogDescription>
            {securityDirty
              ? "账户策略已变更，保存后会撤销所有当前会话并要求重新登录。"
              : "验证管理员身份后保存。密钥留空会保留原值。"}{" "}
            {d.clear_smtp_password || d.clear_laya_key
              ? "本次包含清除已保存密钥。"
              : ""}
          </DialogDescription>
          <form
            className="stack"
            onSubmit={async (e) => {
              e.preventDefault();
              B(true);
              E("");
              try {
                const result = await api("/admin/settings", "PUT", {
                  ...d,
                  settings_revision: initial.settings_revision,
                  password,
                  code,
                });
                P("");
                C("");
                CF(false);
                if (result.login_required) {
                  loginRequired();
                  return;
                }
                N("系统设置已保存。");
                await cache.invalidateQueries({
                  queryKey: ["system-settings"],
                });
              } catch (e) {
                E(message(e));
                C("");
              } finally {
                B(false);
              }
            }}
          >
            <Field
              label="当前管理员密码"
              name={id + "password"}
              value={password}
              onChange={P}
              type="password"
            />
            {self.data?.totp_enrolled && (
              <Field
                label="二次验证码 / 恢复码"
                name={id + "code"}
                value={code}
                onChange={C}
              />
            )}
            <p>若其他管理员已经修改设置，本次会拒绝覆盖；请重新读取并核对。</p>
            {error && (
              <p role="alert" className="error-message">
                {error}
              </p>
            )}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => CF(false)}
              >
                取消
              </Button>
              <Button type="submit" disabled={busy}>
                {busy ? "保存中…" : "确认保存设置"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={reload} onOpenChange={RL}>
        <DialogContent>
          <DialogTitle>重新读取服务器配置</DialogTitle>
          <DialogDescription>
            丢弃本页未保存的更改和输入的密钥，重新读取最新配置。
          </DialogDescription>
          <DialogFooter>
            <Button variant="outline" onClick={() => RL(false)}>
              取消
            </Button>
            <Button
              onClick={async () => {
                RL(false);
                D(draftOf(initial));
                E("");
                N("");
                await cache.invalidateQueries({
                  queryKey: ["system-settings"],
                });
              }}
            >
              丢弃更改并重新读取
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function MailQueue() {
  const [page, P] = useState(1),
    [busy, B] = useState(false),
    [error, E] = useState(""),
    cache = useQueryClient();
  const query = useQuery({
    queryKey: ["settings-mail", page],
    queryFn: () =>
      api(`/admin/resources/mail?page=${page}&page_size=20`) as Promise<{
        items: Resource[];
        total: number;
      }>,
    refetchInterval: 15000,
  });
  return (
    <Card>
      <CardContent className="stack">
        <div className="section-heading">
          <div>
            <h3>邮件发送队列</h3>
            <p>
              入队、发送成功和失败分别记录。只展示发送状态，邮件正文与恢复凭据不回传。
            </p>
          </div>
          <Button variant="outline" onClick={() => query.refetch()}>
            <RefreshCw size={14} />
            刷新队列
          </Button>
        </div>
        {error && (
          <p role="alert" className="error-message">
            {error}
          </p>
        )}
        {query.isPending ? (
          <State loading />
        ) : query.error ? (
          <State error={query.error} onRetry={() => query.refetch()} />
        ) : !query.data.items.length ? (
          <State empty="暂无发送记录" />
        ) : (
          <>
            <div className="table-scroll">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>收件人</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>尝试</TableHead>
                    <TableHead>结果</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell>
                        <strong>{String(item.data.to)}</strong>
                        <small className="block">
                          {date(
                            (item as Resource & { created_at?: string })
                              .created_at,
                          )}
                        </small>
                      </TableCell>
                      <TableCell>
                        <Status value={String(item.data.status)} />
                      </TableCell>
                      <TableCell>
                        {String(item.data.attempts ?? 0)} / 5
                      </TableCell>
                      <TableCell>
                        {item.data.last_error
                          ? "发送失败，请检查邮件服务配置"
                          : item.data.status === "sent"
                            ? "服务已接受邮件"
                            : item.data.status === "pending"
                              ? "等待发送"
                              : "未知"}
                      </TableCell>
                      <TableCell>
                        {item.data.status === "failed" && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy || Number(item.data.attempts) >= 5}
                            onClick={async () => {
                              B(true);
                              E("");
                              try {
                                await api(
                                  `/admin/mail/${item.id}/retry`,
                                  "POST",
                                );
                                await cache.invalidateQueries({
                                  queryKey: ["settings-mail"],
                                });
                              } catch (e) {
                                E(message(e));
                              } finally {
                                B(false);
                              }
                            }}
                          >
                            重新入队
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <div className="pagination">
              <span>
                共 {query.data.total} 项 · 第 {page} 页
              </span>
              <div className="actions">
                <Button
                  variant="outline"
                  disabled={page === 1}
                  onClick={() => P((v) => v - 1)}
                >
                  上一页邮件
                </Button>
                <Button
                  variant="outline"
                  disabled={page * 20 >= query.data.total}
                  onClick={() => P((v) => v + 1)}
                >
                  下一页邮件
                </Button>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
