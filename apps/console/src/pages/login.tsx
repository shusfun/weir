import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  Fingerprint,
  ShieldCheck,
} from "lucide-react";
import { IconTile, Field, Form, Notice, Button, Card, CardContent, Input, PasswordInput } from "@weir/ui";
import { api, fieldErrors, formError, type FormError } from "../api";
import { accountPasswordError } from "../password";
import { passkey } from "../lib/passkey";

type Policy = {
  registration_enabled: boolean;
  email_verification: boolean;
  passkey_enabled: boolean;
};
type Mode = "login" | "reset" | "register" | "verify";
const titles: Record<Mode, string> = {
  login: "欢迎回来",
  reset: "恢复账户",
  register: "创建账户",
  verify: "验证邮箱",
};
const hints: Record<Mode, string> = {
  login: "登录你的控制台，查看连接与账户。",
  reset: "通过邮箱验证码设置新密码。原有会话会被撤销。",
  register: "创建账户后，由管理员分配节点与订阅权限。",
  verify: "输入发送到此邮箱的验证码，完成账户验证。",
};
// 服务端错误码 → 字段；其余显示在表单底部
const loginFields = {
  invalid_email: "email",
  email_taken: "email",
  email_verification_required: "email",
  weak_password: "password",
  invalid_password: "password",
  invalid_credentials: "password",
  invalid_verification_code: "code",
  invalid_email_code: "code",
  invalid_code: "code",
  invalid_second_factor: "code",
  second_factor_required: "code",
} as const;
type LoginField = (typeof loginFields)[keyof typeof loginFields];

