// 版本与更新：从 GitHub Release 自更新。中心只写请求，root 的 weir-update 自己下载、按官方摘要校验、
// 用包里的安装脚本安装，失败自动切回；可切回本机保留的上一版本，或重装最近 3 个正式版本之一。
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Form, Notice, Progress, useToast } from "@weir/ui";
import { api } from "../api";
import { clock, date } from "../lib/models";
import { State } from "./feedback";
import { size } from "./job";
import { emptyReauth, Reauth, Section, useReauthAction } from "./system-settings";
import { field, useConsoleContext } from "../lib/console-context";

interface Release { version: string; published_at: string; notes: string; url: string }
interface Status {
  id: string;
  action: "update" | "rollback";
  from: string;
  version: string;
  state: "downloading" | "installing" | "done" | "failed";
  progress?: { done: number; total: number; speed: number; source: string };
  logs: string[];
  error?: string;
  started_at: string;
  finished_at?: string;
}
interface View { current: string; blocker: string; previous: string; latest?: Release; has_update?: boolean; older?: Release[]; status?: Status; check_error?: string }

export function UpdateCard() {
  const [force, setForce] = useState(0);
  const q = useQuery({
    queryKey: ["self-update", force],
    queryFn: () => api("/admin/system/update" + (force ? "?force=1" : "")) as Promise<View>,
    refetchInterval: (query) => (query.state.data?.status && !query.state.data.status.finished_at ? 1500 : false),
    retry: 30,
    retryDelay: 2000,
  });
  const [target, setTarget] = useState<{ action: "update" | "rollback"; version?: string } | null>(null);
  const toast = useToast();
  const st = q.data?.status;
  // 安装时服务会重启：重启回来后版本变了就提示刷新页面换到新控制台。
  const [startedFrom, setStartedFrom] = useState("");
  useEffect(() => {
    if (startedFrom && q.data && q.data.current !== startedFrom && st?.state === "done") {
      toast(`已更新到 ${q.data.current}`, { tone: "ok", description: "页面将重新加载以使用新版本的控制台。" });
      setTimeout(() => window.location.reload(), 1500);
    }
  }, [q.data?.current, st?.state, startedFrom]);
  useConsoleContext({ notices: [q.data ? `当前版本 ${q.data.current}${q.data.has_update && q.data.latest ? `，可更新到 ${q.data.latest.version}` : ""}` : "版本未知", ...(st && !st.finished_at ? [`正在${st.action === "rollback" ? "切回" : "更新"}：${st.state}`] : []), ...(q.data?.check_error ? ["无法读取 GitHub 发布：" + q.data.check_error] : [])] });
  return (
    <Section title="版本与更新" description="从 GitHub Release 更新：由 root 运行的更新服务自己下载、按官方摘要校验、安装，启动失败自动切回上一版本。Gate 随后从中心自更新到同一版本。">
      {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <div className="stack">
          <div className="flex flex-wrap items-center gap-2">
            <span>当前版本 <code>{q.data.current}</code></span>
            {q.data.latest && (q.data.has_update ? <Badge variant="soft" tone="warn">有新版本 {q.data.latest.version}</Badge> : <Badge variant="soft" tone="ok">已是最新</Badge>)}
            <Button className="ml-auto" size="sm" variant="ghost" onClick={() => setForce(Date.now())}>重新检查</Button>
          </div>
          {q.data.check_error && <Notice tone="warn">无法读取 GitHub 发布：{q.data.check_error}</Notice>}
          {q.data.blocker && <Notice tone="info">{q.data.blocker}。</Notice>}
          <UpdateStatus st={st} />
          {q.data.latest && q.data.has_update && (
            <Collapsible variant="card" defaultOpen title={<span>{q.data.latest.version} 更新说明 <small className="text-[var(--mg-ink-3)]">{date(q.data.latest.published_at)}</small></span>}>
              <CodeBlock className="block max-h-64 overflow-auto whitespace-pre-wrap text-xs">{q.data.latest.notes || "（没有说明）"}</CodeBlock>
            </Collapsible>
          )}
          <div className="actions">
            {q.data.latest && q.data.has_update && <Button disabled={!!q.data.blocker || (!!st && !st.finished_at)} onClick={() => setTarget({ action: "update", version: q.data.latest!.version })}>更新到 {q.data.latest.version}</Button>}
            {q.data.previous && <Button variant="outline" disabled={!!q.data.blocker || (!!st && !st.finished_at)} onClick={() => setTarget({ action: "rollback" })}>切回上一版本 {q.data.previous}</Button>}
            {(q.data.older ?? []).map((r) => r.version !== q.data!.previous && <Button key={r.version} variant="ghost" disabled={!!q.data!.blocker || (!!st && !st.finished_at)} onClick={() => setTarget({ action: "update", version: r.version })}>重装 {r.version}</Button>)}
          </div>
        </div>
      )}
      {target && q.data && <ConfirmUpdate target={target} current={q.data.current} previous={q.data.previous} close={() => setTarget(null)} started={() => { setStartedFrom(q.data!.current); setTarget(null); void q.refetch(); }} />}
    </Section>
  );
}

