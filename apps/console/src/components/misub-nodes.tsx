// 手动节点：按分组展示；可批量导入（链接 / Base64 / Clash YAML / sing-box JSON），分组可重命名或解散（节点保留）。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Label, Notice, SectionHeader, SkeletonList, Switch, Textarea, useToast } from "@migate/ui";
import { Plus, Trash2, Upload } from "lucide-react";
import { api } from "../api";
import type { ManualNode } from "../lib/misub";
import { State } from "./feedback";

export function useManualNodes() {
  return useQuery({ queryKey: ["misub-nodes"], queryFn: () => api("/admin/misub/nodes") as Promise<{ items: ManualNode[]; groups: Record<string, number> }> });
}

const protocolOf = (url: string) => url.split("://")[0]?.toLowerCase() || "?";

export function NodesPanel() {
  const toast = useToast();
  const q = useManualNodes();
  const [edit, setEdit] = useState<ManualNode | "new" | null>(null);
  const [importing, setImporting] = useState(false);
  const [busy, setBusy] = useState(0);
  async function run(id: number, fn: () => Promise<unknown>, ok: string) {
    setBusy(id);
    try {
      await fn();
      toast(ok, { tone: "ok" });
      await q.refetch();
    } catch (e) {
      toast("操作未完成", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
  }
  function renameGroup(from: string) {
    const to = window.prompt(`重命名分组「${from || "未分组"}」（留空表示解散，节点保留）`, from);
    if (to === null) return;
    void run(-1, () => api("/admin/misub/nodes/groups/rename", "POST", { from, to: to.trim() }), to.trim() ? "分组已重命名" : "分组已解散");
  }
  const groups = q.data ? Object.keys(q.data.groups).sort((a, b) => (a === "" ? 1 : b === "" ? -1 : a.localeCompare(b, "zh-CN"))) : [];
  return (
    <section className="stack">
      <SectionHeader title="手动节点" description="单独添加的节点链接，可加入任意订阅组。" actions={<>
        <Button size="sm" variant="outline" onClick={() => setImporting(true)}><Upload />批量导入</Button>
        <Button size="sm" onClick={() => setEdit("new")}><Plus />添加节点</Button>
      </>} />
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<SkeletonList rows={3} lines={2} className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" rowClassName="flex items-center justify-between gap-2" />}
        />
      ) : q.data.items.length === 0 ? (
        <State empty="还没有手动节点" />
      ) : (
        groups.map((g) => (
          <div key={g || "_"} className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
            <div className="flex items-center justify-between gap-2">
              <strong>{g || "未分组"} <small className="text-[var(--mg-ink-3)]">（{q.data.groups[g]}）</small></strong>
              {g && <Button size="sm" variant="outline" disabled={busy === -1} onClick={() => renameGroup(g)}>重命名 / 解散</Button>}
            </div>
            <ul className="stack">
              {q.data.items.filter((n) => n.group === g).map((n) => (
                <li key={n.id} className="flex flex-wrap items-center justify-between gap-2">
                  <span className="min-w-0">
                    <Badge variant="soft" tone="neutral">{protocolOf(n.url)}</Badge> {n.name || decodeURIComponent(n.url.split("#")[1] ?? "") || "未命名"}
                    {!n.enabled && <Badge variant="soft" tone="warn">停用</Badge>}
                  </span>
                  <span className="flex flex-nowrap gap-1.5">
                    <Switch aria-label={"启用 " + (n.name || n.id)} checked={n.enabled} disabled={busy === n.id} onCheckedChange={(v) => run(n.id, () => api("/admin/misub/nodes/" + n.id, "PATCH", { enabled: v }), v ? "已启用" : "已停用")} />
                    <Button size="sm" variant="outline" onClick={() => setEdit(n)}>编辑</Button>
                    <Button size="icon-sm" variant="destructive" aria-label={"删除 " + (n.name || n.id)} disabled={busy === n.id} onClick={() => window.confirm("删除这个节点？") && run(n.id, () => api("/admin/misub/nodes/" + n.id, "DELETE"), "已删除")}><Trash2 /></Button>
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ))
      )}
      {edit && <NodeForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {importing && <ImportForm close={() => setImporting(false)} saved={(n) => { setImporting(false); toast(`已导入 ${n} 个节点`, { tone: "ok" }); void q.refetch(); }} />}
    </section>
  );
}

function NodeForm({ value, close, saved }: { value: ManualNode | "new"; close: () => void; saved: () => void }) {
  const n = value === "new" ? undefined : value;
  const [name, setName] = useState(n?.name ?? "");
  const [url, setUrl] = useState(n?.url ?? "");
  const [group, setGroup] = useState(n?.group ?? "");
  const [plus, setPlus] = useState(n?.plus_as_space ?? false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const body = { name: name.trim(), url: url.trim(), group: group.trim(), plus_as_space: plus };
      await (n ? api("/admin/misub/nodes/" + n.id, "PATCH", body) : api("/admin/misub/nodes", "POST", body));
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{n ? "编辑节点" : "添加节点"}</DialogTitle>
        <DialogDescription>粘贴一条节点链接（vless://、trojan://、ss:// 等）；订阅地址请添加为订阅源。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <Field label="节点链接" htmlFor="node-url"><Textarea id="node-url" required rows={3} className="font-mono text-xs" value={url} onChange={(e) => setUrl(e.target.value)} /></Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="显示名称（可选）" htmlFor="node-name" hint="留空使用链接里的名称。"><Input id="node-name" value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <Field label="分组（可选）" htmlFor="node-group"><Input id="node-group" value={group} onChange={(e) => setGroup(e.target.value)} /></Field>
          </div>
          <Label className="actions"><Switch aria-label="名称中的 + 视为空格" checked={plus} onCheckedChange={setPlus} />名称中的 + 视为空格</Label>
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ImportForm({ close, saved }: { close: () => void; saved: (n: number) => void }) {
  const [content, setContent] = useState("");
  const [group, setGroup] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const r = await api("/admin/misub/nodes/import", "POST", { content, group: group.trim() });
      saved(r.imported);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>批量导入节点</DialogTitle>
        <DialogDescription>支持多行节点链接、Base64 订阅内容、Clash YAML 与 sing-box JSON；一次最多 2000 个。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <Field label="内容" htmlFor="import-content"><Textarea id="import-content" required rows={10} className="font-mono text-xs" value={content} onChange={(e) => setContent(e.target.value)} /></Field>
          <Field label="放入分组（可选）" htmlFor="import-group"><Input id="import-group" value={group} onChange={(e) => setGroup(e.target.value)} /></Field>
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy || !content.trim()}>{busy ? "正在导入…" : "导入"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
