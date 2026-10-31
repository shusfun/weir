// 添加 VPS：两种方式。「SSH 一次装好」：填登录信息 → 核对主机指纹 → 中心登录上去执行安装命令，实时看日志，装完自动上线；
// 「复制命令」：生成一次性配对命令（1 小时有效），自己在 VPS 上以 root 执行。登录密码与私钥只用于这一次安装，不保存。
import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Button, CodeBlock, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, FilePicker, Form, Input, Notice, PasswordInput, Segmented,
  Tabs, TabsList, TabsPanel, TabsTab, Textarea,
} from "@weir/ui";
import { ShieldCheck } from "lucide-react";
import { Link } from "react-router-dom";
import { api, clause } from "../api";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { objectHref } from "../lib/objects";
import { JobByID, type Job } from "./job";
import { useTick, WaitText } from "./wait";
import { pollingNote, useLive } from "../lib/live";

type Mode = "ssh" | "command";

// keyProblem：私钥内容一眼能看出不能用的情况（PuTTY 格式、不是 PEM / OpenSSH）；能不能解析由中心判断。
function keyProblem(key: string) {
  const k = key.trimStart();
  if (!k) return "";
  if (k.startsWith("PuTTY-User-Key-File")) return "这是 PuTTY 的 .ppk 私钥：请用 PuTTYgen 的「Conversions → Export OpenSSH key」导出后再选";
  if (!k.startsWith("-----BEGIN")) return "不是 PEM / OpenSSH 私钥（开头应为 -----BEGIN …）";
  return "";
}

