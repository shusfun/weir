import { useId, useState, type FormEvent } from "react";
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  Check,
  Database,
  KeyRound,
  LockKeyhole,
  UserRound,
} from "lucide-react";
import {
  Button,
  Card,
  CardContent,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Switch,
} from "@migate/ui";
import { Field } from "../components/form-field";
import { api } from "../api";

type DatabaseConfig = {
  host: string;
  port: number;
  user: string;
  database: string;
  password: string;
  ssl_mode: string;
};
type RedisConfig = {
  address: string;
  username: string;
  password: string;
  tls: boolean;
};
const steps = [
  { name: "初始化凭证", hint: "验证安装权限", icon: KeyRound },
  { name: "PostgreSQL", hint: "连接现有数据库", icon: Database },
  { name: "Redis", hint: "检查专用 ACL", icon: LockKeyhole },
  { name: "管理员", hint: "创建账户并锁定安装", icon: UserRound },
];
export function SetupPage({ done }: { done: () => void }) {
  const id = useId();
  const [step, setStep] = useState(0),
    [token, setToken] = useState("");
  const [database, setDatabase] = useState<DatabaseConfig>({
    host: "127.0.0.1",
    port: 5432,
    user: "gate",
    database: "gate",
    password: "",
    ssl_mode: "disable",
  });
  const [redis, setRedis] = useState<RedisConfig>({
    address: "127.0.0.1:6379",
    username: "migate",
    password: "",
    tls: false,
  });
  const [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const [phase, setPhase] = useState<"form" | "switching" | "check_failed">(
    "form",
  );
  async function waitUntilReady() {
    for (let attempt = 0; attempt < 40; attempt += 1) {
      try {
        const state = await api("/setup/status");
        const health = await fetch("/healthz");
        const body = (await health.json().catch(() => ({}))) as {
          status?: string;
        };
        if (
          state.installed &&
          state.ready &&
          health.ok &&
          body.status === "ok"
        ) {
          done();
          return;
        }
      } catch {
        // 继续轮询，直到超时后显示可重试的失败状态。
      }
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
    setPhase("check_failed");
  }
  async function next(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (step === 0) await api("/setup/verify", "POST", {}, token);
      if (step === 1) await api("/setup/test-db", "POST", database, token);
      if (step === 2) await api("/setup/test-redis", "POST", redis, token);
      if (step === 3) {
        if (password !== confirmation) throw Error("两次输入的密码不一致。");
        const length = new TextEncoder().encode(password).length;
        if (length < 12 || length > 72)
          throw Error("密码长度须为 12–72 字节。");
        setPhase("switching");
        try {
          await api(
            "/setup/install",
            "POST",
            { database, redis, email, password },
            token,
          );
        } catch (installError) {
          const message =
            installError instanceof Error ? installError.message : "";
          if (!message.includes("初始化已经完成")) {
            setPhase("form");
            throw installError;
          }
        }
        setToken("");
        setPassword("");
        setConfirmation("");
        await waitUntilReady();
        return;
      }
      setStep((v) => v + 1);
    } catch (e) {
      setError(e instanceof Error ? e.message : "初始化失败，请检查后重试。");
    } finally {
      setBusy(false);
    }
  }
  function pg<K extends keyof DatabaseConfig>(
    key: K,
    value: DatabaseConfig[K],
  ) {
    setDatabase((v) => ({ ...v, [key]: value }));
  }
  function rd<K extends keyof RedisConfig>(key: K, value: RedisConfig[K]) {
    setRedis((v) => ({ ...v, [key]: value }));
  }
  if (phase !== "form") {
    return (
      <main className="auth-screen setup-screen" data-setup-phase={phase}>
        <div className="setup-container">
          <header className="setup-heading">
            <h1>
              {phase === "switching"
                ? "正在切换到正常服务"
                : "服务尚未就绪"}
            </h1>
            <p>
              {phase === "switching"
                ? "安装记录已保存。正在确认数据库、Redis 和后台任务。"
                : "安装记录已保存，但正常服务还没有就绪。可以重新检查，不要再次提交安装。"}
            </p>
          </header>
          {phase === "check_failed" ? (
            <p className="auth-message error-message" role="alert">
              服务尚未就绪。
            </p>
          ) : (
            <p role="status">正在切换到正常服务</p>
          )}
          {phase === "check_failed" && (
            <Button
              type="button"
              onClick={() => {
                setPhase("switching");
                void waitUntilReady();
              }}
            >
              重试检查
            </Button>
          )}
        </div>
      </main>
    );
  }
  return (
    <main className="auth-screen setup-screen">
      <div className="setup-container">
        <header className="setup-heading">
          <div className="auth-brand">
            <span>
              <Activity size={26} />
            </span>
            <strong>MiGate</strong>
          </div>
          <h1>初始化你的控制中心</h1>
          <p>连接现有数据服务，创建管理员后完成安装。</p>
        </header>
        <div className="setup-workspace">
          <nav className="setup-progress" aria-label="安装进度">
            <ol>
              {steps.map((item, i) => (
                <li
                  key={item.name}
                  aria-current={i === step ? "step" : undefined}
                  className={i < step ? "complete" : ""}
                >
                  <span>
                    {i < step ? <Check size={17} /> : <item.icon size={17} />}
                  </span>
                  <div>
                    <strong>{item.name}</strong>
                    <small>{item.hint}</small>
                  </div>
                </li>
              ))}
            </ol>
            <p>
              仅设置本项目的数据空间。不会新建数据库服务或清理其他应用数据。
            </p>
          </nav>
          <Card>
            <CardContent>
              <header className="setup-section-heading">
                <small>步骤 {step + 1} / 4</small>
                <h2>
                  {
                    [
                      "验证初始化凭证",
                      "连接 PostgreSQL",
                      "连接 Redis",
                      "创建首位管理员",
                    ][step]
                  }
                </h2>
                <p>
                  {
                    [
                      "使用服务器生成的短时凭证，确认你有安装权限。",
                      "使用现有数据库与账号，业务数据保存在 migate schema。",
                      "使用专用 ACL 用户，业务键仅使用 migate: 前缀。",
                      "保存账户后，安装入口将锁定；后续修改在后台完成。",
                    ][step]
                  }
                </p>
              </header>
              <form onSubmit={next} className="stack" aria-busy={busy}>
                <fieldset disabled={busy} className="setup-fields">
                  {step === 0 && (
                    <>
                      <Field
                        name={id + "token"}
                        label="短时初始化凭证"
                        type="password"
                        value={token}
                        onChange={setToken}
                      />
                      <p className="setup-info">
                        凭证只保存在服务器的{" "}
                        <code>/etc/migate-control/setup-token</code>
                        ，权限 0600，有效期 30
                        分钟。过期或被拒绝时执行{" "}
                        <code>migate-control --init</code>
                        ，运行中的服务会立即改用新凭证并拒绝旧凭证。终端初始化使用{" "}
                        <code>migate-control --setup</code>，不需要这份凭证。
                      </p>
                    </>
                  )}
                  {step === 1 && (
                    <>
                      <div className="settings-field-grid">
                        <Field
                          name={id + "host"}
                          label="主机"
                          value={database.host}
                          onChange={(v) => pg("host", v)}
                        />
                        <Field
                          name={id + "port"}
                          label="端口"
                          type="number"
                          value={database.port}
                          onChange={(v) => pg("port", Number(v))}
                        />
                        <Field
                          name={id + "database"}
                          label="数据库"
                          value={database.database}
                          onChange={(v) => pg("database", v)}
                        />
                        <Field
                          name={id + "user"}
                          label="账号"
                          value={database.user}
                          onChange={(v) => pg("user", v)}
                        />
                      </div>
                      <Field
                        name={id + "pg-password"}
                        label="密码"
                        type="password"
                        value={database.password}
                        onChange={(v) => pg("password", v)}
                      />
                      <div className="field">
                        <Label htmlFor={id + "ssl"}>SSL 模式</Label>
                        <Select
                          value={database.ssl_mode}
                          onValueChange={(v) => {
                            if (v) pg("ssl_mode", v);
                          }}
                          disabled={busy}
                        >
                          <SelectTrigger id={id + "ssl"}>
                            <SelectValue>
                              {
                                (
                                  {
                                    disable: "关闭（同机本地连接）",
                                    require: "加密连接",
                                    "verify-ca": "验证 CA",
                                    "verify-full": "验证 CA 与服务器名称",
                                  } as Record<string, string>
                                )[database.ssl_mode]
                              }
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            {[
                              ["disable", "关闭（同机本地连接）"],
                              ["require", "加密连接"],
                              ["verify-ca", "验证 CA"],
                              ["verify-full", "验证 CA 与服务器名称"],
                            ].map(([value, label]) => (
                              <SelectItem key={value} value={value}>
                                {label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                    </>
                  )}
                  {step === 2 && (
                    <>
                      <Field
                        name={id + "address"}
                        label="连接地址"
                        value={redis.address}
                        onChange={(v) => rd("address", v)}
                      />
                      <Field
                        name={id + "redis-user"}
                        label="ACL 用户"
                        value={redis.username}
                        onChange={(v) => rd("username", v)}
                      />
                      <Field
                        name={id + "redis-password"}
                        label="密码"
                        type="password"
                        value={redis.password}
                        onChange={(v) => rd("password", v)}
                      />
                      <Label className="settings-policy">
                        <span>
                          <strong>TLS 加密连接</strong>
                          <small>根据现有 Redis 服务设置选择。</small>
                        </span>
                        <Switch
                          disabled={busy}
                          checked={redis.tls}
                          onCheckedChange={(v) => rd("tls", v)}
                          aria-label="TLS 加密连接"
                        />
                      </Label>
                    </>
                  )}
                  {step === 3 && (
                    <>
                      <Field
                        name={id + "email"}
                        label="管理员邮箱"
                        type="email"
                        autoComplete="username"
                        value={email}
                        onChange={setEmail}
                      />
                      <Field
                        name={id + "password"}
                        label="密码（12–72 字节）"
                        type="password"
                        value={password}
                        onChange={setPassword}
                      />
                      <Field
                        name={id + "confirm"}
                        label="确认管理员密码"
                        type="password"
                        value={confirmation}
                        onChange={setConfirmation}
                      />
                      <p className="setup-info">
                        注册默认关闭。邮件服务、二次验证策略与 Laya
                        可在安装后配置。
                      </p>
                    </>
                  )}
                </fieldset>
                {error && (
                  <p className="auth-message error-message" role="alert">
                    {error}
                  </p>
                )}
                <div className="setup-actions">
                  {step > 0 && (
                    <Button
                      type="button"
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        setError("");
                        setStep((v) => v - 1);
                      }}
                    >
                      <ArrowLeft size={15} />
                      上一步
                    </Button>
                  )}
                  <Button type="submit" disabled={busy || !token}>
                    {busy
                      ? step === 3
                        ? "正在安装…"
                        : "正在验证…"
                      : step === 3
                        ? "安装并锁定"
                        : "下一步"}
                    <ArrowRight size={15} />
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
        </div>
        <p className="auth-footer">
          <LockKeyhole size={14} />
          连接信息仅提交至当前控制中心
        </p>
      </div>
    </main>
  );
}
