// 订阅源：机场订阅地址。拉取走「指定 VPS → 默认 VPS → 其他在线 VPS → 中心直连」，拉到后在同一台 VPS 上检测节点。
// 每一行先回答「这个源现在能用多少、下次什么时候刷新、上次是谁拉的」，内部处理参数收进「高级」。
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useFormGuard } from "../lib/dirty";
import { useSources } from "../lib/subscription-sources";
import { Badge, Button, Card, CardContent, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, PageState, Readout, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonTable, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea, TrafficUsage, trafficReading, useConfirm, useToast } from "@weir/ui";
import { Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "./wait";
import { date } from "../lib/models";
import type { InspectedNode, Source, SourceHealth, SourceInspection } from "../lib/subscriptions";
import { OperatorEditor, type Operator } from "./operator-editor";
import { State } from "./feedback";
import { dataObject, ObjectLink, useDialogParam } from "./object-link";
import { field, useConsoleContext } from "../lib/console-context";
import { objectStatus } from "../lib/core";
import { refreshChoices, refreshText, SourceGateSelect } from "./source-gate";

// 可用数一句话：未检测的单独说，不算进不可用
function healthText(h: SourceHealth | null | undefined, nodeCount: number, fetched: boolean) {
  if (!h) return fetched ? `${nodeCount} 个节点 · 未检测` : "还没有拉取";
  const parts = [`${h.alive}/${h.total} 可用`];
  if (h.unchecked) parts.push(`${h.unchecked} 个未检测`);
  if (h.checked_at) parts.push(`${date(h.checked_at)} 检测`);
  return parts.join(" · ");
}

// 地区小标签：可用数最多的前几个
function topRegions(h: SourceHealth | null | undefined, limit = 4) {
  if (!h?.regions) return [];
  return Object.entries(h.regions).sort((a, b) => b[1] - a[1]).slice(0, limit).map(([region, total]) => ({ region, total, alive: h.region_alive?.[region] ?? 0 }));
}

function refreshLine(s: Source) {
  const next = s.next_refresh_at ? `下次 ${date(s.next_refresh_at)}` : s.effective_refresh_hours ? "" : "不自动刷新";
  const last = s.last_attempt_at ? `${date(s.last_attempt_at)} 由 ${s.last_location_label || "未知"} 拉取${s.last_error ? "失败" : ""}` : "";
  return [next, last].filter(Boolean).join(" · ");
}

function changeText(c: Source["change"]) {
  if (!c || !c.added && !c.removed && !c.renamed) return "";
  return ["较上次", c.added ? `+${c.added}` : "", c.removed ? `−${c.removed}` : "", c.renamed ? `改名 ${c.renamed}` : ""].filter(Boolean).join(" ");
}

function useRefresh(refetch: () => unknown) {
  const toast = useToast();
  const row = useRowAction<string>({ refetch });
  const refresh = (s: Source) => row.run("refresh:" + s.id, async () => {
    const r = await api(`/admin/subscriptions/sources/${s.id}/refresh`, "POST", {}) as { source: Source };
    const h = r.source.health;
    if (r.source.last_error) toast("刷新没有成功", { tone: "bad", description: r.source.last_error });
    else toast("已刷新", { tone: "ok", description: healthText(h, r.source.node_count, true) });
  }, undefined, { wait: { label: `正在拉取并检测「${s.name || s.url}」` } });
  return { ...row, refresh };
}

export function SourcesPanel() {
  const confirm = useConfirm();
  const q = useSources();
  const detail = useDialogParam("source", q.data?.items, q.isSuccess);
  const [edit, setEdit] = useState<Source | "new" | null>(null);
  const { refresh, run, isBusy, waitFor } = useRefresh(q.refetch);
  async function remove(s: Source) {
    if (!(await confirm({ title: `删除订阅源「${s.name || s.url}」？`, description: "订阅组中的引用会一并移除。", confirmText: "删除", tone: "danger" }))) return;
    await run("del:" + s.id, () => api("/admin/subscriptions/sources/" + s.id, "DELETE"), "订阅源已删除");
  }
  useConsoleContext({ tab: "订阅源", list: { total: q.data?.items.length ?? 0, items: (q.data?.items ?? []).slice(0, 20).map((s) => ({ type: "source", name: s.name, id: s.id })) } });
  return (
    <section className="stack">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-meta text-[var(--weir-ink-3)]">
          {q.data && q.data.items.length > 0 ? `上游流量合计 · ${q.data.traffic.sources} 个来源报告了流量` : "填上机场订阅地址，用你的 VPS 拉取并检测节点"}
        </span>
        <Button size="sm" onClick={() => setEdit("new")}><Plus />添加订阅源</Button>
      </div>
      {q.isPending || q.error ? (
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonTable card={false} columns={["名称", "可用", "用量", "刷新", ""]} rows={4} lines={2} />} />
      ) : q.data.items.length === 0 ? (
        <State empty="还没有订阅源" detail="添加后会立即用 VPS 拉取并检测节点，再到「外部订阅组」按地区、倍率挑选，分给用户。" />
      ) : (
        <>
          {q.data.traffic.sources > 0 && <TrafficUsage used={q.data.traffic.used} quota={q.data.traffic.total} />}
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {[...q.data.items].sort((a, b) => Number(!!b.last_error) - Number(!!a.last_error)).map((s) => {
              const ui = s.user_info;
              const used = ui?.upload != null && ui.download != null ? ui.upload + ui.download : null;
              const [label, tone] = objectStatus(s.object_state);
              const change = changeText(s.change);
              const wait = waitFor("refresh:" + s.id);
              return (
                <Card key={s.id} className={s.last_error ? "border-[var(--weir-bad)]" : undefined} {...dataObject({ type: "source", id: s.id })}>
                  <CardContent className="stack">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <strong><ObjectLink to={{ type: "source", id: s.id, name: s.name || s.title || "未命名" }} /></strong>
                        <small className="block text-[var(--weir-ink-2)]">{healthText(s.health, s.node_count, !!s.fetched_at)}{change ? ` · ${change}` : ""}</small>
                      </div>
                      {tone !== "ok" && <Badge variant="status" tone={tone === "bad" ? "bad" : tone === "warn" ? "warn" : "neutral"}>{label}</Badge>}
                    </div>
                    {topRegions(s.health).length > 0 && <div className="flex flex-wrap gap-1">{topRegions(s.health).map((r) => <Badge key={r.region} variant="tag">{r.region} {r.alive}/{r.total}</Badge>)}</div>}
                    <TrafficUsage used={used} quota={ui?.total} />
                    {ui?.expire ? <small className="text-[var(--weir-ink-3)]">{date(ui.expire)} 到期</small> : null}
                    <small className="text-[var(--weir-ink-3)]">{s.refreshing ? "正在拉取…" : refreshLine(s)}</small>
                    {s.last_route_reason && <small className="text-[var(--weir-warn-ink)]">{s.last_route_reason}</small>}
                    {s.last_error && <small className="text-[var(--weir-bad-ink)]">{s.last_error}</small>}
                    {wait && <WaitText wait={wait} />}
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Button size="sm" variant="outline" busy={isBusy("refresh:" + s.id)} busyText="正在刷新" onClick={() => void refresh(s)}><RefreshCw />刷新</Button>
                      <Button size="icon-sm" variant="ghost" title="编辑" aria-label={"编辑 " + (s.name || s.url)} onClick={() => setEdit(s)}><Pencil /></Button>
                      <Button size="icon-sm" variant="ghost" title="删除" aria-label={"删除 " + (s.name || s.url)} busy={isBusy("del:" + s.id)} busyText="正在删除" onClick={() => void remove(s)}><Trash2 /></Button>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          </div>
        </>
      )}
      {detail.current && <Dialog open onOpenChange={o => !o && detail.close()}><DialogContent size="xl"><DialogTitle>{detail.current.name || detail.current.title || "订阅源"}</DialogTitle><DialogDescription>{healthText(detail.current.health, detail.current.node_count, !!detail.current.fetched_at)}</DialogDescription><SourceDetail key={detail.current.id} id={String(detail.current.id)} /></DialogContent></Dialog>}
      {edit && <SourceForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); void q.refetch(); }} />}
    </section>
  );
}

// 「地区 × 倍率」统计：订阅组按这些属性挑节点，这里先让人看清这个源里有什么
function HealthBreakdown({ h }: { h: SourceHealth }) {
  const regions = Object.entries(h.regions ?? {}).sort((a, b) => b[1] - a[1]);
  const multipliers = Object.entries(h.multipliers ?? {}).sort((a, b) => (a[0] === "未标注" ? 1 : b[0] === "未标注" ? -1 : Number(a[0]) - Number(b[0])));
  return <div className="grid gap-3 md:grid-cols-2">
    <Table>
      <TableHeader><TableRow><TableHead>地区</TableHead><TableHead>可用 / 总数</TableHead></TableRow></TableHeader>
      <TableBody>{regions.map(([r, total]) => <TableRow key={r}><TableCell>{r}</TableCell><TableCell>{h.region_alive?.[r] ?? 0} / {total}</TableCell></TableRow>)}</TableBody>
    </Table>
    <Table>
      <TableHeader><TableRow><TableHead>倍率</TableHead><TableHead>节点数</TableHead></TableRow></TableHeader>
      <TableBody>{multipliers.map(([m, n]) => <TableRow key={m}><TableCell>{m === "未标注" ? m : `${m} 倍`}</TableCell><TableCell>{n}</TableCell></TableRow>)}</TableBody>
    </Table>
  </div>;
}

export function SourceDetail({ id }: { id: string }) {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const q = useSources();
  const [edit, setEdit] = useState(false);
  const { refresh, run, isBusy, waitFor } = useRefresh(q.refetch);
  const item = q.data?.items.find((s) => String(s.id) === id);
  useConsoleContext({ object: item ? { type: "source", id: item.id, name: item.name } : undefined, dialog: { title: "订阅源详情" } });
  if (q.isPending) return <State loading />;
  if (q.error) return <State error={q.error} onRetry={() => void q.refetch()} />;
  if (!item) return <PageState kind="empty" title="没有这个订阅源" detail="它可能已被删除，或你没有权限查看。" action={<Link to="/subscriptions?tab=sources">返回订阅源</Link>} />;
  const source = item;
  const ui = source.user_info;
  const used = ui?.upload != null && ui.download != null ? ui.upload + ui.download : null;
  async function remove() {
    if (!(await confirm({ title: `删除订阅源「${source.name || source.url}」？`, description: "订阅组中的引用会一并移除。", confirmText: "删除", tone: "danger" }))) return;
    const removed = await run("del:" + source.id, () => api("/admin/subscriptions/sources/" + source.id, "DELETE"), "订阅源已删除");
    if (removed) navigate("/subscriptions?tab=sources");
  }
  const wait = waitFor("refresh:" + source.id);
  return (
    <section className="stack">
      <div className="flex flex-wrap gap-1.5">
        <Button size="sm" variant="outline" busy={isBusy("refresh:" + source.id)} busyText="正在刷新" onClick={() => void refresh(source)}><RefreshCw />刷新</Button>
        <Button size="sm" variant="outline" onClick={() => setEdit(true)}>编辑</Button>
        <Button size="sm" variant="ghost" busy={isBusy("del:" + source.id)} busyText="正在删除" onClick={() => void remove()}><Trash2 />删除</Button>
      </div>
      {wait && <WaitText wait={wait} />}
      {source.last_error ? <Notice tone="bad">{source.last_error}</Notice> : source.last_route_reason ? <Notice tone="warn">{source.last_route_reason}</Notice> : null}
      <div className="grid gap-3 sm:grid-cols-3">
        <Readout label="可用节点" value={source.health ? `${source.health.alive} / ${source.health.total}` : null} caption={source.health?.reason || (source.health?.checked_at ? `${date(source.health.checked_at)} 检测` : "未检测")} />
        <Readout label="用量" value={trafficReading({ used, quota: ui?.total }).text} caption={ui?.expire ? `到期 ${date(ui.expire)}` : "到期未知"} />
        <Readout label="拉取" value={source.gate?.name ?? "自动"} caption={source.last_attempt_at ? `上次由 ${source.last_location_label || "未知"} 拉取` : "还没有拉取"} />
        <Readout label="自动刷新" value={source.effective_refresh_hours ? refreshText(source.effective_refresh_hours) : "不自动刷新"} caption={source.next_refresh_at ? `下次 ${date(source.next_refresh_at)}` : source.refresh_hours ? "" : "跟随默认设置"} />
        <Readout label="节点变化" value={changeText(source.change) || null} caption={source.change?.samples?.slice(0, 3).join("、")} />
        <Readout label="订阅地址" value={source.url || null} />
      </div>
      {source.health && source.health.total > 0 && <HealthBreakdown h={source.health} />}
      {source.remark && <p className="text-body text-[var(--weir-ink-2)]">{source.remark}</p>}
      {edit && <SourceForm value={source} close={() => setEdit(false)} saved={() => { setEdit(false); void q.refetch(); }} />}
    </section>
  );
}

const flags = [
  ["enabled", "启用"],
  ["node_cache", "拉取失败时沿用上次结果"],
  ["plus_as_space", "名称中的 + 视为空格"],
  ["exclude_traffic", "不计入上游流量合计"],
] as const;

// 试拉结果就地显示：先看清能用多少、在哪些地区、用量到期，再决定保存
function InspectionResult({ r }: { r: SourceInspection }) {
  if (!r.ok) return <Notice tone="bad">没拉到：{r.error}{r.route_reason ? `（${r.route_reason}）` : ""}</Notice>;
  const h = r.summary!;
  const ui = r.user_info;
  const used = ui?.upload != null && ui.download != null ? ui.upload + ui.download : null;
  return <div className="stack-tight">
    <div className="grid gap-3 sm:grid-cols-3">
      <Readout label="可用节点" value={`${h.alive} / ${h.total}`} caption={h.reason || (h.unchecked ? `${h.unchecked} 个未检测` : "全部已检测")} />
      <Readout label="用量" value={trafficReading({ used, quota: ui?.total }).text} caption={ui?.expire ? `到期 ${date(ui.expire)}` : "到期未知"} />
      <Readout label="拉取位置" value={r.location_label || "未知"} caption={r.route_reason || undefined} />
    </div>
    {topRegions(h, 8).length > 0 && <div className="flex flex-wrap gap-1">{topRegions(h, 8).map((x) => <Badge key={x.region} variant="tag">{x.region} {x.alive}/{x.total}</Badge>)}</div>}
    <Collapsible title={`节点列表（${r.nodes?.length ?? 0}）`}>
      <Table>
        <TableHeader><TableRow><TableHead>名称</TableHead><TableHead>地区</TableHead><TableHead>倍率</TableHead><TableHead>检测</TableHead></TableRow></TableHeader>
        <TableBody>{(r.nodes ?? []).slice(0, 300).map((n: InspectedNode) => <TableRow key={n.identity}>
          <TableCell className="break-all">{n.name || "未命名"}</TableCell><TableCell>{n.region}</TableCell><TableCell>{n.multiplier != null ? `${n.multiplier} 倍` : "未标注"}</TableCell>
          <TableCell>{n.status === "alive" ? `${n.latency_ms ?? "?"} ms` : n.status === "failed" ? <span className="text-[var(--weir-bad-ink)]">{n.why || "不可用"}</span> : <span className="text-[var(--weir-ink-3)]">未检测</span>}</TableCell>
        </TableRow>)}</TableBody>
      </Table>
    </Collapsible>
  </div>;
}

function SourceForm({ value, close, saved }: { value: Source | "new"; close: () => void; saved: () => void }) {
  const s = value === "new" ? undefined : value;
  const toast = useToast();
  const [form, setForm] = useState({
    name: s?.name ?? "", url: s?.url ?? "", user_agent: s?.user_agent ?? "", filter_include: s?.filter_include ?? "", filter_exclude: s?.filter_exclude ?? "",
    website: s?.website ?? "", remark: s?.remark ?? "", enabled: s?.enabled ?? true, node_cache: s?.node_cache ?? true, plus_as_space: s?.plus_as_space ?? false, exclude_traffic: s?.exclude_traffic ?? false,
    gate_id: s?.gate_id ?? 0, refresh_hours: s?.refresh_hours ?? 0,
  });
  const [ops, setOps] = useState<Operator[]>(s?.operators ?? []);
  const [inspection, setInspection] = useState<(SourceInspection & { for: string }) | null>(null);
  const set = (patch: Partial<typeof form>) => setForm({ ...form, ...patch });
  const guard = useFormGuard({ form, ops }, close);
  const inspectionKey = (value: typeof form) => JSON.stringify([value.url.trim(), value.user_agent.trim(), value.plus_as_space, value.gate_id]);
  const draftKey = inspectionKey(form);
  const current = inspection && inspection.for === draftKey ? inspection : null;
  const inspect = useFormAction(
    (track) => api("/admin/subscriptions/sources/inspect", "POST", { url: form.url, user_agent: form.user_agent, plus_as_space: form.plus_as_space, gate_id: form.gate_id }, track) as Promise<SourceInspection>,
    {
      fields: { invalid_source_url: "url", source_url_credentials: "url", source_url_private: "url" },
      wait: { label: "正在用 VPS 拉取并检测节点", max: 120 },
      onSuccess: (r) => {
        const gateID = r.ok && form.gate_id === 0 && r.gate_id ? r.gate_id : form.gate_id;
        setInspection({ ...r, for: inspectionKey({ ...form, gate_id: gateID }) });
        if (gateID !== form.gate_id) {
          setForm((latest) => inspectionKey(latest) === draftKey ? { ...latest, gate_id: gateID } : latest);
        }
      },
    },
  );
  const save = useFormAction(
    () => {
      const body = { ...form, operators: ops, inspection_id: current?.ok ? current.inspection_id : undefined };
      return s ? api("/admin/subscriptions/sources/" + s.id, "PATCH", body) : api("/admin/subscriptions/sources", "POST", body);
    },
    {
      fields: { invalid_source_url: "url", source_url_credentials: "url", source_url_private: "url", invalid_user_agent: "user_agent", invalid_source_gate: "gate_id", invalid_refresh_hours: "refresh_hours" },
      onSuccess: (r) => {
        guard.commit();
        toast("已保存", { tone: "ok", description: (r as Source).refreshing ? "正在后台拉取并检测节点" : undefined });
        saved();
      },
    },
  );
  const text = (key: "name" | "url" | "user_agent" | "website" | "remark", label: string, hint?: string) => (
    <Field label={label} name={key} hint={hint} required={key === "url"}>
      <Input type={key === "url" ? "url" : "text"} required={key === "url"} value={form[key]} onChange={(e) => set({ [key]: e.target.value })} />
    </Field>
  );
  useConsoleContext({ dialog: { title: s ? "编辑订阅源" : "添加订阅源" }, object: s ? { type: "source", name: s.name, id: s.id } : undefined, form: Object.entries(form).map(([k, v]) => field(k, k, typeof v === "object" ? JSON.stringify(v) : v, k === "url")) });
  const busy = save.busy || inspect.busy;
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && void guard.close()}>
      <DialogContent size="lg">
        <DialogTitle>{s ? "编辑订阅源" : "添加订阅源"}</DialogTitle>
        <DialogDescription>先试拉看看能用多少节点，再保存。</DialogDescription>
        <Form busy={busy} errors={{ ...inspect.errors, ...save.errors }} onSubmit={() => void save.submit()}>
          {text("url", "订阅地址")}
          <FormGrid>
            {text("name", "名称", "节点会加上这个前缀（可在默认设置中关闭）。")}
            <Field label="用哪台 VPS 拉取" name="gate_id" hint="这台离线或拉不到时，自动换其他在线 VPS，最后由中心直接下载。">
              <SourceGateSelect value={form.gate_id} onChange={(gate_id) => set({ gate_id })} autoLabel="自动（默认 VPS 优先）" />
            </Field>
            <Field label="自动刷新" name="refresh_hours">
              <Select value={String(form.refresh_hours)} onValueChange={(v) => set({ refresh_hours: Number(v) || 0 })}>
                <SelectTrigger><SelectValue>{form.refresh_hours ? refreshText(form.refresh_hours) : "跟随默认设置"}</SelectValue></SelectTrigger>
                <SelectContent><SelectItem value="0">跟随默认设置</SelectItem>{refreshChoices.map((h) => <SelectItem key={h} value={String(h)}>{refreshText(h)}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
          </FormGrid>
          <div className="actions">
            <Button type="button" variant="outline" disabled={!form.url.trim() || save.busy} busy={inspect.busy} busyText="正在试拉" onClick={() => void inspect.submit()}>试拉</Button>
            {inspect.wait && <WaitText wait={inspect.wait} />}
          </div>
          {inspect.error && <Notice tone="bad">{inspect.error}</Notice>}
          {current && <InspectionResult r={current} />}
          {inspection && !current && <small className="text-[var(--weir-ink-3)]">拉取参数已更改，试拉结果不再使用。</small>}
          <Collapsible title="高级">
            <div className="stack">
              <FormGrid>
                {text("user_agent", "自定义 User-Agent", "留空时按客户端类型自动选择。")}
                {text("website", "机场网站", "只作记录，中心不会访问。")}
              </FormGrid>
              <Field label="只保留（每行一条，支持正则）"><Textarea rows={2} value={form.filter_include} onChange={(e) => set({ filter_include: e.target.value })} /></Field>
              <Field label="排除（每行一条，支持正则）"><Textarea rows={2} value={form.filter_exclude} onChange={(e) => set({ filter_exclude: e.target.value })} /></Field>
              {text("remark", "备注")}
              <FormGrid>{flags.map(([k, label]) => <SwitchField key={k} label={label} checked={form[k]} onCheckedChange={(v) => set({ [k]: v })} />)}</FormGrid>
              <Collapsible title={`本源的处理链（${ops.length} 步）`}><OperatorEditor value={ops} change={setOps} /></Collapsible>
            </div>
          </Collapsible>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={busy} onClick={() => { if (!busy) void guard.close(); }}>取消</Button>
            <Button type="submit" disabled={inspect.busy} busy={save.busy} busyText="正在保存">{current?.ok ? "保存" : "保存并拉取"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
