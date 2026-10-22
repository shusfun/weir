// AI 调整节点：说明要求 → 模型出草稿 → VPS 实测（内核自检 + 真实连接）→ 看差异确认。模型看不到任何密码与密钥。
import { useEffect, useState } from "react";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Notice, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea } from "@migate/ui";
import { api } from "../api";
import type { NodeItem } from "../lib/core";

interface Change {
  key: string;
  before: unknown;
  after: unknown;
}
interface Attempt {
  explain: string;
  changes: Change[] | null;
  error?: string;
}
interface Draft {
  id: string;
  status: "running" | "passed" | "failed" | "error";
  error?: string;
  explain?: string;
  changes?: Change[] | null;
  attempts: Attempt[];
}

const show = (v: unknown) => (v === undefined || v === null ? "（无）" : typeof v === "string" ? v : JSON.stringify(v));

export function AIDraft({ node, close, applied }: { node: NodeItem; close: () => void; applied: () => void }) {
  const [instruction, setInstruction] = useState("");
  const [draft, setDraft] = useState<Draft | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!draft || draft.status !== "running") return;
    const t = setTimeout(async () => {
      try {
        setDraft(await api("/admin/ai/drafts/" + draft.id));
      } catch (e) {
        setError(String(e));
      }
    }, 1500);
    return () => clearTimeout(t);
  }, [draft]);
  async function start() {
    setBusy(true);
    setError("");
    try {
      setDraft(await api("/admin/ai/drafts", "POST", { node_id: node.id, instruction: instruction.trim() }));
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function apply() {
    if (!draft) return;
    setBusy(true);
    setError("");
    try {
      await api(`/admin/ai/drafts/${draft.id}/apply`, "POST", {});
      applied();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const running = draft?.status === "running";
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>AI 调整 · {node.name}</DialogTitle>
        <DialogDescription>模型只给出草稿；VPS 实测通过、你确认差异后才会生效。密码、UUID 与密钥不会发送给模型。</DialogDescription>
        {!draft ? (
          <form className="stack" onSubmit={(e) => { e.preventDefault(); void start(); }}>
            <Field label="要求" htmlFor="ai-instruction" hint="例如：内核升级后节点连不上了，帮我修；或：伪装目标换一个更稳定的站点。">
              <Textarea id="ai-instruction" required rows={3} maxLength={2000} value={instruction} onChange={(e) => setInstruction(e.target.value)} />
            </Field>
            {error && <Notice tone="bad">{error}</Notice>}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
              <Button type="submit" disabled={busy || !instruction.trim()}>{busy ? "正在提交…" : "生成草稿"}</Button>
            </DialogFooter>
          </form>
        ) : (
          <div className="stack">
            {draft.attempts.map((a, i) => (
              <div key={i} className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
                <strong className="flex items-center gap-2">第 {i + 1} 轮 <Badge variant="soft" tone={a.error ? "bad" : "ok"}>{a.error ? "未通过" : "实测通过"}</Badge></strong>
                {a.explain && <small>{a.explain}</small>}
                {a.error && <small className="text-[var(--mg-bad-ink)]">{a.error}</small>}
              </div>
            ))}
            {running && <Notice role="status">正在生成并实测草稿（每轮约 10–40 秒，最多 3 轮）…</Notice>}
            {draft.status === "error" && <Notice tone="bad">模型调用失败：{draft.error}。节点保持原样。</Notice>}
            {draft.status === "failed" && <Notice tone="bad">{draft.error}</Notice>}
            {draft.status === "passed" && (
              <>
                <Notice tone="ok">实测通过：{draft.explain}</Notice>
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader><TableRow><TableHead>字段</TableHead><TableHead>现在</TableHead><TableHead>草稿</TableHead></TableRow></TableHeader>
                    <TableBody>
                      {(draft.changes ?? []).map((c) => (
                        <TableRow key={c.key}>
                          <TableCell className="font-mono text-xs">{c.key}</TableCell>
                          <TableCell className="break-all text-xs">{show(c.before)}</TableCell>
                          <TableCell className="break-all text-xs">{show(c.after)}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </>
            )}
            {error && <Notice tone="bad">{error}</Notice>}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={close}>{draft.status === "passed" ? "放弃" : "关闭"}</Button>
              {draft.status !== "running" && draft.status !== "passed" && <Button variant="outline" onClick={() => setDraft(null)}>重新描述</Button>}
              {draft.status === "passed" && <Button disabled={busy} onClick={() => void apply()}>{busy ? "正在应用…" : "确认生效"}</Button>}
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
