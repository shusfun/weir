import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  Fingerprint,
  ShieldCheck,
} from "lucide-react";
import { Button, Card, CardContent, Input, Label } from "@migate/ui";
import { api } from "../api";
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
const describe = (e: unknown) =>
  e instanceof Error ? e.message : "操作失败，请重试。";

export function LoginPage({ done }: { done: () => void }) {
  const id = useId(),
    form = useRef<HTMLFormElement>(null);
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
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
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
    setError("");
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
    setError("");
    setNotice("");
    setParams(next === "login" ? {} : { auth: next }, { replace: true });
  }
  function complete() {
    setPassword("");
    setCode("");
    setParams({}, { replace: true });
    done();
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy || unavailable) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      if (!challenge && (mode === "register" || mode === "reset")) {
        const passwordError = accountPasswordError(password);
        if (passwordError) throw Error(passwordError);
        if (password !== confirmation) throw Error("两次输入的密码不一致。");
      }
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
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }
  async function requestCode() {
    const input = form.current?.querySelector<HTMLInputElement>(
      'input[type="email"]',
    );
    if (!input?.reportValidity() || busy || cooldown) return;
    setBusy(true);
    setError("");
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
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-screen">
      <div className="auth-container">
        <a href="/" className="auth-brand" aria-label="MiGate 首页">
          <span>
            <Activity size={26} />
          </span>
          <strong>MiGate</strong>
        </a>
        <p className="auth-subtitle">独立节点，统一管理。</p>
        <Card className="auth-card">
          <CardContent>
            <header className="auth-heading">
              <span className="auth-heading-icon">
                {challenge ? (
                  <ShieldCheck size={22} />
                ) : (
                  <Fingerprint size={22} />
                )}
              </span>
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
              <div className="auth-message error-message" role="alert">
                登录方式暂时无法读取。
                <Button
                  type="button"
                  variant="link"
                  onClick={() => policy.refetch()}
                >
                  重新读取
                </Button>
              </div>
            )}
            {unavailable && !policy.isPending ? (
              <div className="stack">
                <p className="notice" role="status">
                  {policy.isError
                    ? "当前账户策略未知，请重新读取后再继续。"
                    : mode === "register"
                      ? "注册暂未开放，请联系管理员创建账户。"
                      : "当前未启用邮箱验证。"}
                </p>
              </div>
            ) : (
              <form
                ref={form}
                onSubmit={submit}
                className="auth-form"
                aria-busy={busy}
              >
                {!challenge && (
                  <div className="field">
                    <Label htmlFor={id + "email"}>邮箱</Label>
                    <Input
                      id={id + "email"}
                      type="email"
                      autoComplete="username"
                      required
                      value={email}
                      disabled={busy}
                      onChange={(e) => setEmail(e.target.value)}
                      placeholder="you@example.com"
                    />
                  </div>
                )}
                {!challenge && mode !== "verify" && (
                  <div className="field">
                    <Label htmlFor={id + "password"}>
                      {mode === "reset" ? "新密码" : "密码"}
                    </Label>
                    <Input
                      id={id + "password"}
                      type="password"
                      autoComplete={
                        mode === "login" ? "current-password" : "new-password"
                      }
                      required
                      disabled={busy}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                    />
                    {mode !== "login" && (
                      <small>至少 6 位，须包含大写字母、小写字母和数字。</small>
                    )}
                  </div>
                )}
                {!challenge && (mode === "reset" || mode === "register") && (
                  <div className="field">
                    <Label htmlFor={id + "confirm"}>确认密码</Label>
                    <Input
                      id={id + "confirm"}
                      type="password"
                      autoComplete="new-password"
                      required
                      disabled={busy}
                      value={confirmation}
                      onChange={(e) => setConfirmation(e.target.value)}
                    />
                  </div>
                )}
                {showCode && (
                  <div className="field">
                    <Label htmlFor={id + "code"}>
                      {challenge ? "验证码 / 恢复码" : "邮箱验证码"}
                    </Label>
                    <Input
                      id={id + "code"}
                      autoComplete="one-time-code"
                      autoFocus={!!challenge}
                      required
                      disabled={busy}
                      value={code}
                      onChange={(e) => setCode(e.target.value.trim())}
                    />
                    {!challenge && (
                      <Button
                        type="button"
                        variant="outline"
                        disabled={busy || cooldown > 0}
                        onClick={requestCode}
                      >
                        {cooldown ? `${cooldown} 秒后重新发送` : "发送验证码"}
                      </Button>
                    )}
                  </div>
                )}
                {error && (
                  <p className="auth-message error-message" role="alert">
                    {error}
                  </p>
                )}
                {notice && (
                  <p className="auth-message auth-success" role="status">
                    {notice}
                  </p>
                )}
                <Button
                  type="submit"
                  disabled={busy || policy.isPending || !!unavailable}
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
                        disabled={busy}
                        onClick={async () => {
                          const input =
                            form.current?.querySelector<HTMLInputElement>(
                              'input[type="email"]',
                            );
                          if (!input?.reportValidity()) return;
                          setBusy(true);
                          setError("");
                          try {
                            await passkey("login", email);
                            complete();
                          } catch (e) {
                            setError(describe(e));
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
              </form>
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
                    setError("");
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
