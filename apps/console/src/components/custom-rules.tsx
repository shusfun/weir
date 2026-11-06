import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, CirclePause, CirclePlay, Pencil, Plus, Route, Search, Trash2, Unplug } from "lucide-react";
import {
  ActionButton, Badge, Button, Card, CardContent, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle,
  Field, Form, Input, Notice, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea, useConfirm,
  type ActionState,
} from "@weir/ui";
import { api, ApiError } from "../api";
import { field, useConsoleContext } from "../lib/console-context";
import type { CustomEntry, CustomMatch, CustomResult, CustomRuleKind, ProxyView } from "../lib/core";
import type { Pending } from "../lib/action";
import { WaitText } from "./wait";

const kinds: Record<CustomRuleKind, string> = { domain: "完整域名", domain_suffix: "域名后缀", domain_keyword: "域名关键词", ip_cidr: "IP 段" };
const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
type TestResult = { target: string; match: CustomMatch; entry_matches?: boolean; entry_enabled?: boolean; entry_id?: string; revision?: number };
type Target = [string, string];

function TargetSelect({ value, onChange, targets, disabled }: { value: string; onChange: (v: string) => void; targets: Target[]; disabled?: boolean }) {
  return <Select value={value} onValueChange={(v) => onChange(String(v))} disabled={disabled}>
    <SelectTrigger aria-label="规则走向"><SelectValue>{targets.find(([key]) => key === value)?.[1] ?? "原协议组（暂无节点）"}</SelectValue></SelectTrigger>
    <SelectContent>{targets.map(([key, label]) => <SelectItem key={key} value={key}>{label}</SelectItem>)}</SelectContent>
  </Select>;
}