// UpdateStatus：最近一次更新或切回。与下载任务一样是固定行数（状态、进度条、一行说明、日志），
// 下载、安装、完成、失败之间只换文字；还没有更新过时显示占位行，开始更新后下面的内容不被顶开。
function UpdateStatus({ st }: { st?: Status }) {
  if (!st) return <div className="flex min-h-9 items-center rounded-[var(--mg-radius)] border border-dashed border-[var(--mg-line)] px-3 text-[12.5px] text-[var(--mg-ink-3)]">还没有在这里更新过；更新的进度与日志会显示在这里。</div>;
  const p = st.progress;
  const pct = st.state === "done" ? 100 : p && p.total > 0 ? Math.round((p.done / p.total) * 100) : null;
  const label = { downloading: "正在下载", installing: "正在安装", done: "完成", failed: "失败" }[st.state];
  const line =
    st.state === "failed" ? st.error ?? "失败" :
    st.state === "installing" ? "服务会重启，控制台可能短暂断开，恢复后会自动刷新" :
    st.state === "done" ? `${st.finished_at ? clock(st.finished_at) + " " : ""}完成` :
    p ? `${pct !== null ? `${pct}% · ${size(p.done)} / ${size(p.total)}` : `已下载 ${size(p.done)}`}${p.speed > 0 ? ` · ${size(p.speed)}/s` : ""}${p.source ? ` · ${p.source}` : ""}` : "正在准备…";
  const logs = st.logs.map((l) => { const m = /^(\d{4}-\d\d-\d\dT\S+Z) (.*)$/s.exec(l); return m ? `${clock(m[1])} ${m[2]}` : l; });
  return (
    <div className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" aria-live="polite">
      <div className="flex min-h-6 flex-wrap items-center gap-2">
        <strong className="text-[13px]">{st.action === "rollback" ? "切回" : "更新"} {st.from} → {st.version || "上一版本"}</strong>
        <Badge variant="soft" tone={st.state === "done" ? "ok" : st.state === "failed" ? "bad" : "neutral"}>{label}</Badge>
        <small className="ml-auto text-[var(--mg-ink-3)]">{date(st.started_at)}</small>
      </div>
      <Progress value={pct} aria-label="更新进度" />
      <small className={"block truncate " + (st.state === "failed" ? "text-[var(--mg-bad-ink)]" : "text-[var(--mg-ink-3)]")} title={line}>{line}</small>
      <Collapsible title={`日志（${st.logs.length} 行）`} defaultOpen={st.state === "failed"}>
        <CodeBlock className="block h-56 overflow-auto whitespace-pre-wrap text-xs">{logs.join("\n") || "（还没有日志）"}</CodeBlock>
      </Collapsible>
    </div>
  );
}

function ConfirmUpdate({ target, current, previous, close, started }: { target: { action: "update" | "rollback"; version?: string }; current: string; previous: string; close: () => void; started: () => void }) {
  const [re, setRe] = useState(emptyReauth);
  const to = target.action === "rollback" ? previous : target.version!;
  const go = useReauthAction(() => api("/admin/system/update", "POST", { ...target, ...re }), { ok: "已提交，更新服务正在执行", onSuccess: started });
  useConsoleContext({ dialog: { title: target.action === "rollback" ? "切回 " + to : "更新到 " + to }, form: [field("password", "当前密码", re.password, true)] });
  return (
    <Dialog open onOpenChange={(o) => !o && !go.busy && close()}>
      <DialogContent>
        <DialogTitle>{target.action === "rollback" ? `切回 ${to}？` : `更新到 ${to}？`}</DialogTitle>
        <DialogDescription>
          当前 {current}。安装时服务会重启，控制台短暂不可用；新版本启动失败会自动切回 {current}。Gate 之后从中心自更新到同一版本。
        </DialogDescription>
        <Form busy={go.busy} errors={go.errors} onSubmit={() => void go.submit()}>
          <Reauth value={re} onChange={setRe} />
          <DialogFooter error={go.error}>
            <Button type="button" variant="ghost" disabled={go.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={go.busy} busy={go.busy} busyText="正在提交…">{target.action === "rollback" ? "切回" : "更新"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
