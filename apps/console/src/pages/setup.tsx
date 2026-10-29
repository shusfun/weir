import { useEffect, useId, useRef, useState } from "react";
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  Database,
  KeyRound,
  LockKeyhole,
  UserRound,
} from "lucide-react";
import { Steps, Step, Field, Form, FormGrid, Notice, CheckMark, IconTile, prefersReducedMotion, useBurst, SwitchField,
  Button,
  Card,
  CardContent,
  PasswordInput,
  Segmented,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@weir/ui";
import { TextField } from "../components/form-field";
import { api } from "../api";
import { accountPasswordError } from "../password";

type Driver = "sqlite" | "postgres";
type PostgresConfig = {
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
// 中心在本机探测到的数据服务；密码只报告「读到了」，本身不返回，安装时由中心补上。
type Detection = {
  postgres: null | {
    source: "1panel" | "port";
    host: string;
    port: number;
    user?: string;
    has_password: boolean;
    connected: boolean;
    version?: string;
    databases?: { name: string; has_data: boolean; unknown?: boolean }[];
    recommended?: string;
  };
  redis: null | {
    source: "1panel" | "port";
    address: string;
    has_password: boolean;
    status: "ok" | "auth_required" | "wrong_password";
    version?: string;
  };
};
const sourceLabel = { "1panel": "1Panel", port: "本机端口" } as const;
const steps = [
  { name: "初始化凭证", hint: "验证安装权限", icon: KeyRound },
  { name: "存储", hint: "SQLite 或 PostgreSQL + Redis", icon: Database },
  { name: "管理员", hint: "创建账户并锁定安装", icon: UserRound },
];
const sslLabels: Record<string, string> = {
  disable: "关闭（同机本地连接）",
  require: "加密连接",
  "verify-ca": "验证 CA",
  "verify-full": "验证 CA 与服务器名称",
};
export function SetupPage({ done }: { done: () => void }) {
  const id = useId();
  const [step, setStep] = useState(0),
    [token, setToken] = useState("");
  const [driver, setDriver] = useState<Driver>("sqlite"),
    [sqlitePath, setSqlitePath] = useState("");
  const [postgres, setPostgres] = useState<PostgresConfig>({
    host: "127.0.0.1",
    port: 5432,
    user: "gate",
    database: "gate",
    password: "",
    ssl_mode: "disable",
  });
  const [redis, setRedis] = useState<RedisConfig>({
    address: "127.0.0.1:6379",
    // 1Panel 等只设了 requirepass 的 Redis 没有单独的 ACL 用户
    username: "",
    password: "",
    tls: false,
  });
  const [detection, setDetection] = useState<Detection | null>(null),
    [detecting, setDetecting] = useState(false),
    [detectError, setDetectError] = useState("");
  const [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const [phase, setPhase] = useState<"form" | "switching" | "check_failed" | "ready">(
    "form",
  );
  const readyMark = useRef<HTMLSpanElement>(null);
  const burst = useBurst();
  useEffect(() => {
    if (phase !== "ready") return;
    // 圈画完、对勾落笔的时刻爆散
    const timer = setTimeout(() => readyMark.current && burst(readyMark.current, { distance: 72, count: 14 }), 620);
    return () => clearTimeout(timer);
  }, [phase, burst]);
  // 空路径表示使用服务端默认位置（/var/lib/weir/center/weir.db）。
  const storeConfig = () =>
    driver === "sqlite"
      ? { driver, sqlite_path: sqlitePath.trim() }
      : { driver, postgres, redis };
  async function waitUntilReady() {
    for (let attempt = 0; attempt < 40; attempt += 1) {
      try {
        const state = await api("/setup/status");
        const health = await fetch("/healthz");
        const body = (await health.json().catch(() => ({}))) as {
          status?: string;
        };
        if (state.installed && state.ready && health.ok && body.status === "ok") {
          // 安装完成是整个流程里最关键的一下：先画出对勾、爆散一次，再进入控制台
          setPhase("ready");
          setTimeout(done, prefersReducedMotion() ? 600 : 1500);
          return;
        }
      } catch {
        // 继续轮询，直到超时后显示可重试的失败状态。
      }
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
    setPhase("check_failed");
  }
  async function next() {
    setBusy(true);
    setError("");
    try {
      if (step === 0) {
        await api("/setup/verify", "POST", {}, token);
        const state = await api("/setup/status");
        if (!sqlitePath && state.default_sqlite_path) setSqlitePath(state.default_sqlite_path);
      }
      if (step === 1) await api("/setup/test-store", "POST", storeConfig(), token);
      if (step === 2) {
        if (password !== confirmation) throw Error("两次输入的密码不一致。");
        const passwordError = accountPasswordError(password);
        if (passwordError) throw Error(passwordError);
        setPhase("switching");
        try {
          await api("/setup/install", "POST", { store: storeConfig(), email, password }, token);
        } catch (installError) {
          const message = installError instanceof Error ? installError.message : "";
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
  // 探测本机的 PostgreSQL / Redis 并填入表单；密码框留空即使用中心读到的密码。
  async function detect() {
    setDetecting(true);
    setDetectError("");
    try {
      const found: Detection = await api("/setup/detect-store", "POST", {}, token);
      setDetection(found);
      const p = found.postgres;
      if (p) {
        setPostgres((v) => ({
          ...v,
          host: p.host,
          port: p.port,
          user: p.user || v.user,
          database: p.recommended || v.database,
          password: p.has_password ? "" : v.password,
        }));
      }
      const r = found.redis;
      if (r) setRedis((v) => ({ ...v, address: r.address, username: "", password: r.has_password ? "" : v.password }));
    } catch (e) {
      setDetectError(e instanceof Error ? e.message : "探测失败，请手动填写。");
    } finally {
      setDetecting(false);
    }
  }
  useEffect(() => {
    if (step === 1 && driver === "postgres" && !detection && !detecting && !detectError) void detect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, driver]);
  const pgDetected = detection?.postgres;
  const redisDetected = detection?.redis;
  // 用的是探测到的实例时，密码框留空表示使用中心读到的密码
  const pgUsesDetected =
    !!pgDetected?.has_password && pgDetected.host === postgres.host && pgDetected.port === postgres.port && pgDetected.user === postgres.user;
  const redisUsesDetected = !!redisDetected?.has_password && redisDetected.address === redis.address && !redis.username;
  const pickedDatabase = pgDetected?.databases?.find((d) => d.name === postgres.database.trim());
  const usedDatabases = pgDetected?.databases?.filter((d) => d.has_data).map((d) => d.name) ?? [];
  const freeDatabases = pgDetected?.databases?.filter((d) => !d.has_data && !d.unknown).map((d) => d.name) ?? [];
  function pg<K extends keyof PostgresConfig>(key: K, value: PostgresConfig[K]) {
    setPostgres((v) => ({ ...v, [key]: value }));
  }
  function rd<K extends keyof RedisConfig>(key: K, value: RedisConfig[K]) {
    setRedis((v) => ({ ...v, [key]: value }));
  }
  if (phase === "ready") {
    return (
      <main className="auth-screen setup-screen" data-setup-phase={phase}>
        <div className="setup-container flex flex-col items-center text-center">
          <IconTile ref={readyMark} size="xl" tone="neutral" className="rounded-full text-[var(--mg-ok)]">
            <CheckMark circle />
          </IconTile>
          <header className="setup-heading">
            <h1>安装完成</h1>
            <p role="status">正在进入控制台…</p>
          </header>
        </div>
      </main>
    );
  }
  if (phase !== "form") {
    return (
      <main className="auth-screen setup-screen" data-setup-phase={phase}>
        <div className="setup-container">
          <header className="setup-heading">
            <h1>{phase === "switching" ? "正在切换到正常服务" : "服务尚未就绪"}</h1>
            <p>
              {phase === "switching"
                ? "安装记录已保存，正在确认存储可用。"
                : "安装记录已保存，但服务还没有就绪。可以重新检查，不要再次提交安装。"}
            </p>
          </header>
          {phase === "check_failed" ? (
            <Notice tone="bad">服务尚未就绪。</Notice>
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
            <strong>Weir</strong>
          </div>
          <h1>初始化你的控制中心</h1>
          <p>选择存储，创建管理员后完成安装。</p>
        </header>
        <div className="setup-workspace">
          <nav className="setup-progress" aria-label="安装进度">
            <Steps>
              {steps.map((item, i) => (
                <Step
                  key={item.name}
                  index={i}
                  label={item.name}
                  caption={item.hint}
                  icon={<item.icon />}
                  status={i < step ? "complete" : i === step ? "current" : "upcoming"}
                />
              ))}
            </Steps>
            <p>使用 PostgreSQL 时只写入 weir schema，Redis 只使用 weir: 前缀，不清理其他应用数据。</p>
          </nav>
          <Card size="lg">
            <CardContent>
              <header className="setup-section-heading">
                <small>步骤 {step + 1} / 3</small>
                <h2>{["验证初始化凭证", "选择存储", "创建首位管理员"][step]}</h2>
                <p>
                  {
                    [
                      "使用服务器生成的短时凭证，确认你有安装权限。",
                      "单机推荐只用 SQLite，无需额外服务；已有数据库时可选 PostgreSQL + Redis。",
                      "保存账户后，安装入口将锁定；后续修改在后台完成。",
                    ][step]
                  }
                </p>
              </header>
              <Form busy={busy} onSubmit={next} className="setup-fields">
                  {step === 0 && (
                    <>
                      <TextField
                        name={id + "token"}
                        label="短时初始化凭证"
                        type="password"
                        required
                        value={token}
                        onChange={setToken}
                      />
                      <p className="setup-info">
                        安装结束时已打印凭证，有效期 30 分钟。找不到或过期时，在服务器上执行 <code>weir</code> 选{" "}
                        <code>6</code>：没过期就照旧显示，过期了自动换新。终端初始化使用 <code>weir center --setup</code>，不需要这份凭证。
                      </p>
                    </>
                  )}
                  {step === 1 && (
                    <>
                      <Segmented
                        aria-label="存储类型"
                        value={driver}
                        onValueChange={(v) => setDriver(v as Driver)}
                        options={[
                          ["sqlite", "只用 SQLite"],
                          ["postgres", "PostgreSQL + Redis"],
                        ]}
                      />
                      {driver === "sqlite" ? (
                        <TextField
                          name={id + "sqlite"}
                          label="数据库文件"
                          hint="留空使用服务器默认位置。"
                          value={sqlitePath}
                          onChange={setSqlitePath}
                        />
                      ) : (
                        <>
                          <div className="flex flex-col gap-2" aria-live="polite">
                            {detecting ? (
                              <Notice role="status">正在探测服务器上的 PostgreSQL 与 Redis…</Notice>
                            ) : detectError ? (
                              <Notice tone="warn">{detectError}</Notice>
                            ) : detection ? (
                              <>
                                {pgDetected ? (
                                  <Notice tone={pgDetected.connected ? "ok" : "warn"}>
                                    检测到 {sourceLabel[pgDetected.source]} 的 PostgreSQL {pgDetected.version} · {pgDetected.host}:{pgDetected.port}
                                    {pgDetected.connected
                                      ? `，已用 ${pgDetected.user} 登录。` + (freeDatabases.length ? `可用的库：${freeDatabases.join("、")}。` : "")
                                      : "，未能登录，请填写账号和密码。"}
                                    {usedDatabases.length > 0 && ` 已有 Weir 账户、不能重新初始化的库：${usedDatabases.join("、")}。`}
                                  </Notice>
                                ) : (
                                  <Notice tone="warn">本机没有发现 PostgreSQL，请填写地址。</Notice>
                                )}
                                {redisDetected ? (
                                  <Notice tone={redisDetected.status === "ok" ? "ok" : "warn"}>
                                    检测到 {sourceLabel[redisDetected.source]} 的 Redis {redisDetected.version} · {redisDetected.address}
                                    {
                                      {
                                        ok: redisDetected.has_password ? "，已用其中的密码连通。" : "，无需密码。",
                                        auth_required: "，需要密码，请填写。",
                                        wrong_password: "，1Panel 里的密码登录失败，请填写密码。",
                                      }[redisDetected.status]
                                    }
                                  </Notice>
                                ) : (
                                  <Notice tone="warn">本机没有发现 Redis，请填写地址。</Notice>
                                )}
                              </>
                            ) : null}
                            <Button type="button" variant="outline" size="sm" className="self-start" disabled={detecting} onClick={() => void detect()}>
                              {detecting ? "正在探测…" : "重新探测"}
                            </Button>
                          </div>
                          <FormGrid>
                            <TextField name={id + "host"} label="PostgreSQL 主机" required value={postgres.host} onChange={(v) => pg("host", v)} />
                            <TextField name={id + "port"} label="端口" type="number" required value={postgres.port} onChange={(v) => pg("port", Number(v))} />
                            <TextField
                              name={id + "database"}
                              label="数据库"
                              required
                              value={postgres.database}
                              onChange={(v) => pg("database", v)}
                              customError={pickedDatabase?.has_data ? "这个库里已有 Weir 账户，请换一个空库" : ""}
                            />
                            <TextField name={id + "user"} label="账号" required value={postgres.user} onChange={(v) => pg("user", v)} />
                          </FormGrid>
                          <TextField
                            name={id + "pg-password"}
                            label="数据库密码"
                            type="password"
                            value={postgres.password}
                            onChange={(v) => pg("password", v)}
                            placeholder={pgUsesDetected ? "留空使用 1Panel 里的密码" : undefined}
                            hint={pgUsesDetected ? "密码由中心直接读取，不经过浏览器。" : undefined}
                          />
                          <Field label="SSL 模式">
                            <Select value={postgres.ssl_mode} onValueChange={(v) => { if (v) pg("ssl_mode", v); }}>
                              <SelectTrigger>
                                <SelectValue>{sslLabels[postgres.ssl_mode]}</SelectValue>
                              </SelectTrigger>
                              <SelectContent>
                                {Object.entries(sslLabels).map(([value, label]) => (
                                  <SelectItem key={value} value={value}>{label}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </Field>
                          <FormGrid>
                            <TextField name={id + "address"} label="Redis 地址" required value={redis.address} onChange={(v) => rd("address", v)} />
                            <TextField
                              name={id + "redis-user"}
                              label="Redis ACL 用户"
                              hint="只设了密码的 Redis（如 1Panel）留空。"
                              value={redis.username}
                              onChange={(v) => rd("username", v)}
                            />
                          </FormGrid>
                          <TextField
                            name={id + "redis-password"}
                            label="Redis 密码"
                            type="password"
                            value={redis.password}
                            onChange={(v) => rd("password", v)}
                            placeholder={redisUsesDetected ? "留空使用 1Panel 里的密码" : undefined}
                            hint={redisUsesDetected ? "密码由中心直接读取，不经过浏览器。" : undefined}
                          />
                          <SwitchField
                            placement="end"
                            label="Redis TLS 加密连接"
                            description="根据现有 Redis 服务设置选择。"
                            checked={redis.tls}
                            onCheckedChange={(v) => rd("tls", v)}
                          />
                        </>
                      )}
                    </>
                  )}
                  {step === 2 && (
                    <>
                      <TextField
                        name={id + "email"}
                        label="管理员邮箱"
                        type="email"
                        required
                        autoComplete="username"
                        value={email}
                        onChange={setEmail}
                      />
                      <Field
                        label="密码"
                        htmlFor={id + "password"}
                        hint="至少 6 位，含大写字母、小写字母和数字。"
                        customError={password ? accountPasswordError(password) : ""}
                      >
                        <PasswordInput
                          id={id + "password"}
                          autoComplete="new-password"
                          required
                          value={password}
                          onChange={(e) => setPassword(e.target.value)}
                        />
                      </Field>
                      <Field
                        label="确认管理员密码"
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
                      <p className="setup-info">二次验证与通行密钥可在安装后的账户安全中设置。</p>
                    </>
                  )}
                {error && <Notice tone="bad">{error}</Notice>}
                <div className="setup-actions">
                  {step > 0 && (
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => {
                        setError("");
                        setStep((v) => v - 1);
                      }}
                    >
                      <ArrowLeft size={15} />
                      上一步
                    </Button>
                  )}
                  <Button type="submit" disabled={!token}>
                    {busy
                      ? step === 2
                        ? "正在安装…"
                        : step === 1
                          ? "正在测试连接…"
                          : "正在验证…"
                      : step === 2
                        ? "安装并锁定"
                        : step === 1
                          ? "测试连接并继续"
                          : "下一步"}
                    <ArrowRight size={15} />
                  </Button>
                </div>
              </Form>
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