export function CustomRulesCard({ base, view, onSaved }: { base: string; view: ProxyView; onSaved: () => Promise<unknown> }) {
  const c = view.custom;
  const confirm = useConfirm();
  const client = useQueryClient();
  const [dialog, setDialog] = useState<"add" | CustomEntry | null>(null);
  const [source, setSource] = useState("");
  const [addTarget, setAddTarget] = useState("select");
  const [editing, setEditing] = useState<CustomEntry | null>(null);
  const [editingRevision, setEditingRevision] = useState(c.revision);
  const [parsed, setParsed] = useState<CustomResult | null>(null);
  const [previewSource, setPreviewSource] = useState("");
  const [testRow, setTestRow] = useState<string | null>(null);
  const [testTarget, setTestTarget] = useState("");
  const [result, setResult] = useState<TestResult | null>(null);
  const [resultStale, setResultStale] = useState(false);
  const [states, setStates] = useState<Record<string, ActionState>>({});
  const [busy, setBusy] = useState(false);
  const active = useRef(false);
  const mounted = useRef(true);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);
  const [message, setMessage] = useState("");
  const [tone, setTone] = useState<"ok" | "warn" | "bad">("ok");
  const [removing, setRemoving] = useState<string | null>(null);
  const [errorField, setErrorField] = useState<Record<string, string> | undefined>();
  const root = base + "/proxy/custom";
  const targets: Target[] = [["direct", "直连"], ["select", "节点选择"], ["auto", "自动选择"], ...view.types.filter((t) => t.enabled).map((t): Target => [t.key, t.label])];
  const targetText = (target: string) => targets.find(([key]) => key === target)?.[1] ?? "原协议组（暂无节点，客户端改走节点选择）";
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; timers.current.forEach(clearTimeout); };
  }, []);
  useEffect(() => { if (testRow) { setResult(null); setResultStale(true); } }, [c.revision, view.categories.map((x) => x.key + ":" + x.target).join("|"), view.types.map((x) => x.key + ":" + x.enabled).join("|")]);
  useConsoleContext({ form: [field("custom.source", "批量添加清单", source), field("custom.target", "添加走向", addTarget), field("custom.test", "测试目标", testTarget), field("custom.entry", "编辑规则", editing?.value)] });

  const run = async (key: string, path: string, method: string, body: unknown, options: { save?: boolean; remove?: string } = {}) => {
    if (active.current) return null;
    active.current = true;
    setBusy(true); setStates({ [key]: "busy" }); setMessage(""); setErrorField(undefined);
    const started = Date.now();
    let committed = false;
    setPending({ since: started, label: options.save ? "正在检查并保存清单" : "正在计算规则", max: options.save ? 60 : undefined });
    try {
      const response = await api(path, method, body, { onJob: (job) => { if (mounted.current) setPending({ since: started, label: "正在处理清单", job }); } });
      committed = !!options.save;
      if (!mounted.current) return response;
      if (options.remove) {
        setRemoving(options.remove);
        await wait(250);
      }
      if (options.save) {
        client.setQueryData(["proxy", base], response);
        await onSaved();
      }
      if (!mounted.current) return response;
      const unknown = options.save && response.custom?.check?.status === "unknown";
      setStates({ [key]: unknown ? "warn" : "ok" });
      setTone(unknown ? "warn" : "ok");
      setMessage(options.save ? unknown ? `已保存；内核检查未知：${response.custom.check.detail}。客户端下次定时或手动更新订阅时生效。` : "已保存，客户端下次定时或手动更新订阅时生效。" : "计算完成；仅计算规则，不发起连接。");
      timers.current.push(setTimeout(() => { if (mounted.current) setStates((s) => s[key] === "busy" ? s : { ...s, [key]: "idle" }); }, 1200));
      return response;
    } catch (error) {
      if (!mounted.current) return null;
      setStates({ [key]: "error" }); setTone("bad");
      const text = error instanceof Error ? error.message : String(error);
      setMessage(committed ? "清单已保存，但刷新列表未完成：" + text + " 请刷新后核对。" : text);
      if (error instanceof ApiError) {
        if (error.code === "invalid_test_target") setErrorField({ target: text });
        if (["invalid_custom_entry", "duplicate_custom_entry"].includes(error.code)) setErrorField({ value: text });
        if (error.code === "custom_rules_too_large") setErrorField({ source: text });
        if (["custom_check_failed", "custom_revision_conflict"].includes(error.code)) {
          try { await onSaved(); } catch { /* 原错误保留，下一次重试仍会校验版本。 */ }
        }
      }
      return null;
    } finally {
      active.current = false;
      if (mounted.current) { setBusy(false); setPending(null); setRemoving(null); }
    }
  };
  const state = (key: string) => states[key] ?? "idle";
  const invalidateTest = () => { if (result) setResultStale(true); setResult(null); };
  const change = (e: CustomEntry, key: string, body: object) => run(key, root + "/entries/" + encodeURIComponent(e.id), "PATCH", { revision: c.revision, ...body }, { save: true });
  const openTest = (e: CustomEntry | null) => {
    setTestRow(e?.id ?? "all"); setResult(null); setResultStale(false); setErrorField(undefined);
    setTestTarget(e && (e.kind === "domain" || e.kind === "domain_suffix") ? e.value : e?.kind === "ip_cidr" && /\/(32|128)$/.test(e.value) ? e.value.split("/")[0] : "");
  };
  const testForm = (entry: CustomEntry | null, draft = false) => <Form errors={errorField} busy={busy} className="stack gap-2" onSubmit={async () => {
    setResult(null); setResultStale(false);
    const response = await run("test:" + (draft ? "draft" : entry?.id ?? "all"), entry ? root + "/entries/" + encodeURIComponent(entry.id) + "/test" : root + "/test", "POST", { target: testTarget, revision: draft ? editingRevision : c.revision, ...(draft ? { entry } : {}) });
    if (response) setResult(response);
  }}>
    <div className="flex flex-wrap items-start gap-2">
      <Field label={entry ? "本条规则的测试目标" : "整份清单的测试目标"} name="target" required className="min-w-0 flex-1 basis-48">
        <Input value={testTarget} onChange={(e) => { setTestTarget(e.target.value); if (result) setResultStale(true); setResult(null); }} placeholder="输入具体网址、域名或 IP" />
      </Field>
      <ActionButton type="submit" variant="outline" className="sm:mt-7" disabled={busy || !testTarget.trim()} state={state("test:" + (draft ? "draft" : entry?.id ?? "all"))} busyText="正在计算…"><Search />测试</ActionButton>
    </div>
    <div className="h-40 overflow-y-auto">{result ? <TestOutput result={result} /> : resultStale ? <p className="text-meta text-[var(--weir-warn-ink)]">设置或测试目标已变化，旧结果已失效，请重新测试。</p> : <p className="text-meta text-[var(--weir-ink-3)]">{draft ? "测试尚未保存的本条修改；" : ""}只计算规则，不连接网站。</p>}</div>
  </Form>;

  return <Card><CardContent>
    <SectionHeader title="自定义清单" description="清单按顺序优先匹配，未命中再按下方分类分流。由客户端执行，下次定时或手动更新完整订阅时生效；只接收节点列表的客户端用不上。" />
    <div className="flex flex-wrap gap-2">
      <Button disabled={busy} onClick={() => { setDialog("add"); setParsed(null); setEditingRevision(c.revision); setErrorField(undefined); setMessage(""); }}><Plus />批量添加</Button>
      <Button variant="outline" disabled={busy} onClick={() => openTest(null)}><Search />测试整份清单</Button>
    </div>
    {c.migration && <Notice tone="warn">{c.migration}</Notice>}
    <p className="text-meta text-[var(--weir-ink-3)]">已保存 {c.entries.length} 条，已启用 {c.entries.filter((e) => e.enabled).length} 条。列表靠前的规则先匹配。</p>
    <div className="stack gap-2">
      {c.entries.length === 0 && <p className="text-meta text-[var(--weir-ink-3)]">清单为空，全部按下方分流设置。可以一次粘贴多个网站。</p>}
      {c.entries.map((e, index) => <div key={e.id} data-entry-id={e.id} data-removing={removing === e.id || undefined} data-disabled={!e.enabled || undefined} className="weir-rule-row stack gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="text-meta tabular-nums text-[var(--weir-ink-3)]">{index + 1}</span>
          <span className="min-w-0 flex-1 break-all font-mono text-meta">{e.value}</span>
          <Badge variant="soft" tone={e.enabled ? "info" : "neutral"}>{e.enabled ? targetText(e.target) : "已停用"}</Badge>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <small className="text-[var(--weir-ink-3)]">{kinds[e.kind]}</small>
          <div className="flex flex-wrap gap-1" role="group" aria-label={e.value + " 的操作"}>
            <ActionButton size="icon-sm" variant="ghost" disabled={busy} state={state("route:" + e.id)} title={e.target === "direct" ? "改走代理" : "改为直连"} aria-label={e.value + (e.target === "direct" ? " 改走代理" : " 改为直连")} onClick={() => void change(e, "route:" + e.id, { target: e.target === "direct" ? e.last_proxy_target || "select" : "direct" })}>{e.target === "direct" ? <Unplug /> : <Route />}</ActionButton>
            <ActionButton size="icon-sm" variant="ghost" disabled={busy} state={state("enabled:" + e.id)} title={e.enabled ? "停用" : "启用"} aria-label={(e.enabled ? "停用 " : "启用 ") + e.value} onClick={() => void change(e, "enabled:" + e.id, { enabled: !e.enabled })}>{e.enabled ? <CirclePause /> : <CirclePlay />}</ActionButton>
            <Button size="icon-sm" variant="ghost" disabled={busy} title="测试本条" aria-label={"测试 " + e.value} onClick={() => openTest(e)}><Search /></Button>
            <Button size="icon-sm" variant="ghost" disabled={busy} title="修改" aria-label={"修改 " + e.value} onClick={() => { setDialog(e); setEditing({ ...e }); setEditingRevision(c.revision); setTestRow(null); setResult(null); setErrorField(undefined); setMessage(""); }}><Pencil /></Button>
            <ActionButton size="icon-sm" variant="ghost" disabled={busy || index === 0} state={state("up:" + e.id)} title="上移" aria-label={"上移 " + e.value} onClick={() => void change(e, "up:" + e.id, { position: index - 1 })}><ArrowUp /></ActionButton>
            <ActionButton size="icon-sm" variant="ghost" disabled={busy || index === c.entries.length - 1} state={state("down:" + e.id)} title="下移" aria-label={"下移 " + e.value} onClick={() => void change(e, "down:" + e.id, { position: index + 1 })}><ArrowDown /></ActionButton>
            <ActionButton size="icon-sm" variant="destructive" disabled={busy} state={state("delete:" + e.id)} title="删除" aria-label={"删除 " + e.value} onClick={async () => {
              if (await confirm({ title: "删除这条清单规则？", description: e.value, confirmText: "删除", tone: "danger" })) await run("delete:" + e.id, root + "/entries/" + encodeURIComponent(e.id), "DELETE", { revision: c.revision }, { save: true, remove: e.id });
            }}><Trash2 /></ActionButton>
          </div>
        </div>
        <div className="h-12 overflow-y-auto text-meta" role="status">{Object.keys(states).some((key) => key.endsWith(":" + e.id)) && (pending ? <WaitText wait={pending} /> : message ? <span className={tone === "bad" ? "text-[var(--weir-bad-ink)]" : tone === "warn" ? "text-[var(--weir-warn-ink)]" : "text-[var(--weir-ink-3)]"}>{message}{tone === "bad" && " 再点该操作可重试。"}</span> : null)}</div>
        {testRow === e.id && <div className="weir-rule-test">{testForm(e)}</div>}
      </div>)}
    </div>
    <div className="mt-2 h-16 overflow-y-auto text-meta" role="status" aria-live="polite">
      {pending ? <WaitText wait={pending} /> : message ? <span className={tone === "bad" ? "text-[var(--weir-bad-ink)]" : tone === "warn" ? "text-[var(--weir-warn-ink)]" : "text-[var(--weir-ink-2)]"}>{message}</span> : c.check ? <span>{c.check.status === "passed" ? "内核配置检查通过；不代表客户端已经更新。" : c.check.status === "failed" ? "内核配置检查未通过，仍使用上一份有效清单。" : "内核配置检查未知；清单已保存。"}{c.check.status !== "passed" && c.check.detail ? " " + c.check.detail : ""}</span> : null}
    </div>
    {c.pending && <div className="stack gap-2">
      <Notice tone="bad">上次修改未通过内核检查，当前列表仍是有效版本。失败候选已保留，可以重试。</Notice>
      <Collapsible title="查看未通过的候选清单"><ul className="text-meta">{c.pending.entries.map((e) => <li key={e.id} className="break-all">{e.value} → {targetText(e.target)}{!e.enabled && "（停用）"}</li>)}</ul>{c.pending.source && <pre className="whitespace-pre-wrap break-all text-meta">{c.pending.source}</pre>}</Collapsible>
      <ActionButton variant="outline" state={state("retry")} disabled={busy} onClick={() => void run("retry", root, "PUT", { revision: c.revision, entries: c.pending!.entries, source: c.pending!.source }, { save: true })}>重试上次修改</ActionButton>
    </div>}
    {testRow === "all" && <div className="mt-4">{testForm(null)}</div>}
    <Dialog open={dialog !== null} onOpenChange={(open) => { if (!open && !busy) setDialog(null); }}>
      <DialogContent size="md" className="sm:h-[min(680px,80vh)]"><DialogTitle>{dialog === "add" ? "批量添加清单" : "修改清单规则"}</DialogTitle>
        <DialogDescription>{dialog === "add" ? "每行一项，识别后追加到末尾；重复项保留已有走向。" : "保存本条改动时，会检查包含其他规则的完整候选订阅。"}</DialogDescription>
        <Form busy={busy} errors={errorField} onSubmit={async () => {
          const key = dialog === "add" ? "append" : "edit";
          const response = dialog === "add" ? await run(key, root + "/append", "POST", { revision: editingRevision, source, target: addTarget }, { save: true }) : editing ? await run(key, root + "/entries/" + encodeURIComponent(editing.id), "PATCH", { revision: editingRevision, kind: editing.kind, value: editing.value, target: editing.target }, { save: true }) : null;
          if (response) {
            if (dialog === "add") { setParsed(response.parsed ?? null); setSource(""); }
            await wait(450);
            if (mounted.current) setDialog(null);
          } else if (mounted.current) {
            const latest = client.getQueryData<ProxyView>(["proxy", base]);
            if (latest) setEditingRevision(latest.custom.revision);
          }
        }}>
          {dialog === "add" ? <>
            <Field label="本批走向"><TargetSelect value={addTarget} onChange={setAddTarget} targets={targets} disabled={busy} /></Field>
            <Field label="清单" name="source" required hint="域名、网址、IP、IP 段或客户端规则，每行一项。"><Textarea mono rows={7} className="max-h-80" value={source} onChange={(e) => { setSource(e.target.value); setParsed(null); }} /></Field>
            <ActionButton type="button" variant="outline" state={state("parse")} disabled={busy || !source.trim()} onClick={async () => { const response = await run("parse", root + "/parse", "POST", { source }); if (response) { setParsed(response); setPreviewSource(source); } }}>预览识别结果</ActionButton>
            {parsed && previewSource === source && <ParseOutput result={parsed} />}
          </> : editing && <>
            <Field label="类型"><Select value={editing.kind} onValueChange={(kind) => { setEditing({ ...editing, kind: kind as CustomRuleKind }); invalidateTest(); }}><SelectTrigger><SelectValue>{kinds[editing.kind]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(kinds).map(([key, text]) => <SelectItem key={key} value={key}>{text}</SelectItem>)}</SelectContent></Select></Field>
            <Field label="规则" name="value" required><Input value={editing.value} onChange={(e) => { setEditing({ ...editing, value: e.target.value }); invalidateTest(); }} /></Field>
            <Field label="走向"><TargetSelect value={editing.target} onChange={(target) => { setEditing({ ...editing, target }); invalidateTest(); }} targets={targets} disabled={busy} /></Field>
            <Button type="button" variant="outline" disabled={busy} onClick={() => { openTest(editing); setTestRow("draft"); }}><Search />测试本条修改</Button>
          </>}
          <DialogFooter error={tone === "bad" ? <span className="block h-16 overflow-y-auto text-meta">{message}</span> : undefined} status={tone !== "bad" ? <span className="block h-16 overflow-y-auto text-meta">{pending ? <WaitText wait={pending} /> : message}</span> : undefined}>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setDialog(null)}>取消</Button>
            <ActionButton type="submit" disabled={busy || (dialog === "add" ? !source.trim() : !editing?.value.trim())} state={state(dialog === "add" ? "append" : "edit")} busyText="正在检查并保存…">{dialog === "add" ? "添加到清单" : "保存本条"}</ActionButton>
          </DialogFooter>
        </Form>
        {dialog !== "add" && editing && testRow === "draft" && testForm(editing, true)}
      </DialogContent>
    </Dialog>
    {parsed && dialog === null && <ParseOutput result={parsed} />}
  </CardContent></Card>;
}

