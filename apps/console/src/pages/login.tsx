import { useConsoleContext } from "../lib/console-context";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import {
  ArrowLeft,
  ArrowRight,
  ShieldCheck,
} from "lucide-react";
import { Field, Form, GlobeBackground, Notice, Button, Card, CardContent, Input, PasswordInput } from "@weir/ui";
import { api, ApiError, fieldErrors, formError, type FormError } from "../api";
import { HumanVerification, type HumanVerificationHandle } from "../components/human-verification";
import { BrandMark } from "../components/brand-mark";
import { accountPasswordError } from "../password";
import { conditionalPasskey } from "../lib/passkey";

type Policy = {
  human_login_required: boolean;
  registration_enabled: boolean;
  invite_required: boolean;
  email_verification: boolean;
  passkey_enabled: boolean;
  password_reset: boolean;
  registration_email_suffixes: string[];
};
type Mode = "login" | "reset" | "register";
const titles: Record<Mode, string> = {
  login: "欢迎回来",
  reset: "恢复账户",
  register: "创建账户",
};
const hints: Record<Mode, string> = {
  login: "登录 Weir，管理你的节点与订阅。",
  reset: "通过邮箱验证码设置新密码。原有会话会被撤销。",
  register: "邀请码可附带初始套餐；其余节点与订阅权限由管理员分配。",
};
// 服务端错误码 → 字段；其余显示在表单底部
const loginFields = {
  invalid_email: "email",
  email_taken: "email",
  email_suffix_denied: "email",
  email_disposable: "email",
  email_mx_missing: "email",
  email_mx_unavailable: "email",
  email_verification_required: "email",
  weak_password: "password",
  invalid_password: "password",
  invalid_credentials: "password",
  invalid_verification_code: "code",
  invalid_email_code: "code",
  invalid_code: "code",
  invalid_second_factor: "code",
  invalid_access_code: "invite_code",
  access_code_cooldown: "invite_code",
  second_factor_required: "code",
} as const;
type LoginField = (typeof loginFields)[keyof typeof loginFields];

