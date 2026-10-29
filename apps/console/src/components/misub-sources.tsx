// 订阅源：机场订阅地址。中心拉取时只连公网地址；保护性缓存在上游失败或节点骤减时保留上次结果。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Meter, Notice, SectionHeader, SkeletonTable, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea, useToast, useConfirm } from "@weir/ui";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { bytes, date } from "../lib/models";
import type { Source } from "../lib/misub";
import { OperatorEditor, type Operator } from "./operator-editor";
import { State } from "./feedback";
import { dataObject } from "./object-link";
import { field, useConsoleContext } from "../lib/console-context";

export function useSources() {
  return useQuery({ queryKey: ["misub-sources"], queryFn: () => api("/admin/misub/sources") as Promise<{ items: Source[]; traffic: { used: number; total: number; sources: number } }> });
}

export function SourcesPanel() {
  const confirm = useConfirm();
  const toast = useToast();
  const q = useSources();
  const [edit, setEdit] = useState<Source | "new" | null>(null);
  const [nodes, setNodes] = useState<{ name: string; list: string[]; error: string } | null>(null);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  // 刷新成功后直接弹出本次拉取的节点列表，不再额外提示
  const refresh = (s: Source) =>
    run(s.id, async () => {
      const r = await api(`/admin/misub/sources/${s.id}/refresh`, "POST", {});
      setNodes({ name: s.name || s.url, list: r.nodes, error: r.result?.error ?? "" });
    });
  async function remove(s: Source) {
    if (!(await confirm({ title: `删除订阅源「${s.name || s.url}」？`, description: "订阅组中的引用会一并移除。", confirmText: "删除", tone: "danger" }))) return;
    await run(s.id, () => api("/admin/misub/sources/" + s.id, "DELETE"), "订阅源已删除");
  }
  useConsoleContext({ tab: "订阅源", list: { total: q.data?.items.length ?? 0, items: (q.data?.items ?? []).slice(0, 20).map((s) => ({ type: "source", name: s.name, id: s.id })) } });
  return (
    <section className="stack">
      <SectionHeader title="订阅源" description="机场订阅地址；节点数与用量来自最近一次拉取，没有报告时显示「未知」。" actions={<Button size="sm" onClick={() => setEdit("new")}><Plus />添加订阅源</Button>} />
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<SkeletonTable card={false} columns={["名称", "节点", "用量", "最近拉取", ""]} rows={4} lines={2} />}
        />
      ) : q.data.items.length === 0 ? (
        <State empty="还没有订阅源" />
      ) : (
        <>
          {q.data.traffic.sources > 0 && (
            <div className="stack">
              <small>上游流量合计（{q.data.traffic.sources} 个源报告）：{bytes(q.data.traffic.used)} / {bytes(q.data.traffic.total)}</small>
              <Meter label="上游流量合计" value={q.data.traffic.used} max={q.data.traffic.total} />
            </div>
          )}
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>用量</TableHead>
                  <TableHead>最近拉取</TableHead>
                  <TableHead><span className="sr-only">操作</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {q.data.items.map((s) => {
                  const ui = s.user_info;
                  return (
                    <TableRow key={s.id} {...dataObject({ type: "source", id: s.id })}>
                      <TableCell>
                        <strong>{s.name || s.title || "未命名"}</strong> {!s.enabled && <Badge variant="soft" tone="warn">停用</Badge>}
                        <small className="block max-w-72 truncate text-[var(--mg-ink-3)]" title={s.url}>{s.url}</small>
                        {s.last_error && <small className="block text-[var(--mg-bad-ink)]">{s.last_error}</small>}
                      </TableCell>
                      <TableCell>{s.fetched_at ? s.node_count : "未知"}</TableCell>
                      <TableCell className="whitespace-nowrap">{ui?.total ? `${bytes((ui.upload ?? 0) + (ui.download ?? 0))} / ${bytes(ui.total)}` : "未知"}{ui?.expire ? <small className="block">到期 {date(ui.expire)}</small> : null}</TableCell>
                      <TableCell className="whitespace-nowrap">{date(s.fetched_at ?? undefined)}</TableCell>
                      <TableCell>
                        <span className="flex flex-nowrap gap-1.5">
                          <Button size="sm" variant="outline" disabled={isBusy(s.id)} onClick={() => void refresh(s)}><RefreshCw />刷新</Button>
                          <Button size="sm" variant="outline" onClick={() => setEdit(s)}>编辑</Button>
                          <Button size="icon-sm" variant="destructive" aria-label={"删除 " + (s.name || s.url)} disabled={isBusy(s.id)} onClick={() => void remove(s)}><Trash2 /></Button>
                        </span>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </>
      )}
      {edit && <SourceForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      <Dialog open={!!nodes} onOpenChange={(o) => !o && setNodes(null)}>
        <DialogContent size="lg">
          <DialogTitle>{nodes?.name} · {nodes?.list.length ?? 0} 个节点</DialogTitle>
          <DialogDescription>{nodes?.error ? `本次拉取：${nodes.error}（已按保护性缓存处理）` : "本次拉取并处理后的节点。"}</DialogDescription>
          <CodeBlock className="block max-h-96 overflow-auto whitespace-pre text-xs">{nodes?.list.join("\n") || "（空）"}</CodeBlock>
        </DialogContent>
      </Dialog>
    </section>
  );
}

const flags = [
  ["enabled", "启用"],
  ["node_cache", "保护性缓存"],
  ["plus_as_space", "名称中的 + 视为空格"],
  ["exclude_traffic", "不计入上游流量合计"],
] as const;

function SourceForm({ value, close, saved }: { value: Source | "new"; close: () => void; saved: () => void }) {
  const s = value === "new" ? undefined : value;
  const [form, setForm] = useState({
    name: s?.name ?? "", url: s?.url ?? "", user_agent: s?.user_agent ?? "", filter_include: s?.filter_include ?? "", filter_exclude: s?.filter_exclude ?? "",
    website: s?.website ?? "", remark: s?.remark ?? "", enabled: s?.enabled ?? true, node_cache: s?.node_cache ?? true, plus_as_space: s?.plus_as_space ?? false, exclude_traffic: s?.exclude_traffic ?? false,
  });
  const [ops, setOps] = useState<Operator[]>(s?.operators ?? []);
  const set = (patch: Partial<typeof form>) => setForm({ ...form, ...patch });
  const save = useFormAction(
    () => {
      const body = { ...form, operators: ops };
      return s ? api("/admin/misub/sources/" + s.id, "PATCH", body) : api("/admin/misub/sources", "POST", body);
    },
    {
      fields: { invalid_source_url: "url", source_url_credentials: "url", source_url_private: "url", invalid_user_agent: "user_agent" },
      onSuccess: saved,
    },
  );
  const text = (key: "name" | "url" | "user_agent" | "website" | "remark", label: string, hint?: string) => (
    <Field label={label} name={key} hint={hint}>
      <Input type={key === "url" ? "url" : "text"} required={key === "url"} value={form[key]} onChange={(e) => set({ [key]: e.target.value })} />
    </Field>
  );
  useConsoleContext({ dialog: { title: s ? "编辑订阅源" : "添加订阅源" }, object: s ? { type: "source", name: s.name, id: s.id } : undefined, form: Object.entries(form).map(([k, v]) => field(k, k, typeof v === "object" ? JSON.stringify(v) : v, k === "url")) });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>{s ? "编辑订阅源" : "添加订阅源"}</DialogTitle>
        <DialogDescription>更改地址、UA 或「+ 视为空格」后会清除旧缓存与用量记录；网站和备注只作记录，中心不会访问。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            {text("name", "名称", "节点会加上这个前缀（可在设置中关闭）。")}
            {text("url", "订阅地址")}
            {text("user_agent", "自定义 User-Agent（可选）", "留空时按客户端类型自动选择。")}
            {text("website", "机场网站（可选）")}
          </FormGrid>
          <Field label="只保留（每行一条，支持正则）"><Textarea rows={2} value={form.filter_include} onChange={(e) => set({ filter_include: e.target.value })} /></Field>
          <Field label="排除（每行一条，支持正则）"><Textarea rows={2} value={form.filter_exclude} onChange={(e) => set({ filter_exclude: e.target.value })} /></Field>
          {text("remark", "备注（可选）")}
          <FormGrid>
            {flags.map(([k, label]) => (
              <SwitchField key={k} label={label} checked={form[k]} onCheckedChange={(v) => set({ [k]: v })} />
            ))}
          </FormGrid>
          <Collapsible title={`本源的处理链（${ops.length} 步）`}>
            <OperatorEditor value={ops} change={setOps} />
          </Collapsible>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
