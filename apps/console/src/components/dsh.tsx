// 托管的 dsh（DeepSeek Harness）：右侧抽屉内嵌 dsh 原生界面（独立子域名，只能从这里打开），
// 以及 dsh 的启用 / 子域名设置与版本管理。DeepSeek 的 Key 在 dsh 自己的设置里填写，Weir 不保存。
import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogTitle, Field, Form, FormGrid, Input, Notice, Sheet, SheetContent, SkeletonList, StatusLamp, SwitchField, useConfirm } from "@weir/ui";
import { Bot, Download, RotateCcw, Star, Trash2 } from "lucide-react";
import { api, ApiError } from "../api";
import { useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { drawerId, useConsoleContext } from "../lib/console-context";
import { DshContextReporter } from "./dsh-context";
import { State } from "./feedback";
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
  plugin: { label: string; note: string; loaded: string; error: string; adapted: boolean };
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
  return useQuery({ queryKey: ["dsh"], queryFn: () => api("/admin/dsh") as Promise<DshView>, enabled, refetchInterval: (q) => (q.state.data?.status.state === "starting" ? 3000 : 30000) });
}

// 其他页面通过事件打开抽屉（例如节点上的「AI 调整」），附带上下文。
export function openDsh(hint?: DshHint) {
  window.dispatchEvent(new CustomEvent("migate:dsh-open", { detail: hint }));
}

// notReady 是 dsh 不能打开时由 Weir 自己显示的原因；dsh 子域名本身只会返回空 404，看不出原因。
function notReady(v: DshView | undefined): string {
  if (!v) return "暂时无法读取 dsh 状态，请重试。";
  if (v.unsupported) return v.unsupported + "。";
  if (!v.settings.version) return "还没有选择 dsh 版本，请先到「内核 → dsh」下载并设为当前版本。";
  switch (v.status.state) {
    case "starting":
      return "dsh 正在启动，稍等几秒再试。";
    case "failed":
      return "dsh 启动失败" + (v.status.error ? "：" + v.status.error : "") + "。可以到「系统设置 → AI」查看运行日志或重启。";
    default:
      return "dsh 没有在运行。可以到「系统设置 → AI」查看状态或重启。";
  }
}

export function DshDrawer() {
  const dsh = useDsh();
  const [open, setOpen] = useState(false);
  const [src, setSrc] = useState("");
  const [error, setError] = useState("");
  const [hint, setHint] = useState<DshHint | null>(null);
  async function show(next?: DshHint | null) {
    drawerId(true); // 这个标签的抽屉会话：页面感知与界面导航都按它隔离
    setOpen(true);
    setHint(next ?? null);
    setError("");
    setSrc("");
    // 打开前先经 Weir 接口确认 dsh 在运行；没在运行时在抽屉里说明原因。
    const view = (await dsh.refetch()).data;
    if (!view?.ready) {
      setError(notReady(view));
      return;
    }
    try {
      // 每次打开都领取一次性票据；网关用它换会话，令牌不经过页面。同时告诉 dsh 管理员正在看哪里。
      const { url } = await api("/admin/dsh/ticket", "POST", { drawer: drawerId() });
      setSrc(url);
    } catch (e) {
      setError(e instanceof ApiError && e.code === "dsh_not_running" ? notReady(view) : e instanceof Error ? e.message : "暂时无法打开 dsh，请重试。");
    }
  }
  useEffect(() => {
    const onOpen = (e: Event) => void show((e as CustomEvent).detail);
    window.addEventListener("migate:dsh-open", onOpen);
    return () => window.removeEventListener("migate:dsh-open", onOpen);
  }, []);
  // 入口按「已启用」显示：dsh 暂时没在运行时也能点开，看到原因。
  if (!dsh.data?.settings.enabled || !dsh.data.settings.domain) return null;
  return <DrawerView open={open} setOpen={setOpen} hint={hint} error={error} src={src} retry={() => void show(hint)} openNew={() => void show()} />;
}

