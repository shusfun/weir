import { UserSessions } from "../components/user-sessions";
import { useId, useMemo, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { QRCodeSVG } from "qrcode.react";
import {
  Badge,
  CodeBlock,
  InsightBanner,
  Notice,
  PageHeader,
  PageState,
  SectionHeader,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Form,
} from "@weir/ui";
import { api, fieldErrors, formError, reauthFields, type FormError } from "../api";
import { accountPasswordError } from "../password";
import { passkey } from "../lib/passkey";
import { date, type Account, type Items } from "../lib/models";
import { TextField } from "../components/form-field";
import { State } from "../components/feedback";
import { field, useConsoleContext } from "../lib/console-context";

type Passkey = { id: number; created_at: string };
type Action = "totp" | "passkey" | "password" | "disable" | "delete-key";
type Field = "password" | "code" | "next";
// 登录方式卡片：图标 + 当前状态 + 标题说明 + 操作
function SecurityMethod({
  status,
  title,
  description,
  children,
}: {
  status: ReactNode;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <Card className="grid content-start gap-[13px] px-[21px] py-[21px]" aria-label={title}>
      <div className="flex items-center justify-between gap-3">
        <b className="text-subheadline">{title}</b>
        {status}
      </div>
      <p className="m-0 text-meta text-[var(--weir-ink-2)]">{description}</p>
      <div className="flex flex-wrap items-center gap-2">{children}</div>
    </Card>
  );
}

export function SecurityPage({ loginRequired }: { loginRequired: () => void }) {
  const id = useId(),
    cache = useQueryClient();
  const account = useQuery({ queryKey: ["self-account"], queryFn: () => api("/me") as Promise<Account> });
  const policy = useQuery({
    queryKey: ["public-policy"],
    queryFn: () => api("/public") as Promise<{ totp_enabled: boolean; passkey_enabled: boolean }>,
  });

  const keys = useQuery({
    queryKey: ["self-passkeys"],
    queryFn: () => api("/security/passkeys") as Promise<Items<Passkey>>,
  });
  const [action, setAction] = useState<Action | null>(null),
    [password, setPassword] = useState(""),
    [code, setCode] = useState(""),
    [next, setNext] = useState(""),
    [repeat, setRepeat] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<FormError<Field> | null>(null),
    [notice, setNotice] = useState(""),
    [keyId, setKeyId] = useState<number>(),
    [relogin, setRelogin] = useState("");
  const [enrollment, setEnrollment] = useState<{ secret?: string; url?: string; recovery?: string[] }>({});
  const errors = useMemo(() => fieldErrors(error), [error]);
  const titles: Record<Action, string> = {
    totp: "绑定身份验证器",
    passkey: "绑定 Passkey",
    password: "修改登录密码",
    disable: "停用身份验证器",
    "delete-key": "删除 Passkey",
  };
  function clearInputs() {
    setPassword("");
    setCode("");
    setNext("");
    setRepeat("");
  }
  function open(v: Action) {
    clearInputs();
    setError(null);
    setNotice("");
    setEnrollment({});
    setAction(v);
  }
  function close() {
    if (busy || enrollment.recovery) return;
    setAction(null);
    clearInputs();
    setEnrollment({});
    setError(null);
  }
  // 完成后关闭弹窗；需要重新登录的操作给出原因
  function finish(reloginReason?: string) {
    setAction(null);
    clearInputs();
    if (reloginReason) setRelogin(reloginReason);
  }
  async function submit() {
    setBusy(true);
    setError(null);
    try {
      if (action === "totp") {
        if (enrollment.url) {
          const result = await api("/security/totp/confirm", "POST", { code });
          setEnrollment({ recovery: result.recovery_codes });
          clearInputs();
        } else {
          setEnrollment(await api("/security/totp/setup", "POST", { password, code }));
          setCode("");
        }
      } else if (action === "passkey") {
        await passkey("register", undefined, password, code);
        await cache.invalidateQueries({ queryKey: ["self-passkeys"] });
        finish();
        setNotice("Passkey 已绑定");
      } else if (action === "password") {
        await api("/security/password", "POST", { password, code, new_password: next });
        finish("密码已更新，所有旧会话已撤销。");
      } else if (action === "disable") {
        await api("/security/totp/disable", "POST", { password, code });
        finish("身份验证器已停用，原恢复码和旧会话已失效。");
      } else if (action === "delete-key") {
        await api("/security/passkeys/" + keyId, "DELETE", { password, code });
        finish("Passkey 已删除，所有旧会话已撤销。");
      }
    } catch (e) {
      setError(formError(e, { ...reauthFields, weak_password: "next", invalid_password: "next" }));
    } finally {
      setBusy(false);
    }
  }
  useConsoleContext({ page: "账户安全", dialog: action ? { title: titles[action] } : undefined, form: action ? [field("password", "当前密码", password, true), field("code", "验证码", code, true), field("next", "新密码", next, true)] : undefined });
  return (
    <div className="stack security-page">
      <PageHeader size="data" title="账户安全" description={account.data ? <span className="weir-sum"><span>通行密钥 <b>{keys.isPending || keys.error ? "未知" : (keys.data?.items.length ?? 0) > 0 ? "已启用" : "未绑定"}</b></span>{account.data.totp_enrolled ? <span>两步验证 <b>已启用</b></span> : <span className="text-[var(--weir-warn-ink)]">两步验证未开启</span>}</span> : "管理登录方式与有效会话。"} />
      {notice && <Notice role="status">{notice}</Notice>}
      {account.isPending ? (
        <State loading />
      ) : account.error ? (
        <State error={account.error} onRetry={() => account.refetch()} />
      ) : (
        <>
        {!account.data.totp_enrolled && (
          <InsightBanner icon="!" laya={false} title="开启两步验证" progress="密码泄露了，别人也登不进来" action={<Button disabled={!policy.data?.totp_enabled} onClick={() => open("totp")}>开启</Button>} />
        )}
        <div className="security-method-grid">
          <SecurityMethod
            status={<span className="text-meta text-[var(--weir-ink-3)]">已设置</span>}
            title="登录密码"
            description="建议和通行密钥一起使用；修改后其他设备需要重新登录"
          >
            <Button size="sm" variant="ghost" onClick={() => open("password")}>
              修改密码
            </Button>
          </SecurityMethod>
          <SecurityMethod
            status={account.data?.totp_enrolled ? <Badge variant="soft" tone="ok">已开启</Badge> : <Badge variant="soft" tone="warn">未开启</Badge>}
            title="两步验证"
            description="验证器 App 的 6 位动态码"
          >
            <Button
              size="sm"
              variant="ghost"
              className="px-0"
              disabled={!account.data?.totp_enrolled && !policy.data?.totp_enabled}
              onClick={() => open(account.data?.totp_enrolled ? "disable" : "totp")}
            >
              {account.data?.totp_enrolled ? "停用" : "开启"}
            </Button>
            {policy.data && !policy.data.totp_enabled && !account.data?.totp_enrolled && (
              <small>管理员尚未开放此方式</small>
            )}
          </SecurityMethod>
          <SecurityMethod
            status={keys.isPending || keys.error ? <span className="text-meta text-[var(--weir-ink-3)]">未知</span> : (keys.data?.items?.length ?? 0) > 0 ? <Badge variant="soft" tone="ok">已启用</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">未绑定</span>}
            title="通行密钥"
            description="用设备的指纹、面容或安全密钥登录"
          >
            <Button size="sm" variant="ghost" disabled={!policy.data?.passkey_enabled} onClick={() => open("passkey")}>
              {(keys.data?.items?.length ?? 0) > 0 ? "再添加一个" : "添加通行密钥"}
            </Button>
            {policy.data && !policy.data.passkey_enabled && <small>管理员尚未开放此方式</small>}
          </SecurityMethod>
        </div>
        </>
      )}
      {policy.error && <State error={policy.error} onRetry={() => policy.refetch()} />}
      <Card>
        <CardContent>
          <SectionHeader title="已绑定的 Passkey" description="删除需要验证身份，并撤销全部会话。" />
          {keys.isPending ? (
            <State loading />
          ) : keys.error ? (
            <State error={keys.error} onRetry={() => keys.refetch()} />
          ) : !keys.data?.items?.length ? (
            <PageState size="sm" kind="empty" title="尚未绑定 Passkey，可添加此设备或安全密钥。" />
          ) : (
            keys.data.items.map((key, i) => (
              <div className="list-row" key={key.id}>
                <div>
                  <strong>Passkey {i + 1}</strong>
                  <small>绑定于 {date(key.created_at)}</small>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => {
                    setKeyId(key.id);
                    open("delete-key");
                  }}
                >
                  删除 Passkey {i + 1}
                </Button>
              </div>
            ))
          )}
        </CardContent>
      </Card>
      <UserSessions onCurrentRevoked={() => setRelogin("当前会话已撤销，请重新登录。")} />
      <Dialog open={!!action} onOpenChange={(v) => !v && close()}>
        <DialogContent size="md">
          <DialogTitle>{action ? titles[action] : "账户安全"}</DialogTitle>
          <DialogDescription>
            {enrollment.recovery
              ? "请离线保存恢复码。关闭本页后无法再次查看。"
              : "操作仅影响你的账户。敏感变更需验证当前密码与已绑定的二次验证。"}
          </DialogDescription>
          {enrollment.recovery ? (
            <div className="stack">
              <div className="recovery-codes">
                {enrollment.recovery.map((value) => (
                  <CodeBlock key={value} className="text-center text-meta">
                    {value}
                  </CodeBlock>
                ))}
              </div>
              <p>每个恢复码只能使用一次；不要发送给其他人。</p>
              <Button
                onClick={() => {
                  setEnrollment({});
                  setAction(null);
                  loginRequired();
                }}
              >
                已保存恢复码，重新登录
              </Button>
            </div>
          ) : (
            <Form busy={busy} errors={errors} onSubmit={() => void submit()}>
              {enrollment.url ? (
                <>
                  <div className="totp-setup">
                    <QRCodeSVG value={enrollment.url} size={180} />
                    <div>
                      <strong>扫描二维码</strong>
                      <p>在身份验证器中添加账户，或手动输入密钥。</p>
                      <CodeBlock>{enrollment.secret}</CodeBlock>
                    </div>
                  </div>
                  <TextField
                    name={id + "confirm-code"}
                    label="身份验证器动态码"
                    required
                    value={code}
                    onChange={setCode}
                    autoComplete="one-time-code"
                  />
                </>
              ) : (
                <>
                  <TextField
                    name={id + "password"}
                    field="password"
                    label="当前密码"
                    type="password"
                    required
                    value={password}
                    onChange={setPassword}
                    autoComplete="current-password"
                  />
                  {account.data?.totp_enrolled && (
                    <TextField
                      name={id + "code"}
                      field="code"
                      label="二次验证码 / 恢复码"
                      required
                      value={code}
                      onChange={setCode}
                      autoComplete="one-time-code"
                    />
                  )}
                  {action === "password" && (
                    <>
                      <TextField
                        name={id + "new"}
                        field="next"
                        label="新密码"
                        hint="至少 6 个字符。"
                        type="password"
                        required
                        customError={next ? accountPasswordError(next) : ""}
                        value={next}
                        onChange={setNext}
                      />
                      <TextField
                        name={id + "repeat"}
                        label="再次输入新密码"
                        type="password"
                        required
                        customError={repeat && repeat !== next ? "两次输入的新密码不一致" : ""}
                        value={repeat}
                        onChange={setRepeat}
                      />
                    </>
                  )}
                </>
              )}
              {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
              <DialogFooter>
                <Button type="button" variant="outline" disabled={busy} onClick={close}>
                  取消
                </Button>
                <Button
                  type="submit"
                  variant={action === "delete-key" || action === "disable" ? "destructive" : "default"}
                  disabled={busy}
                  busy={busy}
                >
                  {enrollment.url
                      ? "确认 TOTP"
                      : action === "totp"
                        ? "生成绑定二维码"
                        : action === "passkey"
                          ? "继续绑定 Passkey"
                          : action === "password"
                            ? "确认修改密码"
                            : action === "disable"
                              ? "确认停用 TOTP"
                              : "确认删除 Passkey"}
                </Button>
              </DialogFooter>
            </Form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={!!relogin}>
        <DialogContent>
          <DialogTitle>请重新登录</DialogTitle>
          <DialogDescription>{relogin}</DialogDescription>
          <Button
            onClick={() => {
              cache.clear();
              loginRequired();
            }}
          >
            返回登录
          </Button>
        </DialogContent>
      </Dialog>
    </div>
  );
}
