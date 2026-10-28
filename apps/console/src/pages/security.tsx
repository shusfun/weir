import { UserSessions } from "../components/user-sessions";
import { useId, useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { QRCodeSVG } from "qrcode.react";
import { KeyRound, Smartphone, LockKeyhole } from "lucide-react";
import {
  Badge,
  CodeBlock,
  IconTile,
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

type Passkey = { id: number; created_at: string };
type Action = "totp" | "passkey" | "password" | "disable" | "delete-key";
type Field = "password" | "code" | "next";
// 登录方式卡片：图标 + 当前状态 + 标题说明 + 操作
function SecurityMethod({
  icon,
  status,
  title,
  description,
  children,
}: {
  icon: ReactNode;
  status: ReactNode;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <Card>
      <CardContent className="grid h-full content-start gap-3">
        <div className="flex items-center justify-between gap-3">
          <IconTile size="md">{icon}</IconTile>
          <Badge variant="soft">{status}</Badge>
        </div>
        <SectionHeader
          level="h3"
          flush
          className="[&_p]:min-h-10 max-md:[&_p]:min-h-0"
          title={title}
          description={description}
        />
        <div className="flex flex-wrap items-center gap-2">{children}</div>
      </CardContent>
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
  return (
    <div className="stack security-page">
      <PageHeader title="保护你的账户" description="管理登录方式与有效会话。设备配对身份与账户二次验证分别管理。" />
      {notice && <Notice role="status">{notice}</Notice>}
      {account.isPending ? (
        <State loading />
      ) : account.error ? (
        <State error={account.error} onRetry={() => account.refetch()} />
      ) : (
        <div className="security-method-grid">
          <SecurityMethod
            icon={<LockKeyhole />}
            status="已设置"
            title="登录密码"
            description="修改后撤销所有旧会话，重新登录。"
          >
            <Button variant="outline" onClick={() => open("password")}>
              修改密码
            </Button>
          </SecurityMethod>
          <SecurityMethod
            icon={<Smartphone />}
            status={account.data?.totp_enrolled ? "已绑定" : "未绑定"}
            title="身份验证器"
            description="使用动态验证码登录，恢复码仅展示一次。"
          >
            <Button
              variant="outline"
              disabled={!account.data?.totp_enrolled && !policy.data?.totp_enabled}
              onClick={() => open(account.data?.totp_enrolled ? "disable" : "totp")}
            >
              {account.data?.totp_enrolled ? "停用 TOTP" : "设置 TOTP"}
            </Button>
            {policy.data && !policy.data.totp_enabled && !account.data?.totp_enrolled && (
              <small>管理员尚未开放此方式</small>
            )}
          </SecurityMethod>
          <SecurityMethod
            icon={<KeyRound />}
            status={keys.isPending ? "加载中" : keys.error ? "未知" : `${keys.data?.items?.length ?? 0} 个`}
            title="Passkey"
            description="使用设备生物识别或安全密钥完成登录。"
          >
            <Button variant="outline" disabled={!policy.data?.passkey_enabled} onClick={() => open("passkey")}>
              绑定 Passkey
            </Button>
            {policy.data && !policy.data.passkey_enabled && <small>管理员尚未开放此方式</small>}
          </SecurityMethod>
        </div>
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
                  <CodeBlock key={value} className="text-center text-[12.5px]">
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
                        hint="至少 6 位，含大写字母、小写字母和数字。"
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
                >
                  {busy
                    ? "正在处理…"
                    : enrollment.url
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