// DrawerView：抽屉本身；从某个节点打开时把那个节点声明为正在看的对象。
function DrawerView({ open, setOpen, hint, error, src, retry, openNew }: { open: boolean; setOpen: (v: boolean) => void; hint: DshHint | null; error: string; src: string; retry: () => void; openNew: () => void }) {
  useConsoleContext(open && hint?.nodeId ? { object: { type: "node", name: hint.nodeName ?? "节点", id: hint.nodeId } } : null);
  return (
    <>
      <Button className="dsh-fab" size="icon-lg" aria-label="打开 dsh" title="打开 dsh（DeepSeek）" onClick={openNew}>
        <Bot />
      </Button>
      {drawerId() && <DshContextReporter open={open} />}
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="right" title="dsh" description="DeepSeek Harness · 草稿会由 Weir 弹窗请你确认" className="w-[min(680px,100vw)] gap-3 p-3">
          <div className="flex h-full min-h-[70vh] flex-col gap-2">
            {hint && <Notice role="status">{hint.notice}</Notice>}
            {error ? (
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

// DshSettingsCard：启用、子域名、后台分析开关与运行状态。
export function DshSettingsCard() {
  const q = useDsh();
  const client = useQueryClient();
  const [draft, setDraft] = useState<DshSettings | null>(null);
  const [re, setRe] = useState(emptyReauth);
  const v = draft ?? q.data?.settings;
  const st = q.data?.status;
  const [label, tone] = stateLabel[st?.state ?? "stopped"];
  const save = useReauthAction(() => api("/admin/dsh/settings", "PUT", { ...v, ...re }), {
    ok: "已保存",
    fields: { invalid_dsh_domain: "domain" },
    onSuccess: () => {
      setDraft(null);
      setRe(emptyReauth);
      void client.invalidateQueries({ queryKey: ["dsh"] });
    },
  });
  const { run, isBusy } = useRowAction({ refetch: q.refetch });
  return (
    <Section title="dsh（DeepSeek Harness）" description="dsh 由中心托管，只监听本机回环。浏览器只能从控制台右下角的抽屉里打开它；其他途径访问 dsh 子域名只会得到一个空的 404，看起来就像这个域名不存在。DeepSeek 的 Key 在 dsh 自己的设置里填写，Weir 不保存。">
      {q.isPending || q.error || !v || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          {q.data.unsupported && <Notice tone="warn">{q.data.unsupported}。</Notice>}
          <div className="flex flex-wrap items-center gap-3">
            <StatusLamp tone={tone} />
            <strong>{label}</strong>
            {st?.version && <code className="text-xs">{st.version}</code>}
            {!v.version && <small className="text-[var(--mg-ink-3)]">尚未选择版本，请先到「内核」页面下载 dsh。</small>}
            {st?.error && <small className="text-[var(--mg-bad-ink)]">{st.error}</small>}
            <Button type="button" size="sm" variant="outline" disabled={isBusy() || !v.enabled} onClick={() => void run("restart", () => api("/admin/dsh/restart", "POST"), "正在重启 dsh")}><RotateCcw />重启</Button>
          </div>
          <Field label="dsh 子域名" name="domain" hint="用随机名（如 k3f9x2.你的域名），不要用 dsh.；在现有反代里把它也转发到中心（与控制台相同的上游），中心按域名分流。">
            <div className="flex gap-2">
              <Input className="min-w-0 flex-1" autoComplete="off" data-1p-ignore data-lpignore="true" placeholder={"k3f9x2." + siteOf(window.location.hostname)} value={v.domain} onChange={(e) => setDraft({ ...v, domain: e.target.value })} />
              <Button type="button" variant="outline" onClick={() => setDraft({ ...v, domain: randomLabel() + "." + siteOf(window.location.hostname) })}>随机生成</Button>
            </div>
          </Field>
          <DshDomainGuide domain={v.domain} />
          <FormGrid>
            <SwitchField label="启用 dsh" checked={v.enabled} onCheckedChange={(on) => setDraft({ ...v, enabled: on })} />
            <SwitchField label="Laya 判为可疑的事件自动交给 dsh 分析" checked={v.auto_analyze} onCheckedChange={(on) => setDraft({ ...v, auto_analyze: on })} />
          </FormGrid>
          <Reauth value={re} onChange={setRe} />
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          <div className="actions"><Button type="submit" disabled={save.busy}>{save.busy ? "正在保存…" : "保存"}</Button></div>
          {st && st.logs.length > 0 && (
            <Collapsible title={`运行日志（最近 ${st.logs.length} 行，令牌已隐藏）`}>
              <CodeBlock className="block max-h-64 overflow-auto whitespace-pre text-xs">{st.logs.join("\n")}</CodeBlock>
            </Collapsible>
          )}
        </Form>
      )}
    </Section>
  );
}

// siteOf 取控制台所在站点（粗略取最后两段，供占位与随机生成用；服务端按公共后缀表校验）。
function siteOf(host: string) {
  const parts = host.split(".");
  return parts.length > 2 ? parts.slice(-2).join(".") : host;
}

function randomLabel() {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  const bytes = crypto.getRandomValues(new Uint8Array(6));
  return Array.from(bytes, (b) => chars[b % chars.length]).join("");
}

// DshDomainGuide：DNS 与证书由管理员配置；可选的反代断开配置让浏览器显示原生错误页。Weir 不改反代。
function DshDomainGuide({ domain }: { domain: string }) {
  const site = siteOf(domain.includes(".") ? domain.split(".").slice(1).join(".") : window.location.hostname);
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
}

// DshVersions：与内核相同的版本管理：下载（官方摘要校验）、设为当前版本（切换前备份会话）、回退、删除。
export function DshVersions() {
  const confirm = useConfirm();
  const q = useDsh();
  const [releases, setReleases] = useState(false);
  const { run: act, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const set = q.data?.settings;
  return (
    <Section title="dsh" description="DeepSeek Harness 官方 npm 包，与内核一样由中心下载：Node.js 运行环境按 nodejs.org 的官方摘要校验，dsh 按 npm 官方源的 sha512 摘要校验，镜像只用来加速。切换版本前会备份 dsh 的会话与设置。">
      {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <div className="stack">
          <div className="flex flex-wrap items-center gap-2">
            <small className="text-[var(--mg-ink-3)]">{set?.version ? `当前版本 ${set.version}` : "尚未选择版本"}{set?.previous ? `，上一版本 ${set.previous}` : ""}</small>
            <Button size="sm" variant="outline" disabled={!!q.data.unsupported} onClick={() => setReleases(true)}><Download />拉取版本</Button>
            {set?.previous && q.data.installed.some((i) => i.version === set.previous) && (
              <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void act("rollback", () => api("/admin/dsh/version", "PUT", { version: set.previous }), `已回退到 ${set.previous}`)}><RotateCcw />回退到 {set.previous}</Button>
            )}
          </div>
          {q.data.unsupported && <Notice tone="warn">{q.data.unsupported}。</Notice>}
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
          <PluginStatus view={q.data} isBusy={isBusy} act={act} />
        </div>
      )}
      {releases && <DshReleases close={() => setReleases(false)} fetched={() => void q.refetch()} />}
    </Section>
  );
}

// PluginStatus：Weir 插件的加载情况与创造模式适配入口。适配产物走草稿：Weir 校验、试启动后弹窗请你确认。
function PluginStatus({ view, isBusy, act }: { view: DshView; isBusy: (key?: string) => boolean; act: (key: string, fn: () => Promise<unknown>, ok: string) => Promise<boolean> }) {
  const confirm = useConfirm();
  const client = useQueryClient();
  const p = view.plugin;
  return (
    <div className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
      <div className="flex flex-wrap items-center gap-2">
        <strong>Weir 插件</strong>
        {p.loaded ? <Badge variant="soft" tone="ok">{p.loaded}</Badge> : <Badge variant="soft" tone="warn">{p.label ? "未加载" : "不兼容"}</Badge>}
        {p.adapted && <Badge variant="soft" tone="neutral">已适配</Badge>}
      </div>
      <small className="text-[var(--mg-ink-3)]">提供「Weir 助手」预设、控制台上下文与 Weir 说明。插件不可用时只启用基础预设，Weir 的工具照常可用。</small>
      {p.note && <Notice tone="warn">{p.note}</Notice>}
      {p.error && <Notice tone="bad">{p.error}</Notice>}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={!view.ready} onClick={() => openDsh({ notice: "在 dsh 里换到创造模式（Creator）预设，让它按工作区 migate-plugin/ADAPT.md 适配插件；改好后回到「内核 → dsh」点「提交适配草稿」。" })}><Bot />用创造模式适配</Button>
        <Button size="sm" variant="outline" disabled={!view.ready || isBusy()} onClick={() => void act("adapt", async () => { await api("/admin/dsh/plugin/adapt", "POST"); await client.invalidateQueries({ queryKey: ["ai-drafts"] }); }, "适配草稿已试启动通过，请在弹窗里确认")}>{isBusy("adapt") ? "正在校验并试启动…" : "提交适配草稿"}</Button>
        {p.adapted && <Button size="sm" variant="ghost" disabled={isBusy()} onClick={async () => (await confirm({ title: "改回 Weir 内置插件？", confirmText: "改回" })) && void act("restore", () => api("/admin/dsh/plugin", "DELETE"), "已改回内置插件，dsh 正在重启")}><RotateCcw />改回内置插件</Button>}
      </div>
    </div>
  );
}

// 后台安装（首次还要下载 Node.js）：每 3 秒查询一次，最多等 35 分钟。返回下载所用的源。
async function fetchVersion(version: string): Promise<string> {
  await api("/admin/dsh/fetch", "POST", { version });
  for (let i = 0; i < 700; i++) {
    await new Promise((r) => setTimeout(r, 3000));
    const st = await api("/admin/dsh/fetch?version=" + encodeURIComponent(version));
    if (st.status === "done") return st.registry;
    if (st.status === "failed") throw new Error("下载失败：" + st.error);
    if (st.status === "none") throw new Error("下载状态已丢失，请重试。");
  }
  throw new Error("下载超时，请稍后查看已下载的版本。");
}

function DshReleases({ close, fetched }: { close: () => void; fetched: () => void }) {
  const q = useQuery({ queryKey: ["dsh-releases"], queryFn: async () => (await api("/admin/dsh/releases")).releases as DshRelease[] });
  const { run, isBusy } = useRowAction<string>({ refetch: fetched });
  const download = (version: string) =>
    run(version, () => fetchVersion(version), `已下载 dsh ${version}`, { detail: (registry) => `摘要与官方源一致，来自 ${registry}`, fail: "dsh 下载未完成" });
  return (
    <Dialog open onOpenChange={(o) => !o && !isBusy() && close()}>
      <DialogContent size="lg">
        <DialogTitle>dsh · 上游版本</DialogTitle>
        <DialogDescription>来自 npm 官方源的 @deepseek-ai/dsh。dsh 仍是开发者预览版，版本之间可能不兼容；出问题时可以回退到上一版本。首次下载还会安装 Node.js 运行环境，需要几分钟。</DialogDescription>
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="stack mg-focus-gutter max-h-[50vh] overflow-y-auto">
            {q.data.map((r) => (
              <li key={r.version} className="flex items-center justify-between gap-2">
                <span className="flex flex-wrap items-center gap-2"><code>{r.version}</code>{(r.tags ?? []).map((t) => <Badge key={t} variant="soft" tone={t === "latest" ? "ok" : "neutral"}>{t}</Badge>)}<small className="text-[var(--mg-ink-3)]">{date(r.published_at)}</small></span>
                <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void download(r.version)}>{isBusy(r.version) ? "正在下载…" : "下载"}</Button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
