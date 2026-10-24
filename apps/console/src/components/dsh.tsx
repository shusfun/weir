// 托管的 dsh（DeepSeek Harness）：右侧抽屉内嵌 dsh 原生界面（独立子域名，只能从这里打开），
// 以及 dsh 的启用 / 子域名设置与版本管理。DeepSeek 的 Key 在 dsh 自己的设置里填写，MiGate 不保存。
import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Dialog, DialogContent, DialogDescription, DialogTitle, Field, Input, Label, Notice, Sheet, SheetContent, SkeletonList, StatusLamp, Switch, useToast } from "@migate/ui";
import { Bot, Download, RotateCcw, Star, Trash2 } from "lucide-react";
import { api } from "../api";
import { date } from "../lib/models";
import { State } from "./feedback";
import { Reauth, Section, useSave } from "./system-settings";

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
  // MiGate 插件：label 为期望加载的插件，loaded 为实际加载的插件，note / error 为没有加载的原因。
  plugin: { label: string; note: string; loaded: string; error: string; adapted: boolean };
}

// 打开抽屉时附带的上下文：notice 显示在抽屉顶部；nodeId / 当前页面一起交给 dsh（get_console_context）。
export interface DshHint {
  notice: string;
  nodeId?: number;
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

export function DshDrawer() {
  const dsh = useDsh();
  const [open, setOpen] = useState(false);
  const [src, setSrc] = useState("");
  const [error, setError] = useState("");
  const [hint, setHint] = useState<DshHint | null>(null);
  async function show(next?: DshHint | null) {
    setOpen(true);
    setHint(next ?? null);
    setError("");
    try {
      // 每次打开都领取一次性票据；网关用它换会话，令牌不经过页面。同时告诉 dsh 管理员正在看哪里。
      const { url } = await api("/admin/dsh/ticket", "POST", { node_id: next?.nodeId ?? 0, path: window.location.pathname + window.location.search });
      setSrc(url);
    } catch (e) {
      setSrc("");
      setError(String(e));
    }
  }
  useEffect(() => {
    const onOpen = (e: Event) => void show((e as CustomEvent).detail);
    window.addEventListener("migate:dsh-open", onOpen);
    return () => window.removeEventListener("migate:dsh-open", onOpen);
  }, []);
  if (!dsh.data?.ready) return null;
  return (
    <>
      <Button className="dsh-fab" size="icon-lg" aria-label="打开 dsh" title="打开 dsh（DeepSeek）" onClick={() => void show()}>
        <Bot />
      </Button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="right" title="dsh" description="DeepSeek Harness · 草稿会由 MiGate 弹窗请你确认" className="w-[min(680px,100vw)] gap-3 p-3">
          <div className="flex h-full min-h-[70vh] flex-col gap-2">
            {hint && <Notice role="status">{hint.notice}</Notice>}
            {error ? <Notice tone="bad">{error}</Notice> : src ? (
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
  const [re, setRe] = useState({ password: "", code: "" });
  const { busy, error, run } = useSave();
  const v = draft ?? q.data?.settings;
  const st = q.data?.status;
  const [label, tone] = stateLabel[st?.state ?? "stopped"];
  return (
    <Section title="dsh（DeepSeek Harness）" description="dsh 由中心托管，只监听本机回环。浏览器只能从控制台右下角的抽屉里打开它；直接访问 dsh 子域名一律拒绝。DeepSeek 的 Key 在 dsh 自己的设置里填写，MiGate 不保存。">
      {q.isPending || q.error || !v || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(() => api("/admin/dsh/settings", "PUT", { ...v, ...re }), "已保存", () => { setDraft(null); setRe({ password: "", code: "" }); void client.invalidateQueries({ queryKey: ["dsh"] }); }); }}>
          {q.data.unsupported && <Notice tone="warn">{q.data.unsupported}。</Notice>}
          <div className="flex flex-wrap items-center gap-3">
            <StatusLamp tone={tone} />
            <strong>{label}</strong>
            {st?.version && <code className="text-xs">{st.version}</code>}
            {!v.version && <small className="text-[var(--mg-ink-3)]">尚未选择版本，请先到「内核」页面下载 dsh。</small>}
            {st?.error && <small className="text-[var(--mg-bad-ink)]">{st.error}</small>}
            <Button type="button" size="sm" variant="outline" disabled={busy || !v.enabled} onClick={() => void run(() => api("/admin/dsh/restart", "POST"), "正在重启 dsh", () => void q.refetch())}><RotateCcw />重启</Button>
          </div>
          <Field label="dsh 子域名" htmlFor="dsh-domain" hint="例如 dsh.你的域名。请在现有反代里把它也转发到中心（与控制台相同的上游），并配好 HTTPS 证书；中心按域名分流。">
            <Input id="dsh-domain" placeholder="dsh.example.com" value={v.domain} onChange={(e) => setDraft({ ...v, domain: e.target.value })} />
          </Field>
          <div className="grid gap-2 sm:grid-cols-2">
            <Label className="actions"><Switch aria-label="启用 dsh" checked={v.enabled} onCheckedChange={(on) => setDraft({ ...v, enabled: on })} />启用 dsh</Label>
            <Label className="actions"><Switch aria-label="可疑事件自动交给 dsh 分析" checked={v.auto_analyze} onCheckedChange={(on) => setDraft({ ...v, auto_analyze: on })} />Laya 判为可疑的事件自动交给 dsh 分析</Label>
          </div>
          <Reauth value={re} onChange={setRe} />
          {error && <Notice tone="bad">{error}</Notice>}
          <div className="actions"><Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button></div>
          {st && st.logs.length > 0 && (
            <details>
              <summary className="cursor-pointer text-sm">运行日志（最近 {st.logs.length} 行，令牌已隐藏）</summary>
              <CodeBlock className="mt-2 block max-h-64 overflow-auto whitespace-pre text-xs">{st.logs.join("\n")}</CodeBlock>
            </details>
          )}
        </form>
      )}
    </Section>
  );
}

interface DshRelease {
  version: string;
  published_at: string;
  tags?: string[];
}

// DshVersions：与内核相同的版本管理：下载（官方摘要校验）、设为当前版本（切换前备份会话）、回退、删除。
export function DshVersions() {
  const toast = useToast();
  const q = useDsh();
  const [releases, setReleases] = useState(false);
  const [busy, setBusy] = useState("");
  async function act(key: string, fn: () => Promise<unknown>, ok: string) {
    setBusy(key);
    try {
      await fn();
      toast(ok, { tone: "ok" });
      await q.refetch();
    } catch (e) {
      toast("操作未完成", { description: String(e), tone: "bad" });
    } finally {
      setBusy("");
    }
  }
  const set = q.data?.settings;
  return (
    <Section title="dsh" description="DeepSeek Harness 官方 npm 包，与内核一样由中心下载：Node.js 运行环境按 nodejs.org 的官方摘要校验，dsh 按 npm 官方源的 sha512 摘要校验，镜像只用来加速。切换版本前会备份 dsh 的会话与设置。">
      {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <div className="stack">
          <div className="flex flex-wrap items-center gap-2">
            <small className="text-[var(--mg-ink-3)]">{set?.version ? `当前版本 ${set.version}` : "尚未选择版本"}{set?.previous ? `，上一版本 ${set.previous}` : ""}</small>
            <Button size="sm" variant="outline" disabled={!!q.data.unsupported} onClick={() => setReleases(true)}><Download />拉取版本</Button>
            {set?.previous && q.data.installed.some((i) => i.version === set.previous) && (
              <Button size="sm" variant="outline" disabled={!!busy} onClick={() => void act("rollback", () => api("/admin/dsh/version", "PUT", { version: set.previous }), `已回退到 ${set.previous}`)}><RotateCcw />回退到 {set.previous}</Button>
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
                      <Button size="xs" variant="ghost" disabled={!!busy} onClick={() => void act(i.version, () => api("/admin/dsh/version", "PUT", { version: i.version }), `已切换到 ${i.version}，dsh 正在重启`)}><Star />设为当前</Button>
                      <Button size="xs" variant="ghost" aria-label={"删除 " + i.version} disabled={!!busy} onClick={() => window.confirm(`删除 dsh ${i.version}？`) && void act(i.version, () => api("/admin/dsh/versions/" + encodeURIComponent(i.version), "DELETE"), "已删除")}><Trash2 /></Button>
                    </>
                  )}
                </span>
              ))}
            </div>
          )}
          <PluginStatus view={q.data} busy={busy} act={act} />
        </div>
      )}
      {releases && <DshReleases close={() => setReleases(false)} fetched={() => void q.refetch()} />}
    </Section>
  );
}