export function LoginPage({ done }: { done: () => void }) {
  const id = useId(),
    emailInput = useRef<HTMLInputElement>(null);
  const [params, setParams] = useSearchParams();
  const raw = params.get("auth");
  const mode: Mode =
    raw === "reset" || raw === "register" || raw === "verify" ? raw : "login";
  const policy = useQuery({
    queryKey: ["auth-policy"],
    queryFn: () => api("/public") as Promise<Policy>,
    retry: false,
  });
  const [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [confirmation, setConfirmation] = useState("");
  const [challenge, setChallenge] = useState(""),
    [code, setCode] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<FormError<LoginField> | null>(null),
    [notice, setNotice] = useState("");
  const errors = useMemo(() => fieldErrors(error), [error]);
  const fail = (e: unknown) => setError(formError(e, loginFields));
  const [cooldown, setCooldown] = useState(0);
  useEffect(() => {
    if (!cooldown) return;
    const timer = window.setTimeout(
      () => setCooldown((v) => Math.max(0, v - 1)),
      1000,
    );
    return () => clearTimeout(timer);
  }, [cooldown]);
  useEffect(() => {
    setChallenge("");
    setCode("");
    setPassword("");
    setConfirmation("");
    setError(null);
  }, [mode]);
  const unavailable =
    (mode === "register" && !policy.data?.registration_enabled) ||
    (mode === "verify" && !policy.data?.email_verification);
  const showCode =
    !!challenge ||
    mode === "reset" ||
    mode === "verify" ||
    (mode === "register" && policy.data?.email_verification);
  function switchMode(next: Mode) {
    if (busy) return;
    setChallenge("");
    setCode("");
    setError(null);
    setNotice("");
    setParams(next === "login" ? {} : { auth: next }, { replace: true });
  }
  function complete() {
    setPassword("");
    setCode("");
    setParams({}, { replace: true });
    done();
  }
  // 密码强度与两次一致由 Field 的 customError 在提交前拦截
  async function submit() {
    if (unavailable) return;
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      if (challenge) {
        await api("/auth/mfa", "POST", { challenge, code });
        complete();
        return;
      }
      if (mode === "login") {
        const response: { mfa_required?: boolean; challenge?: string } =
          await api("/auth/login", "POST", { email, password });
        setPassword("");
        if (response.mfa_required && response.challenge) {
          setChallenge(response.challenge);
          setCode("");
        } else complete();
        return;
      }
      const endpoint =
        mode === "reset"
          ? "/auth/reset"
          : mode === "verify"
            ? "/auth/verify"
            : "/auth/register";
      await api(endpoint, "POST", { email, password, code });
      switchMode("login");
      setPassword("");
      setConfirmation("");
      setCode("");
      setParams({}, { replace: true });
      setNotice(
        mode === "reset"
          ? "密码已重设，请使用新密码登录。"
          : mode === "verify"
            ? "邮箱已验证，请登录。"
            : "账户创建成功，请登录。节点权限由管理员分配。",
      );
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  async function requestCode() {
    if (!emailInput.current?.reportValidity() || busy || cooldown) return;
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      await api("/auth/email", "POST", {
        email,
        purpose: mode === "reset" ? "reset" : "verify",
      });
      setCooldown(60);
      setNotice(
        "若此邮箱符合条件，验证码将发送到邮箱；请检查收件箱与垃圾邮件。验证码 10 分钟有效。",
      );
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-screen">
      <div className="auth-container">
        <a href="/" className="auth-brand" aria-label="Weir 首页">
          <span>
            <Activity size={26} />
          </span>
          <strong>Weir</strong>
        </a>
        <p className="auth-subtitle">独立节点，统一管理。</p>
        <Card className="auth-card" size="lg">
          <CardContent>
            <header className="auth-heading">
              <IconTile size="lg" className="mb-3.5">
                {challenge ? <ShieldCheck /> : <Fingerprint />}
              </IconTile>
              <h1>{challenge ? "验证你的身份" : titles[mode]}</h1>
              <p>
                {challenge
                  ? "输入身份验证器中的六位验证码，或使用一枚未使用的恢复码。"
                  : hints[mode]}
              </p>
            </header>
            {policy.isPending && (
              <p className="auth-hint" role="status">
                正在读取登录方式…
              </p>
            )}
            {policy.isError && (
              <Notice tone="bad">
                登录方式暂时无法读取。
                <Button
                  type="button"
                  variant="link"
                  onClick={() => policy.refetch()}
                >
                  重新读取
                </Button>
              </Notice>
            )}
            {unavailable && !policy.isPending ? (
              <div className="stack">
                <Notice tone="warn" role="status">
                  {policy.isError
                    ? "当前账户策略未知，请重新读取后再继续。"
                    : mode === "register"
                      ? "注册暂未开放，请联系管理员创建账户。"
                      : "当前未启用邮箱验证。"}
                </Notice>
              </div>
            ) : (
              <Form busy={busy} errors={errors} onSubmit={() => void submit()} className="auth-form">
                {!challenge && (
                  <Field label="邮箱" htmlFor={id + "email"} name="email">
                    <Input
                      ref={emailInput}
                      id={id + "email"}
                      type="email"
                      autoComplete="username"
                      required
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      placeholder="you@example.com"
                    />
                  </Field>
                )}
                {!challenge && mode !== "verify" && (
                  <Field
                    label={mode === "reset" ? "新密码" : "密码"}
                    htmlFor={id + "password"}
                    name="password"
                    hint={mode !== "login" ? "至少 6 位，须包含大写字母、小写字母和数字。" : undefined}
                    customError={mode !== "login" && password ? accountPasswordError(password) : ""}
                  >
                    <PasswordInput
                      id={id + "password"}
                      autoComplete={
                        mode === "login" ? "current-password" : "new-password"
                      }
                      required
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                    />
                  </Field>
                )}
                {!challenge && (mode === "reset" || mode === "register") && (
                  <Field
                    label="确认密码"
                    htmlFor={id + "confirm"}
                    customError={confirmation && confirmation !== password ? "两次输入的密码不一致" : ""}
                  >
                    <PasswordInput
                      id={id + "confirm"}
                      autoComplete="new-password"
                      required
                      value={confirmation}
                      onChange={(e) => setConfirmation(e.target.value)}
                    />
                  </Field>
                )}
                {showCode && (
                  <Field label={challenge ? "验证码 / 恢复码" : "邮箱验证码"} htmlFor={id + "code"} name="code">
                    <Input
                      id={id + "code"}
                      autoComplete="one-time-code"
                      autoFocus={!!challenge}
                      required
                      value={code}
                      onChange={(e) => setCode(e.target.value.trim())}
                    />
                    {!challenge && (
                      <Button
                        type="button"
                        variant="outline"
                        disabled={cooldown > 0}
                        onClick={requestCode}
                      >
                        {cooldown ? `${cooldown} 秒后重新发送` : "发送验证码"}
                      </Button>
                    )}
                  </Field>
                )}
                {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
                {notice && (
                  <Notice tone="ok" role="status">
                    {notice}
                  </Notice>
                )}
                <Button
                  type="submit"
                  disabled={policy.isPending || !!unavailable}
                >
                  {busy
                    ? "正在验证…"
                    : challenge
                      ? "验证并登录"
                      : mode === "login"
                        ? "登录"
                        : mode === "reset"
                          ? "重设密码"
                          : mode === "verify"
                            ? "验证邮箱"
                            : "创建账户"}
                  <ArrowRight size={16} />
                </Button>
                {policy.data?.passkey_enabled &&
                  mode === "login" &&
                  !challenge && (
                    <>
                      <div className="auth-divider">
                        <span>或</span>
                      </div>
                      <Button
                        type="button"
                        variant="outline"
                        onClick={async () => {
                          if (!emailInput.current?.reportValidity()) return;
                          setBusy(true);
                          setError(null);
                          try {
                            await passkey("login", email);
                            complete();
                          } catch (e) {
                            fail(e);
                          } finally {
                            setBusy(false);
                          }
                        }}
                      >
                        <Fingerprint size={17} />
                        使用 Passkey
                      </Button>
                    </>
                  )}
              </Form>
            )}
            <nav className="auth-links" aria-label="账户操作">
              {challenge ? (
                <Button
                  type="button"
                  variant="link"
                  disabled={busy}
                  onClick={() => {
                    setChallenge("");
                    setCode("");
                    setError(null);
                  }}
                >
                  <ArrowLeft size={14} />
                  重新登录
                </Button>
              ) : mode !== "login" ? (
                <Button
                  type="button"
                  variant="link"
                  disabled={busy}
                  onClick={() => switchMode("login")}
                >
                  <ArrowLeft size={14} />
                  返回登录
                </Button>
              ) : (
                <>
                  <Button
                    type="button"
                    variant="link"
                    disabled={busy}
                    onClick={() => switchMode("reset")}
                  >
                    忘记密码
                  </Button>
                  {policy.data?.email_verification && (
                    <Button
                      type="button"
                      variant="link"
                      onClick={() => switchMode("verify")}
                    >
                      验证邮箱
                    </Button>
                  )}
                  {policy.data?.registration_enabled && (
                    <Button
                      type="button"
                      variant="link"
                      onClick={() => switchMode("register")}
                    >
                      注册账户
                    </Button>
                  )}
                </>
              )}
            </nav>
          </CardContent>
        </Card>
        <p className="auth-footer">
          <ShieldCheck size={14} />
          账户验证与设备配对独立管理
        </p>
      </div>
    </main>
  );
}
