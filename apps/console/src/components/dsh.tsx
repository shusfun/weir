// 托管的 dsh（DeepSeek Harness）：右侧抽屉内嵌 dsh 原生界面（独立子域名，只能从这里打开），
// 以及 dsh 的启用 / 子域名设置与版本管理。DeepSeek 的 Key 在 dsh 自己的设置里填写，Weir 不保存。
import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogTitle, Field, Form, FormGrid, Input, Notice, Sheet, SheetContent, SkeletonList, StatusLamp, SwitchField, useConfirm } from "@weir/ui";
import { Bot, Download, RotateCcw, Star, Trash2 } from "lucide-react";
import { api, ApiError } from "../api";
import { useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { drawerId, field, useConsoleContext } from "../lib/console-context";
import { DshContextReporter } from "./dsh-context";
import { State } from "./feedback";
import { JobLine, latestJob, useJobs, useStartedJob } from "./job";
import { emptyReauth, Reauth, Section, useReauthAction } from "./system-settings";

export interface DshSettings {
  enabled: boolean;
  domain: string;
  version: string;
  previous?: string;
  auto_analyze: boolean;
}
interface DshInstalled {
  version: string;
  integrity: string;
  registry: string;
  installed_at: string;
}
export interface DshView {
  settings: DshSettings;
  status: { state: "stopped" | "starting" | "running" | "failed"; version?: string; error?: string; started_at?: string; logs: string[] };
  installed: DshInstalled[];
  unsupported: string;
  ready: boolean;
  // Weir 插件：label 为期望加载的插件，loaded 为实际加载的插件，note / error 为没有加载的原因。
  plugin: { label: string; note: string; loaded: string; error: string; verified: boolean };
}

// 打开抽屉时附带的提示：notice 显示在抽屉顶部。dsh 看到的页面与对象由各页面声明（见 lib/console-context.ts）。
export interface DshHint {
  notice: string;
  nodeId?: number;
  nodeName?: string;
}

const stateLabel: Record<string, [string, "ok" | "warn" | "bad" | "off" | "info"]> = {
  running: ["运行中", "ok"],
  starting: ["正在启动", "info"],
  failed: ["启动失败", "bad"],
  stopped: ["未运行", "off"],
};

export function useDsh(enabled = true) {
  // 已启用但还没在运行（刚保存、正在启动、刚重启）时 3 秒刷新一次，步骤清单与入口及时变成「运行中」
  return useQuery({ queryKey: ["dsh"], queryFn: () => api("/admin/dsh") as Promise<DshView>, enabled, refetchInterval: (q) => (q.state.data?.settings.enabled && q.state.data.status.state !== "running" ? 3000 : 30000) });
}

// 其他页面通过事件打开抽屉（例如节点上的「AI 调整」），附带上下文。
export function openDsh(hint?: DshHint) {
  window.dispatchEvent(new CustomEvent("weir:dsh-open", { detail: hint }));
}

// notReady 是 dsh 不能打开时的一句话原因（工具提示、日志用）；抽屉里显示的是 DshSetup 的步骤清单。
function notReady(v: DshView | undefined): string {
  if (!v) return "暂时无法读取 dsh 状态，请重试。";
  if (v.unsupported) return v.unsupported + "。";
  if (!v.settings.version) return "还没有下载 dsh。";
  if (!v.settings.domain || !v.settings.enabled) return "dsh 还没有启用。";
  switch (v.status.state) {
    case "starting":
      return "dsh 正在启动，稍等几秒。";
    case "failed":
      return "dsh 启动失败" + (v.status.error ? "：" + v.status.error : "") + "。";
    default:
      return "dsh 没有在运行。";
  }
}

// DshDrawer：管理员在任何页面都有右下角入口。dsh 已就绪时内嵌 dsh；没就绪时抽屉里列出还差的步骤，
// 每一步就地完成（下载、填地址并启用、等它启动），完成后自动打开 dsh。「让 dsh 起草」「AI 调整」也打开这里。
export function DshDrawer() {
  const dsh = useDsh();
  const [open, setOpen] = useState(false);
  const [src, setSrc] = useState("");
  const [error, setError] = useState("");
  const [hint, setHint] = useState<DshHint | null>(null);
  const ready = !!dsh.data?.ready;
  async function ticket() {
    setError("");
    setSrc("");
    try {
      // 每次打开都领取一次性票据；网关用它换会话，令牌不经过页面。同时告诉 dsh 管理员正在看哪里。
      const { url } = await api("/admin/dsh/ticket", "POST", { drawer: drawerId() });
      setSrc(url);
    } catch (e) {
      setError(e instanceof ApiError && e.code === "dsh_not_running" ? notReady(dsh.data) : e instanceof Error ? e.message : "暂时无法打开 dsh，请重试。");
    }
  }
  async function show(next?: DshHint | null) {
    drawerId(true); // 这个标签的抽屉会话：页面感知与界面导航都按它隔离
    setOpen(true);
    setHint(next ?? null);
    const view = (await dsh.refetch()).data;
    if (view?.ready) await ticket();
  }
  useEffect(() => {
    const onOpen = (e: Event) => void show((e as CustomEvent).detail);
    window.addEventListener("weir:dsh-open", onOpen);
    return () => window.removeEventListener("weir:dsh-open", onOpen);
  }, []);
  // 在抽屉里配置好、dsh 刚就绪时，接着打开它
  useEffect(() => {
    if (open && ready && !src && !error) void ticket();
  }, [open, ready]);
  return <DrawerView open={open} setOpen={setOpen} hint={hint} ready={ready} view={dsh.data} error={error} src={src} retry={() => void ticket()} openNew={() => void show()} />;
}

// DrawerView：抽屉本身；从某个节点打开时把那个节点声明为正在看的对象。
function DrawerView({ open, setOpen, hint, ready, view, error, src, retry, openNew }: { open: boolean; setOpen: (v: boolean) => void; hint: DshHint | null; ready: boolean; view?: DshView; error: string; src: string; retry: () => void; openNew: () => void }) {
  useConsoleContext(open && hint?.nodeId ? { object: { type: "node", name: hint.nodeName ?? "节点", id: hint.nodeId } } : null);
  return (
    <>
      <Button className="dsh-fab" size="icon-lg" aria-label={ready ? "打开 dsh" : "dsh：还没配置好，点开查看还差哪几步"} title={ready ? "打开 dsh（DeepSeek）" : "dsh 还没配置好：" + notReady(view)} onClick={openNew}>
        <Bot />
        {!ready && view && <span className="absolute -top-0.5 -right-0.5 size-2.5 rounded-full bg-[var(--mg-warn)]" aria-hidden />}
      </Button>
      {drawerId() && <DshContextReporter open={open} />}
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="right" title="dsh" description="DeepSeek Harness · 草稿会由 Weir 弹窗请你确认" className="w-[min(680px,100vw)] gap-3 p-3">
          <div className="flex h-full min-h-[70vh] flex-col gap-2">
            {hint && <Notice role="status">{hint.notice}</Notice>}
            {!ready ? (
              <div className="min-h-0 flex-1 overflow-y-auto"><DshSetup view={view} /></div>
            ) : error ? (
              <>
                <Notice tone="warn" role="status">{error}</Notice>
                <div className="actions">
                  <Button variant="outline" size="sm" onClick={retry}><RotateCcw />重试</Button>
                </div>
              </>
            ) : src ? (
              <iframe
                key={src}
                title="dsh"
                src={src}
                className="min-h-0 w-full flex-1 rounded-[var(--mg-radius)] border border-[var(--mg-line)] bg-[var(--mg-well)]"
                sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads allow-modals"
                allow="clipboard-write"
                referrerPolicy="no-referrer"
              />
            ) : <State loading />}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

type StepState = "done" | "current" | "todo";

// DshSetup：dsh 还差哪几步。只有当前这一步显示操作，做完自动进入下一步；做过的步骤保留一行结果。
export function DshSetup({ view }: { view?: DshView }) {
  if (!view) return <State loading />;
  const set = view.settings;
  const steps: [string, StepState][] = [
    ["下载 dsh", set.version ? "done" : "current"],
    ["填写 dsh 地址并启用", !set.version ? "todo" : set.enabled && set.domain ? "done" : "current"],
    ["等待 dsh 启动", !(set.version && set.enabled && set.domain) ? "todo" : view.status.state === "running" ? "done" : "current"],
    ["在 dsh 里填 DeepSeek Key", view.ready ? "current" : "todo"],
  ];
  return (
    <ol className="m-0 list-none space-y-3 p-0" aria-label="dsh 配置步骤">
      <li className="text-[13px] text-[var(--mg-ink-2)]">dsh 是 Weir 的 AI 助手：在任何页面都知道你正在看什么，帮你调节点、写驱动、研判安全事件。用之前还差下面几步。</li>
      {view.unsupported && <li><Notice tone="warn">{view.unsupported}。</Notice></li>}
      <SetupStep n={1} title={steps[0][0]} state={steps[0][1]} done={set.version ? `已下载 ${set.version}` : ""}>
        <DshFetchStep blocked={!!view.unsupported} />
      </SetupStep>
      <SetupStep n={2} title={steps[1][0]} state={steps[1][1]} done={set.domain ? `已启用，地址 ${set.domain}` : ""}>
        <DshAddressForm view={view} turnOn />
      </SetupStep>
      <SetupStep n={3} title={steps[2][0]} state={steps[2][1]} done="运行中">
        <DshStartStep view={view} />
      </SetupStep>
      <SetupStep n={4} title={steps[3][0]} state={steps[3][1]} done="">
        <small className="text-[var(--mg-ink-3)]">dsh 打开后在它自己的设置里填写 DeepSeek 的 API Key；Key 只保存在 dsh 里，Weir 不保存、也不会发给别处。</small>
      </SetupStep>
    </ol>
  );
}

function SetupStep({ n, title, state, done, children }: { n: number; title: string; state: StepState; done: string; children: ReactNode }) {
  return (
    <li className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" aria-current={state === "current" ? "step" : undefined}>
      <div className="flex items-center gap-2">
        <Badge variant="soft" tone={state === "done" ? "ok" : state === "current" ? "info" : "neutral"}>{state === "done" ? "已完成" : state === "current" ? `第 ${n} 步` : "待完成"}</Badge>
        <strong className="text-[13px]">{title}</strong>
        {state === "done" && done && <small className="min-w-0 truncate text-[var(--mg-ink-3)]">{done}</small>}
      </div>
      {state === "current" && children}
    </li>
  );
}

// DshFetchStep：下载推荐版本（Weir 插件已验证范围内最新的一个）；下载完自动设为当前版本。
function DshFetchStep({ blocked }: { blocked: boolean }) {
  const q = useQuery({ queryKey: ["dsh-releases"], queryFn: async () => (await api("/admin/dsh/releases")).releases as DshRelease[] });
  const jobs = useJobs("dsh");
  const started = useStartedJob();
  const client = useQueryClient();
  const [job, setJob] = useState<string | null>(null);
  const { run, isBusy } = useRowAction<string>();
  const pick = q.data?.find((r) => r.verified) ?? q.data?.[0];
  return (
    <div className="stack">
      {q.error ? <State error={q.error} onRetry={() => q.refetch()} /> : (
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" disabled={blocked || !pick || isBusy()} busy={isBusy()} busyText="正在开始…" onClick={() => pick && void run("fetch", async () => setJob(started((await api("/admin/dsh/fetch", "POST", { version: pick.version })).job.id)), `开始下载 dsh ${pick.version}`, { fail: "下载没有开始" })}>
            <Download />{pick ? `下载 dsh ${pick.version}` : "正在读取版本…"}
          </Button>
          {pick && <small className="text-[var(--mg-ink-3)]">{pick.verified ? "Weir 插件已验证这个版本" : "这个版本超出 Weir 插件已验证的范围：只启用基础预设"}；首次下载还会装 Node.js，约需几分钟。</small>}
        </div>
      )}
      <JobLine id={job} job={latestJob(jobs.data, "dsh:")} idle="下载进度显示在这里" onFinish={() => void client.invalidateQueries({ queryKey: ["dsh"] })} />
      <small className="text-[var(--mg-ink-3)]">想装别的版本：<Link className="text-[var(--mg-accent-text)] underline-offset-4 hover:underline" to="/kernels?tab=dsh">内核 → dsh</Link>。</small>
    </div>
  );
}

// DshStartStep：启用后由管理进程启动 dsh；失败时显示原因与最近日志，可以就地重启。
function DshStartStep({ view }: { view: DshView }) {
  const client = useQueryClient();
  const { run, isBusy } = useRowAction({ refetch: () => client.invalidateQueries({ queryKey: ["dsh"] }) });
  const st = view.status;
  const [label, tone] = stateLabel[st.state] ?? stateLabel.stopped;
  return (
    <div className="stack">
      <div className="flex flex-wrap items-center gap-2">
        <StatusLamp tone={tone} />
        <span>{label}</span>
        {st.error && <small className="min-w-0 flex-1 truncate text-[var(--mg-bad-ink)]" title={st.error}>{st.error}</small>}
        <Button className="ml-auto" size="sm" variant="outline" disabled={isBusy()} busy={isBusy()} busyText="正在重启…" onClick={() => void run("restart", () => api("/admin/dsh/restart", "POST"), "正在重启 dsh")}><RotateCcw />重启</Button>
      </div>
      <Collapsible title={`运行日志（最近 ${st.logs.length} 行，令牌已隐藏）`} defaultOpen={st.state === "failed"}>
        <CodeBlock className="block h-48 overflow-auto whitespace-pre text-xs">{st.logs.join("\n") || "（还没有日志）"}</CodeBlock>
      </Collapsible>
    </div>
  );
}

// DshAddressForm：dsh 地址、启用、后台分析。启用 dsh 与改地址要验证身份（让 AI 接入面板的工具、决定中心把哪个地址转给 dsh），
// 只改后台分析开关、停用不用。turnOn：配置清单里默认勾上「启用」，填好地址一次保存。
function DshAddressForm({ view, turnOn }: { view: DshView; turnOn?: boolean }) {
  const client = useQueryClient();
  const saved = view.settings;
  const [draft, setDraft] = useState<DshSettings | null>(null);
  const v = draft ?? (turnOn ? { ...saved, enabled: true } : saved);
  const [re, setRe] = useState(emptyReauth);
  const needReauth = (v.enabled && !saved.enabled) || v.domain.trim().toLowerCase() !== saved.domain;
  const save = useReauthAction(() => api("/admin/dsh/settings", "PUT", { ...v, domain: v.domain.trim(), ...(needReauth ? re : {}) }), {
    ok: "已保存",
    fields: { invalid_dsh_domain: "domain" },
    onSuccess: () => {
      setDraft(null);
      setRe(emptyReauth);
      void client.invalidateQueries({ queryKey: ["dsh"] });
    },
  });
  useConsoleContext({ form: [field("dsh_domain", "dsh 地址", v.domain), field("dsh_enabled", "启用 dsh", v.enabled ? "是" : "否"), field("auto_analyze", "可疑事件自动分析", v.auto_analyze ? "是" : "否"), field("password", "当前密码", re.password, true)] });
  return (
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <Field label="dsh 地址" name="domain" required={v.enabled} hint="与控制台同一个站点、不同的地址：子域名（如 dsh.你的域名）或带端口的地址（如 你的域名:61873）。在反代里把它也转发到中心（与控制台相同的上游），中心按地址分流。">
        <div className="flex gap-2">
          <Input className="min-w-0 flex-1" autoComplete="off" data-1p-ignore data-lpignore="true" placeholder={"dsh." + siteOf(window.location.hostname)} value={v.domain} onChange={(e) => setDraft({ ...v, domain: e.target.value })} />
          <Button type="button" variant="outline" onClick={() => setDraft({ ...v, domain: randomLabel() + "." + siteOf(window.location.hostname) })}>随机生成</Button>
        </div>
      </Field>
      <DshDomainGuide domain={v.domain} />
      <FormGrid>
        <SwitchField label="启用 dsh" checked={v.enabled} onCheckedChange={(on) => setDraft({ ...v, enabled: on })} />
        <SwitchField label="Laya 判为可疑的事件自动交给 dsh 分析" checked={v.auto_analyze} onCheckedChange={(on) => setDraft({ ...v, auto_analyze: on })} />
      </FormGrid>
      <Reauth value={re} onChange={setRe} need={needReauth} reason="启用 dsh 会让 AI 接入面板的工具，改地址决定中心把哪个地址转给 dsh：请确认是你本人。" />
      <div className="actions"><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></div>
      {save.error && <Notice tone="bad">{save.error}</Notice>}
    </Form>
  );
}

// DshSettingsCard：系统设置里的 dsh：没配好时就是同一份步骤清单；配好后显示运行状态与地址、开关。
export function DshSettingsCard() {
  const q = useDsh();
  return (
    <Section title="dsh（DeepSeek Harness）" description="dsh 由中心托管，只监听本机回环。浏览器只能从控制台右下角的抽屉里打开它；其他途径访问 dsh 地址只会得到一个空的 404，看起来就像这个地址不存在。DeepSeek 的 Key 在 dsh 自己的设置里填写，Weir 不保存。">
      {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : !q.data.settings.version || !q.data.settings.enabled || !q.data.settings.domain ? <DshSetup view={q.data} /> : (
        <div className="stack">
          <DshStartStep view={q.data} />
          <DshAddressForm view={q.data} />
        </div>
      )}
    </Section>
  );
}

// siteOf 取控制台所在站点（粗略取最后两段，供占位与随机生成用；服务端按公共后缀表校验）。
function siteOf(host: string) {
  const parts = host.replace(/:\d+$/, "").split(".");
  return parts.length > 2 ? parts.slice(-2).join(".") : host;
}

function randomLabel() {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  const bytes = crypto.getRandomValues(new Uint8Array(6));
  return Array.from(bytes, (b) => chars[b % chars.length]).join("");
}

// DshDomainGuide：DNS 与证书由管理员配置；可选的反代断开配置让浏览器显示原生错误页。Weir 不改反代。
function DshDomainGuide({ domain }: { domain: string }) {
  const site = siteOf(domain.includes(".") ? domain : window.location.hostname);
  const snippet = `# dsh 站点（${domain || "随机名." + site}）：把 404 改为直接断开连接
proxy_intercept_errors on;
error_page 404 = @drop;
location @drop { return 444; }

# 反代默认站点：未配置的域名同样断开
server {
    listen 443 ssl default_server;
    ssl_reject_handshake on;
}`;
  return (
    <Collapsible title="DNS、证书与反代（建议）">
      <ul className="m-0 list-disc space-y-1 pl-5 text-[13px] text-[var(--mg-ink-2)]">
        <li>证书用泛域名证书（*.{site}），不要为这个子域名单独签证书：单独签发的名字会写进公开的证书透明日志。</li>
        <li>最好配泛解析（*.{site}），让 dsh 子域名和任意不存在的子域名解析结果一样。</li>
        <li>dsh 子域名只出现在管理员页面里；登录页、公开页、订阅与错误信息里都不会出现。</li>
        <li>可选：想让浏览器直接显示原生的连接错误，在反代的 dsh 站点里加入下面的配置（OpenResty / Nginx）。</li>
      </ul>
      <CodeBlock className="block overflow-auto whitespace-pre text-xs">{snippet}</CodeBlock>
    </Collapsible>
  );
}

interface DshRelease {
  version: string;
  published_at: string;
  tags?: string[];
  // 在 Weir 插件已验证的版本范围内
  verified?: boolean;
}

// DshVersions：与内核相同的版本管理：下载（官方摘要校验）、设为当前版本（切换前备份会话）、回退、删除。
export function DshVersions() {
  const confirm = useConfirm();
  const q = useDsh();
  const jobs = useJobs("dsh");
  const [releases, setReleases] = useState(false);
  const { run: act, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const set = q.data?.settings;
  return (
    <Section title="dsh" description="DeepSeek Harness 官方 npm 包，与内核一样由中心下载：Node.js 运行环境按 nodejs.org 的官方摘要校验，dsh 按 npm 官方源的 sha512 摘要校验，镜像只用来加速。切换版本前会备份 dsh 的会话与设置。">
      {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <div className="stack">
          <div className="flex flex-wrap items-center gap-2">
            <small className="text-[var(--mg-ink-3)]">{set?.version ? `当前版本 ${set.version}` : "尚未下载：第一次下载的版本会自动设为当前版本"}{set?.previous ? `，上一版本 ${set.previous}` : ""}</small>
            <Button size="sm" variant="outline" disabled={!!q.data.unsupported} onClick={() => setReleases(true)}><Download />拉取版本</Button>
            {set?.previous && q.data.installed.some((i) => i.version === set.previous) && (
              <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void act("rollback", () => api("/admin/dsh/version", "PUT", { version: set.previous }), `已回退到 ${set.previous}`)}><RotateCcw />回退到 {set.previous}</Button>
            )}
          </div>
          {q.data.unsupported && <Notice tone="warn">{q.data.unsupported}。</Notice>}
          <JobLine job={latestJob(jobs.data, "dsh:")} idle="没有进行中的下载" onFinish={() => void q.refetch()} />
          {q.data.installed.length === 0 ? <small>尚未下载</small> : (
            <div className="flex flex-wrap gap-2">
              {q.data.installed.map((i) => (
                <span key={i.version} className="flex items-center gap-1 rounded-[var(--mg-radius)] border border-[var(--mg-line)] px-2 py-1" title={`${i.integrity.slice(0, 24)}… · 来自 ${i.registry} · ${date(i.installed_at)}`}>
                  <code className="text-xs">{i.version}</code>
                  {set?.version === i.version ? <Badge variant="soft" tone="ok">当前</Badge> : (
                    <>
                      <Button size="xs" variant="ghost" disabled={isBusy()} onClick={() => void act(i.version, () => api("/admin/dsh/version", "PUT", { version: i.version }), `已切换到 ${i.version}，dsh 正在重启`)}><Star />设为当前</Button>
                      <Button size="xs" variant="ghost" aria-label={"删除 " + i.version} disabled={isBusy()} onClick={async () => (await confirm({ title: `删除 dsh ${i.version}？`, confirmText: "删除", tone: "danger" })) && void act(i.version, () => api("/admin/dsh/versions/" + encodeURIComponent(i.version), "DELETE"), "已删除")}><Trash2 /></Button>
                    </>
                  )}
                </span>
              ))}
            </div>
          )}
          <PluginStatus view={q.data} />
        </div>
      )}
      {releases && <DshReleases close={() => setReleases(false)} fetched={() => void q.refetch()} />}
    </Section>
  );
}

// PluginStatus：Weir 插件的加载情况。插件随 Weir 发版适配；dsh 版本超出已验证范围时只启用基础预设、后台安全分析暂停。
function PluginStatus({ view }: { view: DshView }) {
  const p = view.plugin;
  return (
    <div className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
      <div className="flex flex-wrap items-center gap-2">
        <strong>Weir 插件</strong>
        {p.loaded ? <Badge variant="soft" tone="ok">{p.loaded}</Badge> : <Badge variant="soft" tone="warn">{p.verified ? "未加载" : "版本未验证"}</Badge>}
      </div>
      <small className="text-[var(--mg-ink-3)]">提供「Weir 助手」预设、控制台上下文、Weir 说明与 skill（调节点、写驱动、研判安全事件）。插件不可用时只启用基础预设，Weir 的工具照常可用。</small>
      {p.note && <Notice tone="warn">{p.note}</Notice>}
      {p.error && <Notice tone="bad">{p.error}</Notice>}
    </div>
  );
}

function DshReleases({ close, fetched }: { close: () => void; fetched: () => void }) {
  const q = useQuery({ queryKey: ["dsh-releases"], queryFn: async () => (await api("/admin/dsh/releases")).releases as DshRelease[] });
  const jobs = useJobs("dsh");
  const [job, setJob] = useState<string | null>(null);
  const started = useStartedJob();
  const { run, isBusy } = useRowAction<string>();
  const download = (version: string) => run(version, async () => setJob(started((await api("/admin/dsh/fetch", "POST", { version })).job.id)), `开始下载 dsh ${version}`, { fail: "下载没有开始" });
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogTitle>dsh · 上游版本</DialogTitle>
        <DialogDescription>来自 npm 官方源的 @deepseek-ai/dsh，任何版本都能装；超出 Weir 插件已验证范围的版本只启用基础预设、后台安全分析暂停。首次下载还会安装 Node.js 运行环境（可续传），需要几分钟；关掉窗口下载照常进行。</DialogDescription>
        <JobLine id={job} job={latestJob(jobs.data, "dsh:")} idle="点版本右侧的「下载」开始；进度显示在这里" onFinish={fetched} />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="stack mg-focus-gutter max-h-[50vh] overflow-y-auto">
            {q.data.map((r) => (
              <li key={r.version} className="flex items-center justify-between gap-2">
                <span className="flex flex-wrap items-center gap-2"><code>{r.version}</code>{(r.tags ?? []).map((t) => <Badge key={t} variant="soft" tone={t === "latest" ? "ok" : "neutral"}>{t}</Badge>)}{r.verified ? <Badge variant="soft" tone="ok">插件已验证</Badge> : <Badge variant="soft" tone="neutral" title="超出 Weir 插件已验证的范围：只启用基础预设，后台安全分析暂停">未验证</Badge>}<small className="text-[var(--mg-ink-3)]">{date(r.published_at)}</small></span>
                <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void download(r.version)}>下载</Button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