export function LoginPage({ done }: { done: () => void }) {
  const id = useId(),
    emailInput = useRef<HTMLInputElement>(null);
  const [params, setParams] = useSearchParams();
  const raw = params.get("auth");
  const mode: Mode =
    raw === "verify" ? "register" : raw === "reset" || raw === "register" ? raw : "login";
  useConsoleContext({ page: "登录", tab: titles[mode] });
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
  const [inviteCode,setInviteCode]=useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<FormError<LoginField> | null>(null),
    [notice, setNotice] = useState("");
  const errors = useMemo(() => fieldErrors(error), [error]);
  const fail = (e: unknown) => setError(formError(e, loginFields));
  const [cooldown, setCooldown] = useState(0);
  const human = useRef<HumanVerificationHandle>(null);
  const [humanRequired, setHumanRequired] = useState(false);
  const [website, setWebsite] = useState("");
  const needsHuman = !challenge && (mode !== "login" || humanRequired || !!policy.data?.human_login_required);
  const humanFail = (e: unknown) => {
    if (e instanceof ApiError && (e.code === "human_verification_required" || e.code === "human_verification_invalid")) setHumanRequired(true);
    fail(e);
  };
  const passkeyAttempt = useRef<AbortController | null>(null);
  const doneRef = useRef(done);
  doneRef.current = done;
  const [passkeyError, setPasskeyError] = useState("");
  useEffect(() => {
    setPasskeyError("");
    if (mode !== "login" || challenge || !policy.data?.passkey_enabled) return;
    const controller = new AbortController();
    passkeyAttempt.current = controller;
    void conditionalPasskey(controller.signal).then((loggedIn) => {
      if (loggedIn && !controller.signal.aborted) { setParams({}, { replace: true }); doneRef.current(); }
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setPasskeyError(`通行密钥暂不可用：${formError(error, {}).message}仍可使用密码登录。`);
    });
    return () => { controller.abort(); if (passkeyAttempt.current === controller) passkeyAttempt.current = null; };
  }, [mode, challenge, policy.data?.passkey_enabled, setParams]);
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
    setHumanRequired(false);
  }, [mode]);
  const unavailable =
    (mode === "register" && !policy.data?.registration_enabled) ||
    (mode === "reset" && !policy.data?.password_reset);
  const showCode =
    !!challenge ||
    mode === "reset" ||
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
    passkeyAttempt.current?.abort();
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
        const proof = needsHuman ? await human.current?.solve("login") : undefined;
        if (needsHuman && !proof) throw new Error("请等待人机验证就绪");
        const response: { mfa_required?: boolean; challenge?: string } =
          await api("/auth/login", "POST", { email, password, website, altcha: proof?.altcha }, { traceId: proof?.traceId });
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
          : "/auth/register";
      const proof = await human.current?.solve(mode);
      if (!proof) throw new Error("请等待人机验证就绪");
      await api(endpoint, "POST", { email, password, code, invite_code: mode==="register"?inviteCode:undefined, website, altcha: proof.altcha, turnstile: proof.turnstile }, { traceId: proof.traceId });
      switchMode("login");
      setPassword("");
      setConfirmation("");
      setCode("");
      setParams({}, { replace: true });
      setNotice(
        mode === "reset"
          ? "密码已重设，请使用新密码登录。"
          : "账户创建成功，请登录查看初始套餐与授权。",
      );
    } catch (e) {
      humanFail(e);
    } finally {
      human.current?.reset();
      setBusy(false);
    }
  }
  async function requestCode() {
    if (!emailInput.current?.reportValidity() || busy || cooldown) return;
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      const proof = await human.current?.solve("email");
      if (!proof) throw new Error("请等待人机验证就绪");
      const result = await api("/auth/email", "POST", {
        email,
        purpose: mode === "reset" ? "reset" : "register",
        invite_code: mode === "register" ? inviteCode : undefined,
        altcha: proof.altcha,
        turnstile: proof.turnstile,
        website,
      }, { traceId: proof.traceId });
      setCooldown(result.retry_after ?? 60);
      setNotice(
        `若此邮箱符合条件，验证码将发送到邮箱；请检查收件箱与垃圾邮件。验证码 ${result.expires_in ?? 600} 秒有效。`,
      );
    } catch (e) {
      fail(e);
    } finally {
      human.current?.reset();
      setBusy(false);
    }
  }
  return (
    <main className="auth-screen">
      <GlobeBackground demo centerLon={-35} />
      <div className="auth-container">
        <a href="/" className="auth-brand" aria-label="Weir 首页">
          <BrandMark />
        </a>
        <h1 className="auth-title">{challenge ? "验证你的身份" : titles[mode]}</h1>
        <p className="auth-subtitle">
          {challenge ? "输入身份验证器中的六位验证码，或使用一枚未使用的恢复码。" : hints[mode]}
        </p>
        <Card className="auth-card" size="lg">
          <CardContent>
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
                      : "尚未配置邮件服务，无法找回密码。"}
                </Notice>
              </div>
            ) : (
              <Form key={`${mode}:${challenge ? "second-factor" : "credentials"}`} busy={busy} errors={errors} onSubmit={() => void submit()} className="auth-form">
                <div className="auth-honeypot" aria-hidden="true" inert>
                  <label htmlFor={id + "website"}>网站</label>
                  <Input id={id + "website"} name="website" autoComplete="off" tabIndex={-1} value={website} onChange={(e) => setWebsite(e.target.value)} />
                </div>
                {!challenge && (
                  <Field label="邮箱" htmlFor={id + "email"} name="email">
                    <Input
                      ref={emailInput}
                      id={id + "email"}
                      type="email"
                      autoComplete={mode === "login" ? "username webauthn" : "username"}
                      required
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      placeholder="you@example.com"
                    />
                  </Field>
                )}
                {!challenge && (
                  <Field
                    label={mode === "reset" ? "新密码" : "密码"}
                    htmlFor={id + "password"}
                    name="password"
                    hint={mode !== "login" ? "至少 6 个字符。" : undefined}
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
                {mode==="register"&&policy.data?.invite_required&&(
                  <Field label="邀请码" name="invite_code" hint="由管理员提供，一码一用；发送验证码前会先校验。">
                    <PasswordInput required minLength={51} maxLength={51} autoComplete="off" value={inviteCode} onChange={(e)=>setInviteCode(e.target.value.trim())}/>
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
                {needsHuman && <HumanVerification ref={human} purpose={mode} />}
                {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
                {passkeyError && mode === "login" && <Notice tone="warn">{passkeyError}</Notice>}
                {mode === "register" && !!policy.data?.registration_email_suffixes?.length && <Notice>允许的邮箱后缀：{policy.data.registration_email_suffixes.join("、")}</Notice>}
                {notice && (
                  <Notice tone="ok" role="status">
                    {notice}
                  </Notice>
                )}
                <Button
                  type="submit"
                  disabled={policy.isPending || !!unavailable}
                  busy={busy}
                  busyText="正在验证…"
                >
                  {challenge
                      ? "验证并登录"
                      : mode === "login"
                        ? "登录"
                        : mode === "reset"
                          ? "重设密码"
                          : "创建账户"}
                  <ArrowRight size={16} />
                </Button>
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
                  {policy.data?.password_reset && <Button
                    type="button"
                    variant="link"
                    disabled={busy}
                    onClick={() => switchMode("reset")}
                  >
                    忘记密码
                  </Button>}
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
        <p className="auth-footnote">背景是全球公开机房之间的随机航线，不代表任何真实节点或流量。</p>
        <p className="auth-footer">
          <ShieldCheck size={14} />
          账户验证与设备配对独立管理
        </p>
      </div>
    </main>
  );
}