// PluginStatus：MiGate 插件的加载情况与创造模式适配入口。适配产物走草稿：MiGate 校验、试启动后弹窗请你确认。
function PluginStatus({ view, busy, act }: { view: DshView; busy: string; act: (key: string, fn: () => Promise<unknown>, ok: string) => Promise<void> }) {
  const client = useQueryClient();
  const p = view.plugin;
  return (
    <div className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
      <div className="flex flex-wrap items-center gap-2">
        <strong>MiGate 插件</strong>
        {p.loaded ? <Badge variant="soft" tone="ok">{p.loaded}</Badge> : <Badge variant="soft" tone="warn">{p.label ? "未加载" : "不兼容"}</Badge>}
        {p.adapted && <Badge variant="soft" tone="neutral">已适配</Badge>}
      </div>
      <small className="text-[var(--mg-ink-3)]">提供「MiGate 助手」预设、控制台上下文与 MiGate 说明。插件不可用时只启用基础预设，MiGate 的工具照常可用。</small>
      {p.note && <Notice tone="warn">{p.note}</Notice>}
      {p.error && <Notice tone="bad">{p.error}</Notice>}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={!view.ready} onClick={() => openDsh({ notice: "在 dsh 里换到创造模式（Creator）预设，让它按工作区 migate-plugin/ADAPT.md 适配插件；改好后回到「内核 → dsh」点「提交适配草稿」。" })}><Bot />用创造模式适配</Button>
        <Button size="sm" variant="outline" disabled={!view.ready || !!busy} onClick={() => void act("adapt", async () => { await api("/admin/dsh/plugin/adapt", "POST"); await client.invalidateQueries({ queryKey: ["ai-drafts"] }); }, "适配草稿已试启动通过，请在弹窗里确认")}>{busy === "adapt" ? "正在校验并试启动…" : "提交适配草稿"}</Button>
        {p.adapted && <Button size="sm" variant="ghost" disabled={!!busy} onClick={() => window.confirm("改回 MiGate 内置插件？") && void act("restore", () => api("/admin/dsh/plugin", "DELETE"), "已改回内置插件，dsh 正在重启")}><RotateCcw />改回内置插件</Button>}
      </div>
    </div>
  );
}