export function InviteGate({ open, close, done }: { open: boolean; close: () => void; done: () => void }) {
  const [mode, setMode] = useState<Mode>("ssh");
  const [name, setName] = useState("");
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogTitle>添加 VPS</DialogTitle>
        <DialogDescription>装好后 VPS 主动连中心，之后建节点、改配置都在这里操作，不用再登录 VPS。</DialogDescription>
        <Tabs value={mode} onValueChange={(v) => setMode(v as Mode)}>
          <TabsList aria-label="安装方式">
            <TabsTab value="ssh">SSH 一次装好</TabsTab>
            <TabsTab value="command">复制安装命令</TabsTab>
          </TabsList>
          <TabsPanel value="ssh">{open && <SSHInstall name={name} setName={setName} close={close} done={done} />}</TabsPanel>
          <TabsPanel value="command">{open && <CommandInstall name={name} setName={setName} close={close} done={done} />}</TabsPanel>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function NameField({ name, setName }: { name: string; setName: (v: string) => void }) {
  return (
    <Field label="名称" required hint="例如：香港-1。可随时修改。" customError={name && !name.trim() ? "请输入名称" : ""}>
      <Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
    </Field>
  );
}

// SSHInstall：填写 → 核对指纹 → 安装中 / 结果。三步在同一个位置切换，按钮不移动。
function SSHInstall({ name, setName, close, done }: { name: string; setName: (v: string) => void; close: () => void; done: () => void }) {
  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [user, setUser] = useState("root");
  const [auth, setAuth] = useState<"password" | "key">("password");
  const [password, setPassword] = useState("");
  const [key, setKey] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [fileError, setFileError] = useState("");
  const [probe, setProbe] = useState<{ fingerprint: string; key_type: string } | null>(null);
  const [job, setJob] = useState<Job | null>(null);
  const [result, setResult] = useState<Job | null>(null);
  // 表单在子组件里，弹窗找不到它：提交键用 form 属性指向表单（页脚渲染在表单外）
  const formId = useId();
  const target = () => ({ host: host.trim(), port: Number(port) || 22, user: user.trim(), ...(auth === "password" ? { password } : { private_key: key, passphrase }) });
  const check = useFormAction(() => api("/admin/gates/ssh/probe", "POST", { host: host.trim(), port: Number(port) || 22, user: user.trim() }) as Promise<{ fingerprint: string; key_type: string }>, {
    fields: { invalid_ssh_host: "host", ssh_not_public: "host", ssh_unresolved: "host", invalid_ssh_port: "port", invalid_ssh_user: "user" },
    onSuccess: setProbe,
    wait: () => ({ label: `正在从中心连接 ${host.trim()}:${Number(port) || 22} 并读取主机指纹`, max: 15 }),
  });
  const install = useFormAction(() => api("/admin/gates/ssh/install", "POST", { ...target(), name: name.trim(), fingerprint: probe?.fingerprint }) as Promise<{ job: Job }>, {
    fields: { invalid_ssh_key: "key", ssh_key_passphrase_required: "passphrase", ssh_auth_required: auth === "password" ? "password" : "key", invalid_name: "name" },
    onSuccess: (r) => setJob(r.job),
  });
  useConsoleContext({
    dialog: { title: "添加 VPS", step: job ? "正在安装" : probe ? "核对主机指纹" : "填写 SSH 登录信息" },
    form: [field("name", "名称", name), field("host", "地址", host), field("port", "SSH 端口", port), field("user", "用户名", user), field("auth", "登录方式", auth === "password" ? "密码" : "私钥"),
      field("password", "登录密码", password, true), field("private_key", "私钥", key, true)],
  });
  const restart = () => { setJob(null); setResult(null); setProbe(null); install.reset(); check.reset(); };
  if (job) {
    const gid = result?.status === "done" ? Number(result.result?.gate_id) : 0;
    return (
      <div className="stack">
        <JobByID id={job.id} initial={job} openLogs onFinish={(j) => { setResult(j); if (j.status === "done") done(); }} />
        {result?.status === "done" && <Notice tone="ok">已安装并上线。{gid ? <Link className="underline" to={objectHref({ type: "gate", id: gid })} onClick={close}>打开这台 VPS</Link> : null}，点「＋ 协议」建节点。</Notice>}
        {result && result.status !== "done" && <Notice tone="warn">没有装好：{result.error}。可以改登录信息后重试，或换「复制安装命令」自己执行。</Notice>}
        <DialogFooter>
          {result && result.status !== "done" && <Button variant="outline" onClick={restart}>重新填写</Button>}
          <Button variant={result ? "default" : "outline"} onClick={close}>{result ? "完成" : "在后台继续"}</Button>
        </DialogFooter>
      </div>
    );
  }
  if (probe) {
    return (
      <Form id={formId} busy={install.busy} errors={install.errors} onSubmit={() => void install.submit()}>
        <Notice role="status">
          <ShieldCheck className="inline size-4" /> 第一次连接 <strong>{host}:{port}</strong>，主机公钥（{probe.key_type}）的指纹是：
          <CodeBlock className="mt-2 block break-all">{probe.fingerprint}</CodeBlock>
          <small className="mt-1 block">在 VPS 控制台或已登录的终端里运行 <code>ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub</code> 可以核对。指纹对得上再安装；安装时指纹变了会立即中止。</small>
        </Notice>
        <DialogFooter error={install.error}>
          <Button type="button" variant="outline" disabled={install.busy} onClick={() => setProbe(null)}>上一步</Button>
          <Button type="submit" form={formId} ripple busy={install.busy} busyText="正在启动…">指纹无误，开始安装</Button>
        </DialogFooter>
      </Form>
    );
  }
  const ready = name.trim() && host.trim() && user.trim() && (auth === "password" ? password : key.trim() && !keyProblem(key));
  return (
    <Form id={formId} busy={check.busy} errors={check.errors} onSubmit={() => void check.submit()}>
      <NameField name={name} setName={setName} />
      <div className="grid gap-3 sm:grid-cols-[1fr_96px_140px]">
        <Field label="VPS 地址" name="host" required hint="公网 IP 或域名。">
          <Input required value={host} placeholder="203.0.113.10" onChange={(e) => setHost(e.target.value)} autoComplete="off" />
        </Field>
        <Field label="SSH 端口" name="port" required>
          <Input inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
        </Field>
        <Field label="用户名" name="user" required hint="不是 root 时用 sudo。">
          <Input required value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" />
        </Field>
      </div>
      <Segmented aria-label="登录方式" value={auth} onValueChange={setAuth} options={[["password", "密码"], ["key", "私钥"]]} />
      {auth === "password" ? (
        <Field label="登录密码" name="password" required>
          <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="off" />
        </Field>
      ) : (
        <>
          {/* 选文件时的问题马上显示（error 是外部错误，不等失焦）；不能用的文件不填进私钥框 */}
          <Field label="私钥文件" name="keyfile" error={fileError} hint="云厂商下载的 .pem，或 ~/.ssh/id_ed25519 这类 OpenSSH 私钥。只在浏览器里读出内容填进下面的框，不单独上传。">
            <FilePicker accept=".pem,.key,.txt" anyType maxBytes={16 * 1024} onError={setFileError}
              onFile={async (f) => {
                const text = await f.text();
                const problem = keyProblem(text);
                setFileError(problem ? `${f.name}：${problem}` : "");
                if (!problem) setKey(text);
              }}>
              选择私钥文件，或拖到这里
            </FilePicker>
          </Field>
          <Field label="私钥" name="key" required customError={keyProblem(key)} hint="也可以直接粘贴私钥全文（-----BEGIN … PRIVATE KEY-----）。支持 OpenSSH、RSA、EC、PKCS#8；不支持 PuTTY 的 .ppk。">
            <Textarea rows={5} className="font-mono text-xs" value={key} onChange={(e) => setKey(e.target.value)} spellCheck={false} />
          </Field>
          <Field label="私钥密码" name="passphrase" hint="私钥没有设密码就留空。">
            <PasswordInput value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="off" />
          </Field>
        </>
      )}
      <small className="text-[var(--weir-ink-3)]">登录信息只用于这一次安装：不保存、不写日志，也不会交给 AI。中心只执行固定的安装命令，装完断开。</small>
      <DialogFooter error={check.error} status={check.wait && <WaitText wait={check.wait} />}>
        <Button type="button" variant="outline" disabled={check.busy} onClick={close}>取消</Button>
        <Button type="submit" form={formId} ripple disabled={!ready} busy={check.busy} busyText="正在连接…">连接并核对指纹</Button>
      </DialogFooter>
    </Form>
  );
}

function CommandInstall({ name, setName, close, done }: { name: string; setName: (v: string) => void; close: () => void; done: () => void }) {
  const [command, setCommand] = useState("");
  const [pairing, setPairing] = useState("");
  const [copyError, setCopyError] = useState("");
  const formId = useId();
  const invite = useFormAction(() => api("/admin/gates/invite", "POST", { name: name.trim() }) as Promise<{ command: string; pairing: string }>, {
    onSuccess: (res) => {
      setCommand(res.command);
      setPairing(res.pairing);
      done();
    },
  });
  useConsoleContext({ dialog: { title: "添加 VPS", step: command ? "已生成安装命令" : "填写名称" }, form: [field("name", "名称", name)] });
  if (command)
    return (
      <div className="stack">
        <CodeBlock className="block whitespace-pre-wrap break-all">{command}</CodeBlock>
        <small className="text-[var(--weir-ink-3)]">在 VPS 上以 root 执行。命令 1 小时内有效且只能使用一次。</small>
        {pairing && <PairingProgress digest={pairing} command={command} close={close} done={done} />}
        <DialogFooter error={copyError}>
          <Button variant="outline" onClick={() => { setCommand(""); setPairing(""); invite.reset(); }}>重新生成</Button>
          <CopyButton ripple value={command} onError={() => setCopyError("复制失败，请手动选择命令复制。")}>复制命令</CopyButton>
          <Button variant="outline" onClick={close}>完成</Button>
        </DialogFooter>
      </div>
    );
  return (
    <Form id={formId} busy={invite.busy} errors={invite.errors} onSubmit={() => void invite.submit()}>
      <NameField name={name} setName={setName} />
      <DialogFooter error={invite.error}>
        <Button type="button" variant="outline" disabled={invite.busy} onClick={close}>取消</Button>
        <Button type="submit" form={formId} ripple disabled={!name.trim()} busy={invite.busy} busyText="正在生成…">生成安装命令</Button>
      </DialogFooter>
    </Form>
  );
}

type Pairing = { state: "waiting" | "paired" | "online" | "expired" | "removed"; gate_id?: number; name?: string };

// PairingProgress：复制安装命令之后的进度，固定占一行：等待执行 → 已配对，等第一次心跳 → 已上线。
// 按凭证的摘要查询（凭证本身不出现在网址里）；事件流连着时配对与第一次心跳由中心提醒，断开时每 3 秒查一次。
// 等了 2 分钟还没有配对，提示在 VPS 上确认它能用 HTTPS 连到中心：连不上中心时这里不会有任何变化。
function PairingProgress({ digest, command, close, done }: { digest: string; command: string; close: () => void; done: () => void }) {
  const live = useLive();
  const [since] = useState(() => Date.now());
  const q = useQuery({
    queryKey: ["pairing", digest],
    queryFn: () => api("/admin/gates/pairing/" + digest) as Promise<Pairing>,
    refetchInterval: (query) => (["online", "expired", "removed"].includes(query.state.data?.state ?? "") ? false : live ? 30_000 : 3000),
  });
  const st = q.data?.state;
  useTick(st !== "online" && st !== "expired" && st !== "removed");
  const secs = Math.round((Date.now() - since) / 1000);
  const origin = command.match(/https:\/\/[^/\s'"]+/)?.[0] ?? "";
  const note = live ? "" : " · " + pollingNote.replace("每秒", "每 3 秒");
  if (q.error) return <Notice tone="bad" role="alert">读取配对进度失败：{clause(q.error)}。<Button size="xs" variant="ghost" onClick={() => void q.refetch()}>重新读取</Button></Notice>;
  if (st === "online")
    return (
      <Notice tone="ok" role="status">
        「{q.data?.name}」已上线。<Link className="underline" to={objectHref({ type: "gate", id: q.data!.gate_id! })} onClick={() => { done(); close(); }}>打开这台 VPS</Link>，点「＋ 协议」建节点。
      </Notice>
    );
  if (st === "expired") return <Notice tone="warn" role="status">这条命令已过期或已经用掉，没有配出 VPS。点「重新生成」换一条。</Notice>;
  if (st === "removed") return <Notice tone="warn" role="status">配出的 VPS 已被删除。</Notice>;
  const text = st === "paired" ? `已配对「${q.data?.name}」，等它第一次心跳 · 已等 ${secs} 秒${note}` : `等待在 VPS 上执行命令 · 已等 ${secs} 秒${note}`;
  return (
    <Notice role="status">
      <span className="tabular-nums">{text}</span>
      {st !== "paired" && secs >= 120 && origin && (
        <small className="mt-1 block">还没有配对：在 VPS 上运行 <code>curl -sS {origin}/healthz</code>，确认它能用 HTTPS 连到中心（防火墙、DNS、证书）；连不上中心时这里不会有任何变化。</small>
      )}
      {st === "paired" && secs >= 150 && <small className="mt-1 block">已配对但还没有心跳：在 VPS 上运行 <code>systemctl status weir-gate</code> 查看 Gate 服务。</small>}
    </Notice>
  );
}
