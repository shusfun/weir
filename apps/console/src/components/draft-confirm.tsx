// AI 草稿确认窗：dsh（抽屉对话或后台分析）提交、系统实测通过的草稿，在任何页面都由 MiGate 自己弹窗确认。
// 弹窗内容从 MiGate 接口读取，不接收 dsh 页面发来的任何消息；dsh 的说明只按纯文本显示。
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Notice, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast } from "@migate/ui";
import { api } from "../api";

interface Change {
  key: string;
  before: unknown;
  after: unknown;
}
interface FileDiff {
  name: string;
  state: "added" | "changed" | "removed" | "same";
  lines?: { op: " " | "+" | "-" | "…"; text: string }[];
}
export interface Draft {
  id: string;
  kind: "node" | "defense" | "plugin";
  plugin?: { version: string; compat: string; dsh_version: string; warnings?: string[]; files: FileDiff[] };
  source: "dsh" | "analysis";
  explain: string;
  created_at: string;
  node_id?: number;
  node_name?: string;
  changes?: Change[];
  result?: { stage?: string; latency_ms?: number; [k: string]: unknown };
  defense?: { event_id: number; action: string; minutes: number; source_ip: string; laya?: { category: string; confidence: number } };
}

const show = (v: unknown) => (v === undefined || v === null ? "（无）" : typeof v === "string" ? v : JSON.stringify(v));
const verdict: Record<string, string> = { suspicious: "可疑", normal: "正常", unknown: "未知" };
const duration = (m: number) => (m >= 1440 && m % 1440 === 0 ? `${m / 1440} 天` : m >= 60 && m % 60 === 0 ? `${m / 60} 小时` : `${m} 分钟`);

const fileState: Record<FileDiff["state"], [string, "ok" | "warn" | "bad" | "neutral"]> = {
  added: ["新增", "ok"],
  changed: ["修改", "warn"],
  removed: ["删除", "bad"],
  same: ["未变", "neutral"],
};
const lineClass: Record<string, string> = {
  "+": "bg-[var(--mg-ok-soft)] text-[var(--mg-ok-ink)]",
  "-": "bg-[var(--mg-bad-soft)] text-[var(--mg-bad-ink)]",
  "…": "text-[var(--mg-ink-3)]",
  " ": "",
};

// PluginDiff：适配插件的试启动结果与逐文件差异（插件只有声明式配置，没有脚本）。
function PluginDiff({ p }: { p: NonNullable<Draft["plugin"]> }) {
  return (
    <div className="stack">
      <Notice tone="ok">已用 dsh {p.dsh_version} 带着这份插件试启动通过；兼容范围 <code>{p.compat}</code>。</Notice>
      {(p.warnings ?? []).length > 0 && <Notice tone="warn">试启动日志里有 {p.warnings!.length} 条提醒：{p.warnings!.slice(0, 3).join("；")}</Notice>}
      {p.files.map((f) => (
        <details key={f.name} open={f.state !== "same"} className="rounded-[var(--mg-radius)] border border-[var(--mg-line)]">
          <summary className="flex cursor-pointer items-center gap-2 px-2 py-1 text-sm"><code>{f.name}</code><Badge variant="soft" tone={fileState[f.state][1]}>{fileState[f.state][0]}</Badge></summary>
          {f.lines && f.lines.length > 0 && (
            <pre className="max-h-64 overflow-auto px-2 pb-2 text-xs leading-5">
              {f.lines.map((l, i) => <div key={i} className={lineClass[l.op] ?? ""}>{(l.op === "…" ? "  " : l.op + " ") + l.text}</div>)}
            </pre>
          )}
        </details>
      ))}
    </div>
  );
}

export function DraftConfirm() {
  const toast = useToast();
  const client = useQueryClient();
  // 只在页面可见时每 4 秒查一次待确认的草稿。
  const q = useQuery({ queryKey: ["ai-drafts"], queryFn: async () => (await api("/admin/ai/drafts")).drafts as Draft[], refetchInterval: 4000, refetchIntervalInBackground: false });
  const [later, setLater] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const d = (q.data ?? []).find((x) => !later.includes(x.id));
  if (!d) return null;
  async function decide(apply: boolean) {
    if (!d) return;
    setBusy(true);
    setError("");
    try {
      if (apply) {
        await api(`/admin/ai/drafts/${d.id}/apply`, "POST", {});
        toast(d.kind === "defense" ? `已封禁 ${d.defense?.source_ip}` : d.kind === "plugin" ? "已启用适配插件，dsh 正在重启" : `草稿已生效：${d.node_name}`, { tone: "ok" });
      } else {
        await api(`/admin/ai/drafts/${d.id}`, "DELETE");
        toast("已放弃草稿", { tone: "info" });
      }
      await client.invalidateQueries();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const pending = (q.data ?? []).length;
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && setLater((l) => [...l, d.id])}>
      <DialogContent size="lg">
        <DialogTitle className="flex flex-wrap items-center gap-2">
          {d.kind === "defense" ? "确认防御草稿" : d.kind === "plugin" ? `确认插件适配 · MiGate 插件 ${d.plugin?.version ?? ""}` : `确认 AI 草稿 · ${d.node_name ?? "节点"}`}
          <Badge variant="soft" tone="neutral">{d.source === "analysis" ? "dsh 后台分析" : "dsh 对话"}</Badge>
          {pending > 1 && <Badge variant="soft" tone="warn">还有 {pending - 1} 份待确认</Badge>}
        </DialogTitle>
        <DialogDescription>草稿由 dsh 提交{d.kind === "node" ? "，已在节点所在 VPS 上用真实内核实测通过" : ""}。确认之前不会生效；dsh 无法替你确认。</DialogDescription>
        <div className="stack">
          <div className="stack">
            <small className="text-[var(--mg-ink-3)]">dsh 的说明</small>
            <p className="whitespace-pre-wrap break-words text-sm">{d.explain}</p>
          </div>
          {d.kind === "node" ? (
            <>
              <Notice tone="ok">实测通过{d.result?.stage ? `（${d.result.stage}` + (typeof d.result.latency_ms === "number" ? `，${d.result.latency_ms} ms）` : "）") : ""}。</Notice>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader><TableRow><TableHead>字段</TableHead><TableHead>现在</TableHead><TableHead>草稿</TableHead></TableRow></TableHeader>
                  <TableBody>
                    {(d.changes ?? []).map((c) => (
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
          ) : d.defense && (
            <Notice tone="warn">
              封禁来源 <code>{d.defense.source_ip}</code> {duration(d.defense.minutes)}（安全事件 #{d.defense.event_id}）。
              {d.defense.laya ? ` Laya 判断：${verdict[d.defense.laya.category] ?? d.defense.laya.category}（置信度 ${Math.round(d.defense.laya.confidence * 100)}%）。` : " Laya 判断：未知。"}
              到期自动解封，也可以在「系统设置 → 安全防御」里提前解封。
            </Notice>
          )}
          {d.kind === "plugin" && d.plugin && <PluginDiff p={d.plugin} />}
          {error && <Notice tone="bad">{error}</Notice>}
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setLater((l) => [...l, d.id])}>稍后</Button>
          <Button type="button" variant="outline" disabled={busy} onClick={() => void decide(false)}>放弃</Button>
          <Button type="button" disabled={busy} onClick={() => void decide(true)}>{busy ? "正在执行…" : d.kind === "defense" ? "确认封禁" : d.kind === "plugin" ? "确认启用" : "确认生效"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