function ParseOutput({ result: r }: { result: CustomResult }) {
  return <div className="stack gap-1 text-meta" role="status"><p>识别 {r.rules.length} 条，跳过重复 {r.reduced} 条，未识别 {r.unused.length} 行。</p>{r.warnings.map((w) => <p key={w} className="text-[var(--weir-warn-ink)]">{w}</p>)}{r.unused.length > 0 && <Collapsible title="查看未识别的行"><ul>{r.unused.map((u) => <li key={u.line} className="break-all">第 {u.line} 行：{u.text} — {u.reason}</li>)}</ul></Collapsible>}</div>;
}

function TestOutput({ result: r }: { result: TestResult }) {
  const m = r.match;
  const policy = m.policy === "DIRECT" ? "直连" : m.policy === "REJECT" ? "拒绝" : `走「${m.policy}」`;
  return <div className="stack gap-1 text-meta" role="status">
    {r.entry_id && <p>本条{r.entry_matches ? "匹配" : "不匹配"}{r.entry_enabled === false ? "，但已停用" : ""}{r.entry_matches && r.entry_enabled && m.entry_id !== r.entry_id ? "，实际由更靠前的规则处理" : ""}。</p>}
    <p className="break-all"><strong>{r.target}</strong>：{policy}</p>
    <p className="break-all">最终命中「{m.label}」{m.value && <>，规则 <code>{m.value}</code></>}</p>
    {!!m.maybe?.length && <p className="text-[var(--weir-ink-3)]">客户端解析后可能先命中：{m.maybe.map((v) => v.label + "（" + (kinds[v.kind as CustomRuleKind] ?? v.kind) + "）").join("、")}</p>}
  </div>;
}
