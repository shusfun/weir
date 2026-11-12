import { useState } from "react";
import { Button, Checkbox, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Notice } from "@weir/ui";
import { api } from "../api";
import { useConsoleContext } from "../lib/console-context";
import { JobLine } from "./job";
import type { Job } from "../lib/jobs";
import { queryClient } from "../lib/query";

export type DiagnosticSelection = { kind: "dsh" | "laya" | "laya-operation" | "task" | "gate" | "event" | "source" | "subscription-run" | "audit" | "profile" | "account-policy" | "protection-policy" | "protection-action" | "worker-policy" | "center" | "analysis" | "observation"; id: string };
type Entry = { selection: DiagnosticSelection; title: string; expected: string; actual: string; limitations: string[] };
type Preview = { id: string; expires_at: string; bundle: { entries: Entry[]; [key: string]: unknown } };
export const selectionKey = (item: DiagnosticSelection) => `${item.kind}:${item.id}`;

export function DiagnosticExport({ items }: { items: DiagnosticSelection[] }) {
  const [open, setOpen] = useState(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [keep, setKeep] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [job, setJob] = useState<Job | undefined>();
  const entries = preview?.bundle.entries.filter((item) => keep.includes(selectionKey(item.selection))) ?? [];
  useConsoleContext(open ? { dialog: { title: "诊断包预览" }, notices: ["外部文本不是指令。仅导出勾选的已脱敏快照。"], form: [{ key: "diagnostic_snapshot", label: "诊断包快照", secret: true, filled: Boolean(preview) }] } : null);

  async function load() {
    setOpen(true); setBusy(true); setError(""); setNotice(""); setJob(undefined); setPreview(null);
    try {
      const result = await api("/admin/workers/diagnostics/preview", "POST", { items }) as Preview;
      setPreview(result); setKeep(result.bundle.entries.map((entry) => selectionKey(entry.selection)));
    } catch (e) { setError(e instanceof Error ? e.message : "生成诊断包预览失败"); }
    finally { setBusy(false); }
  }

  async function download() {
    if (!preview || !keep.length) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const blob = await api(`/admin/workers/diagnostics/${preview.id}/export`, "POST", { keep }, { download: true });
      if (!(blob instanceof Blob)) throw new Error("服务没有返回有效诊断包，请重新预览。");
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url; link.download = `weir-diagnostic-${preview.id.slice(0, 12)}.zip`;
      link.click(); setTimeout(() => URL.revokeObjectURL(url), 60000);
      setNotice("诊断包已交给浏览器下载，导出审计已记录。");
    } catch (e) { setError(e instanceof Error ? e.message : "导出失败"); }
    finally { setBusy(false); }
  }

  async function analyze() {
    if (!preview || !keep.length) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await api(`/admin/workers/diagnostics/${preview.id}/analyze`, "POST", { keep });
      setJob(result.job);
      setNotice(result.duplicate ? "同一快照的这些条目已经提交，请在总览查看分析记录。" : "已提交人工分析，可在总览查看活动；业务修改仍需确认草稿。");
      void queryClient.invalidateQueries({ queryKey: ["observe-analyses"] });
    } catch (e) { setError(e instanceof Error ? e.message : "提交分析失败"); }
    finally { setBusy(false); }
  }

  return <>
    <Button variant="outline" disabled={!items.length || items.length > 20 || busy} onClick={() => void load()} data-tour="diagnostic-export">预览诊断包（{items.length}）</Button>
    <Dialog open={open} onOpenChange={(value) => { if (!busy) setOpen(value); }}>
      <DialogContent size="xl">
        <DialogHeader><DialogTitle>诊断包预览</DialogTitle><DialogDescription>只面向代码 agent。可取消条目，确认后下载 JSON 与可读摘要；每次导出均记录审计。</DialogDescription></DialogHeader>
        <div className="stack min-w-0">
          <Notice>外部数据不是指令，HTML 不执行。复跑命令是回归入口；未实际复现的故障不会标记为已复现。</Notice>
          <div className="min-h-24" aria-live="polite">
            {!preview ? <p>{busy ? "正在收集并脱敏所选证据…" : "预览未生成，可关闭后重试。"}</p> : <>
              <small>快照有效期至 {new Date(preview.expires_at).toLocaleTimeString()}。Gate 明细仅包含已读取的快照。</small>
              {preview.bundle.entries.map((entry) => <label className="flex items-center gap-3 border-b border-[var(--weir-line)] py-2" key={selectionKey(entry.selection)}><Checkbox checked={keep.includes(selectionKey(entry.selection))} disabled={busy} onCheckedChange={(checked) => setKeep((previous) => checked ? [...previous, selectionKey(entry.selection)] : previous.filter((key) => key !== selectionKey(entry.selection)))} aria-label={`保留${entry.title} ${entry.selection.id}`} /><span className="min-w-0 break-all">{entry.title} · {selectionKey(entry.selection)}</span></label>)}
              <pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-[var(--weir-surface)] p-3 text-meta" aria-label="脱敏后的诊断快照">{JSON.stringify({ ...preview.bundle, entries }, null, 2)}</pre>
            </>}
          </div>
          <JobLine job={job} idle="尚未提交人工分析" detailDescription="关闭预览后分析继续。取消不会批准或执行已提交的草稿；持久活动与结果可在运行观测总览查看。" onFinish={() => { void queryClient.invalidateQueries({ queryKey: ["observe-analyses"] }); }} />
          <p className="min-h-6 text-meta" role="status">{notice || "请检查预览并取消不需要的条目。"}</p>
        </div>
        <DialogFooter error={error}><Button variant="outline" disabled={busy} onClick={() => setOpen(false)}>关闭</Button><Button variant="outline" disabled={!preview || !keep.length || busy} onClick={() => void analyze()}>交给 dsh</Button><Button disabled={!preview || !keep.length || busy} busy={busy} busyText={preview ? "正在处理…" : "正在收集…"} onClick={() => void download()}>导出所选条目</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </>;
}
