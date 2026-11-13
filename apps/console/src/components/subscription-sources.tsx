import { useFormGuard } from "../lib/dirty";
// 订阅源：机场订阅地址。中心拉取时只连公网地址；保护性缓存在上游失败或节点骤减时保留上次结果。
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "../lib/query";
import { Badge, Button, Card, CardContent, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Meter, Notice, PageState, Readout, SectionHeader, SkeletonTable, SwitchField, TrafficUsage, trafficReading, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea, useToast, useConfirm } from "@weir/ui";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "./wait";
import { bytes, date } from "../lib/models";
import type { Source } from "../lib/subscriptions";
import { OperatorEditor, type Operator } from "./operator-editor";
import { State } from "./feedback";
import { dataObject } from "./object-link";
import { field, useConsoleContext } from "../lib/console-context";

export function useSources() {
  return useQuery({ queryKey: ["subscription-sources"], queryFn: () => api("/admin/subscriptions/sources") as Promise<{ items: Source[]; traffic: { used: number; total: number; sources: number } }> });
}

export function SourcesPanel() {
  const confirm = useConfirm();
  const toast = useToast();
  const q = useSources();
  const [edit, setEdit] = useState<Source | "new" | null>(null);
  const [nodes, setNodes] = useState<{ name: string; list: string[]; error: string } | null>(null);
  const { run, isBusy, waitFor } = useRowAction<string>({ refetch: q.refetch });
  // 刷新成功后直接弹出本次拉取的节点列表，不再额外提示
  const refresh = (s: Source) =>
    run("refresh:" + s.id, async () => {
      const r = await api(`/admin/subscriptions/sources/${s.id}/refresh`, "POST", {});
      setNodes({ name: s.name || s.url, list: r.nodes, error: r.result?.error ?? "" });
    }, undefined, { wait: { label: "正在拉取订阅源" } });
  async function remove(s: Source) {
    if (!(await confirm({ title: `删除订阅源「${s.name || s.url}」？`, description: "订阅组中的引用会一并移除。", confirmText: "删除", tone: "danger" }))) return;
    await run("del:" + s.id, () => api("/admin/subscriptions/sources/" + s.id, "DELETE"), "订阅源已删除");
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
        <State empty="还没有订阅源" detail="想把机场订阅合并进来时才需要。填上订阅地址后，可在订阅组里处理并分给用户。" />
      ) : (
        <>
          {(
            <div className="stack">
              <small>上游流量合计 · {q.data.traffic.sources} / {q.data.items.filter((s) => s.enabled && !s.exclude_traffic).length} 个来源报告完整流量；失败来源保留上次成功值</small>
              <TrafficUsage used={q.data.traffic.sources ? q.data.traffic.used : null} quota={q.data.traffic.sources ? q.data.traffic.total : null} />
            </div>
          )}
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {[...q.data.items].sort((a, b) => Number(!!b.last_error) - Number(!!a.last_error)).map((s) => {
              const ui = s.user_info;
              const used = ui?.upload != null && ui.download != null ? ui.upload + ui.download : null;
              return (
                <Card key={s.id} className={s.last_error ? "border-[var(--weir-bad)]" : undefined} {...dataObject({ type: "source", id: s.id })}>
                  <CardContent className="stack">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <strong>{s.name || s.title || "未命名"}</strong>
                        <small className="block truncate text-[var(--weir-ink-3)]" title={s.url}>{s.url || "未知"}</small>
                      </div>
                      {s.last_error ? <Badge variant="status" tone="bad">刷新失败</Badge> : !s.enabled ? <Badge variant="soft" tone="warn">停用</Badge> : null}
                    </div>
                    <TrafficUsage used={used} quota={ui?.total} />
                    <small className="text-[var(--weir-ink-3)]">上次成功更新：{s.fetched_at ? date(s.fetched_at) : "未知"}{s.last_error && s.user_info ? " · 当前为上次成功值" : ""}</small>
                    <small className="text-[var(--weir-ink-3)]">{s.fetched_at ? `${s.node_count} 个节点` : "节点数未知"}{ui?.expire ? ` · ${date(ui.expire)} 到期` : ""}</small>
                    {s.last_error && <small className="text-[var(--weir-bad-ink)]">{s.last_error}</small>}
                    {waitFor("refresh:" + s.id) && <WaitText wait={waitFor("refresh:" + s.id)} />}
                    <div className="flex flex-wrap gap-1.5">
                      <Button size="sm" disabled={isBusy()} busy={isBusy("refresh:" + s.id)} busyText="正在刷新…" onClick={() => void refresh(s)}>重新刷新</Button>
                      <Button size="sm" variant="outline" onClick={() => setEdit(s)}>编辑</Button>
                      <Button size="sm" variant="destructive" aria-label={"删除 " + (s.name || s.url)} disabled={isBusy()} busy={isBusy("del:" + s.id)} busyText="正在删除" onClick={() => void remove(s)}><Trash2 /></Button>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          </div>
        </>
      )}
      {edit && <SourceForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      <Dialog open={!!nodes} onOpenChange={(o) => !o && setNodes(null)}>
        <DialogContent size="lg">
          <DialogTitle>{nodes?.name} · {nodes?.list.length ?? 0} 个节点</DialogTitle>
          <DialogDescription>{nodes?.error ? `本次拉取：${nodes.error}（已按保护性缓存处理）` : "本次拉取并处理后的节点。"}</DialogDescription>
          <CodeBlock className="block max-h-96 overflow-auto whitespace-pre text-meta">{nodes?.list.join("\n") || "（空）"}</CodeBlock>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export function SourceDetail({ id }: { id: string }) {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const toast = useToast();
  const q = useSources();
  const [edit, setEdit] = useState(false);
  const [nodes, setNodes] = useState<{ name: string; list: string[]; error: string } | null>(null);
  const { run, isBusy, waitFor } = useRowAction<string>({ refetch: q.refetch });
  const item = q.data?.items.find((s) => String(s.id) === id);
  if (q.isPending) return <State loading />;
  if (q.error) return <State error={q.error} onRetry={() => void q.refetch()} />;
  if (!item) return <PageState kind="empty" title="没有这个订阅源" detail="它可能已被删除，或你没有权限查看。" action={<Link to="/subscriptions?tab=sources">返回订阅源</Link>} />;
  const source = item;
  const ui = source.user_info;
  const used = ui?.upload != null && ui.download != null ? ui.upload + ui.download : null;
  const refresh = () => run("refresh:" + source.id, async () => {
    const r = await api(`/admin/subscriptions/sources/${source.id}/refresh`, "POST", {});
    setNodes({ name: source.name || source.url, list: r.nodes, error: r.result?.error ?? "" });
  }, undefined, { wait: { label: "正在拉取订阅源" } });
  async function remove() {
    if (!(await confirm({ title: `删除订阅源「${source.name || source.url}」？`, description: "订阅组中的引用会一并移除。", confirmText: "删除", tone: "danger" }))) return;
    const removed = await run("del:" + source.id, () => api("/admin/subscriptions/sources/" + source.id, "DELETE"), "订阅源已删除");
    if (removed) navigate("/subscriptions?tab=sources");
  }
  return (
    <section className="stack">
      <div className="flex flex-wrap gap-1.5">
        <Button size="sm" variant="outline" disabled={isBusy()} busy={isBusy("refresh:" + source.id)} busyText="正在刷新…" onClick={() => void refresh()}><RefreshCw />刷新</Button>
        <Button size="sm" variant="outline" onClick={() => setEdit(true)}>编辑</Button>
        <Button size="sm" variant="destructive" disabled={isBusy()} busy={isBusy("del:" + source.id)} busyText="正在删除" onClick={() => void remove()}><Trash2 />删除</Button>
      </div>
      {waitFor("refresh:" + source.id) && <WaitText wait={waitFor("refresh:" + source.id)} />}
      {source.last_error ? <Notice tone="bad">{source.last_error}</Notice> : null}
      <div className="grid gap-3 sm:grid-cols-3">
        <Readout label="状态" value={source.enabled ? "启用" : "已停用"} />
        <Readout label="节点" value={source.fetched_at ? source.node_count : null} />
        <Readout label="用量" value={trafficReading({ used, quota: ui?.total }).text} caption={ui?.expire ? `到期 ${date(ui.expire)}` : "到期未知"} />
        <Readout label="上次成功更新" value={source.fetched_at ? date(source.fetched_at) : null} />
        <Readout label="订阅地址" value={source.url || null} />
        <Readout label="机场网站" value={source.website || null} />
      </div>
      <p className="text-body text-[var(--weir-ink-2)]">{source.remark || "没有备注"}</p>
      {edit && <SourceForm value={source} close={() => setEdit(false)} saved={() => { setEdit(false); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      <Dialog open={!!nodes} onOpenChange={(o) => !o && setNodes(null)}>
        <DialogContent size="lg">
          <DialogTitle>{nodes?.name} · {nodes?.list.length ?? 0} 个节点</DialogTitle>
          <DialogDescription>{nodes?.error ? `本次拉取：${nodes.error}（已按保护性缓存处理）` : "本次拉取并处理后的节点。"}</DialogDescription>
          <CodeBlock className="block max-h-96 overflow-auto whitespace-pre text-meta">{nodes?.list.join("\n") || "（空）"}</CodeBlock>
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
  const guard = useFormGuard({ form, ops }, close);
  const save = useFormAction(
    () => {
      const body = { ...form, operators: ops };
      return s ? api("/admin/subscriptions/sources/" + s.id, "PATCH", body) : api("/admin/subscriptions/sources", "POST", body);
    },
    {
      fields: { invalid_source_url: "url", source_url_credentials: "url", source_url_private: "url", invalid_user_agent: "user_agent" },
      onSuccess: () => { guard.commit(); saved(); },
    },
  );
  const text = (key: "name" | "url" | "user_agent" | "website" | "remark", label: string, hint?: string) => (
    <Field label={label} name={key} hint={hint}>
      <Input type={key === "url" ? "url" : "text"} required={key === "url"} value={form[key]} onChange={(e) => set({ [key]: e.target.value })} />
    </Field>
  );
  useConsoleContext({ dialog: { title: s ? "编辑订阅源" : "添加订阅源" }, object: s ? { type: "source", name: s.name, id: s.id } : undefined, form: Object.entries(form).map(([k, v]) => field(k, k, typeof v === "object" ? JSON.stringify(v) : v, k === "url")) });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && void guard.close()}>
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
            <Button type="button" variant="outline" disabled={save.busy} onClick={() => { if (!save.busy) void guard.close(); }}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