function DshReleases({ close, fetched }: { close: () => void; fetched: () => void }) {
  const toast = useToast();
  const q = useQuery({ queryKey: ["dsh-releases"], queryFn: async () => (await api("/admin/dsh/releases")).releases as DshRelease[] });
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  async function fetchVersion(version: string) {
    setBusy(version);
    setError("");
    try {
      // 后台安装（首次还要下载 Node.js）：每 3 秒查询一次，最多等 35 分钟。
      await api("/admin/dsh/fetch", "POST", { version });
      for (let i = 0; i < 700; i++) {
        await new Promise((r) => setTimeout(r, 3000));
        const st = await api("/admin/dsh/fetch?version=" + encodeURIComponent(version));
        if (st.status === "done") {
          toast(`已下载 dsh ${version}`, { tone: "ok", description: `摘要与官方源一致，来自 ${st.registry}` });
          fetched();
          return;
        }
        if (st.status === "failed") throw new Error("下载失败：" + st.error);
        if (st.status === "none") throw new Error("下载状态已丢失，请重试。");
      }
      throw new Error("下载超时，请稍后查看已下载的版本。");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>dsh · 上游版本</DialogTitle>
        <DialogDescription>来自 npm 官方源的 @deepseek-ai/dsh。dsh 仍是开发者预览版，版本之间可能不兼容；出问题时可以回退到上一版本。首次下载还会安装 Node.js 运行环境，需要几分钟。</DialogDescription>
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="stack mg-focus-gutter max-h-[50vh] overflow-y-auto">
            {q.data.map((r) => (
              <li key={r.version} className="flex items-center justify-between gap-2">
                <span className="flex flex-wrap items-center gap-2"><code>{r.version}</code>{(r.tags ?? []).map((t) => <Badge key={t} variant="soft" tone={t === "latest" ? "ok" : "neutral"}>{t}</Badge>)}<small className="text-[var(--mg-ink-3)]">{date(r.published_at)}</small></span>
                <Button size="sm" variant="outline" disabled={!!busy} onClick={() => void fetchVersion(r.version)}>{busy === r.version ? "正在下载…" : "下载"}</Button>
              </li>
            ))}
          </ul>
        )}
        {error && <Notice tone="bad">{error}</Notice>}
      </DialogContent>
    </Dialog>
  );
}
