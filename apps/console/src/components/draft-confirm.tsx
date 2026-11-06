// 通用确认窗：dsh（窗口里的对话或后台分析）提交、系统试运行通过的草稿，在任何页面都由 Weir 自己弹出。
// 内容从 Weir 接口读取，不接收 dsh 页面发来的任何消息；按参数说明生成字段：中文名与单位、引用显示名称、改前 / 改后。
// 管理员可以直接改字段再确认（改过的字段做标记）；危险操作单独标出。dsh 无法确认。
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { Badge, Button, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Input, Notice, PasswordInput, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, StatusLamp, Switch, useToast } from "@weir/ui";
import { api, clause } from "../api";
import { nodeStatus, type Gate, type NodeItem } from "../lib/core";
import { ObjectLink } from "./object-link";
import { useFormAction } from "../lib/action";
import { DiffView, type DiffLine } from "./diff";
import { field, useConsoleContext } from "../lib/console-context";
import { useLive } from "../lib/live";
import { WaitText } from "./wait";

interface StepField {
  key: string;
  label: string;
  kind: string;
  unit?: string;
  ref?: string;
  options?: [string, string][];
  hint?: string;
  secret?: boolean;
  required?: boolean;
  value: unknown;
  display: string;
  before?: string;
  changed?: boolean;
}
interface Step {
  action: string;
  title: string;
  risk: "normal" | "danger";
  fields: StepField[];
  result?: { latency_ms?: number; stage?: string };
  preview?: string[];
  edited?: string[];
  diff?: DiffLine[];
}
export interface Draft {
  id: string;
  kind: "actions";
  source: "dsh" | "analysis";
  explain: string;
  created_at: string;
  steps: Step[] | null;
  danger?: boolean;
}
type Edits = Record<string, Record<string, unknown>>;

const editable = (f: StepField) => !f.secret && ["string", "int", "gb", "days", "bool", "enum"].includes(f.kind) && f.key !== "email";
// FieldInput：按参数说明生成可编辑控件；引用、对象类只显示名称，不可直接改。
// 机密参数（订阅地址、代理密码、节点链接）不经过 dsh，总是由管理员在这里直接填，值不回显。
function FieldInput({ f, value, onChange }: { f: StepField; value: unknown; onChange: (v: unknown) => void }) {
  const label = f.label + (f.unit ? `（${f.unit}）` : "");
  if (f.secret) {
    return <PasswordInput aria-label={label} autoComplete="off" placeholder={f.display === "不改" ? "不填则不改" : f.required ? "必填：不填不能确认" : "可不填"} value={String(value ?? "")} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)} />;
  }
  switch (f.kind) {
    case "bool":
      return <Switch aria-label={label} checked={value === true} onCheckedChange={onChange} />;
    case "enum":
      return (
        <Select value={String(value ?? "")} onValueChange={onChange}>
          <SelectTrigger aria-label={label}><SelectValue>{f.options?.find(([o]) => o === value)?.[1] ?? String(value ?? "")}</SelectValue></SelectTrigger>
          <SelectContent>{(f.options ?? []).map(([o, t]) => <SelectItem key={o} value={o}>{t}</SelectItem>)}</SelectContent>
        </Select>
      );
    case "int":
    case "gb":
    case "days":
      return <Input aria-label={label} inputMode="decimal" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />;
    default:
      return <Input aria-label={label} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />;
  }
}

// StepCard：一步草稿。修改类显示改前 → 改后；管理员改过的字段标「已改」。
function StepCard({ step, index, total, edits, setEdit }: { step: Step; index: number; total: number; edits: Record<string, unknown>; setEdit: (key: string, v: unknown) => void }) {
  const [editing, setEditing] = useState(false);
  const danger = step.risk === "danger";
  return (
    <section className={"stack rounded-[var(--weir-radius)] border p-3 " + (danger ? "border-[var(--weir-bad)]" : "border-[var(--weir-line)]")} aria-label={step.title}>
      <div className="flex flex-wrap items-center gap-2">
        {total > 1 && <Badge variant="soft" tone="neutral">第 {index + 1} / {total} 步</Badge>}
        <strong className="text-body">{step.title}</strong>
        {danger && <Badge variant="soft" tone="bad">危险操作</Badge>}
        {step.fields.some(editable) && (
          <Button className="ml-auto" size="xs" variant="ghost" aria-expanded={editing} onClick={() => setEditing((v) => !v)}>{editing ? "收起修改" : "修改字段"}</Button>
        )}
      </div>
      {step.fields.length > 0 && (
        <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-3 gap-y-1.5 text-body">
          {step.fields.map((f) => {
            const edited = f.key in edits;
            return (
              <div key={f.key} className="contents">
                <dt className="text-[var(--weir-ink-3)]">{f.label}{f.unit ? `（${f.unit}）` : ""}</dt>
                <dd className="min-w-0 break-words">
                  {f.secret ? (
                    <FieldInput f={f} value={edits[f.key]} onChange={(v) => setEdit(f.key, v)} />
                  ) : editing && editable(f) ? (
                    <FieldInput f={f} value={edited ? edits[f.key] : f.value} onChange={(v) => setEdit(f.key, v)} />
                  ) : f.before !== undefined && f.changed ? (
                    <span><span className="text-[var(--weir-ink-3)] line-through">{f.before}</span> → <strong>{edited ? String(edits[f.key]) : f.display}</strong></span>
                  ) : (
                    <span>{edited ? String(edits[f.key]) : f.display}</span>
                  )}
                  {edited && !f.secret && <Badge className="ml-2" variant="soft" tone="warn">已改</Badge>}
                </dd>
              </div>
            );
          })}
        </dl>
      )}
      {step.result && typeof step.result.latency_ms === "number" && <Notice tone="ok">实测通过（{step.result.stage ?? "连接"}，{step.result.latency_ms} ms）。</Notice>}
      {step.diff && <DiffView lines={step.diff} />}
      {(step.preview ?? []).length > 0 && (
        <ul className="m-0 list-disc space-y-0.5 pl-5 text-body text-[var(--weir-ink-2)]">{step.preview!.map((p) => <li key={p}>{p}</li>)}</ul>
      )}
    </section>
  );
}

// 待确认的草稿：事件流连着时 dsh 一提交就收到提醒，不轮询；断开时只在页面可见时每 4 秒查一次。
export function useDrafts() {
  const live = useLive();
  return useQuery({ queryKey: ["ai-drafts"], queryFn: async () => (await api("/admin/ai/drafts")).drafts as Draft[], refetchInterval: live ? false : 4000, refetchIntervalInBackground: false });
}

// 点「稍后」收起的草稿由铃铛与 dsh 窗口重新打开。
const reopenEvent = "weir:drafts-reopen";
export function reopenDrafts() {
  window.dispatchEvent(new CustomEvent(reopenEvent));
}

// DraftsPending：铃铛里的「N 份待确认」；草稿有效期 1 小时，写明最紧的一份还剩多久。
export function DraftsPending({ onOpen }: { onOpen: () => void }) {
  const q = useDrafts();
  const list = q.data ?? [];
  if (list.length === 0) return null;
  return (
    <div className="stack border-b border-[var(--weir-line)] px-3 py-2">
      <div className="flex items-start gap-2">
        <Badge variant="soft" tone="warn">待确认</Badge>
        <strong className="text-body">dsh 有 {list.length} 份草稿等你确认</strong>
      </div>
      <small className="text-[var(--weir-ink-3)]">{minutesLeft(list[0])}</small>
      <div><Button size="xs" onClick={() => { reopenDrafts(); onOpen(); }}>查看</Button></div>
    </div>
  );
}

function minutesLeft(d: Draft) {
  const left = Math.ceil((new Date(d.created_at).getTime() + 3600_000 - Date.now()) / 60000);
  return left > 0 ? `还有 ${left} 分钟作废` : "即将作废";
}

interface Outcome {
  nodes: number[];
  gates: number[];
  outputs: { step: string; email?: string; password?: string }[];
}

// DraftOutcome：「确认」只代表配置写入了。窗口留着，显示 VPS 与节点实际跑起来没有；最多等 90 秒，超时如实说。
function DraftOutcome({ outcome, close }: { outcome: Outcome; close: () => void }) {
  const secrets = outcome.outputs.filter((o) => o.password);
  const [expired, setExpired] = useState(false);
  useEffect(() => {
    const t = window.setTimeout(() => setExpired(true), 90000);
    return () => window.clearTimeout(t);
  }, []);
  const watching = outcome.nodes.length > 0 || outcome.gates.length > 0;
  const settled = (nodes?: NodeItem[], gates?: Gate[]) => {
    const related = (gates ?? []).filter((g) => outcome.gates.includes(g.id) || (nodes ?? []).some((n) => outcome.nodes.includes(n.id) && n.gate_id === g.id));
    if (related.some((g) => g.state === "pending")) return false;
    return (nodes ?? []).filter((n) => outcome.nodes.includes(n.id)).every((n) => n.readiness?.state === "ready" || n.readiness?.state === "credentials");
  };
  const [settledNow, setSettledNow] = useState(false);
  const poll = expired || settledNow ? false : 2000;
  const nodes = useQuery({ queryKey: ["nodes"], enabled: watching, queryFn: async () => (await api("/admin/nodes")).items as NodeItem[], refetchInterval: poll });
  const gates = useQuery({ queryKey: ["gates"], enabled: watching, queryFn: async () => (await api("/admin/gates")).items as Gate[], refetchInterval: poll });
  const done = !!nodes.data && !!gates.data && settled(nodes.data, gates.data);
  useEffect(() => setSettledNow(done), [done]);
  const shownNodes = (nodes.data ?? []).filter((n) => outcome.nodes.includes(n.id));
  const shownGates = (gates.data ?? []).filter((g) => outcome.gates.includes(g.id) || shownNodes.some((n) => n.gate_id === g.id));
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="md" layer="top">
        <DialogTitle>草稿已写入</DialogTitle>
        <DialogDescription>{watching ? "下面是 VPS 与节点实际的运行情况，dsh 下一轮也能读到。" : "改动已保存。"}</DialogDescription>
        <div className="stack">
          {shownGates.map((g) => (
            <div key={g.id} className="flex flex-wrap items-center gap-2 text-body">
              <StatusLamp tone={!g.online || g.state === "failed" ? "bad" : g.state === "pending" ? "info" : "ok"} />
              <ObjectLink to={{ type: "gate", id: g.id, name: g.name }} />
              <span className="text-[var(--weir-ink-2)]">{!g.online ? "VPS 离线" : g.state === "failed" ? "配置应用失败：" + g.last_error : g.state === "pending" ? "配置正在下发" : "配置已生效"}</span>
            </div>
          ))}
          {shownNodes.map((n) => {
            const g = (gates.data ?? []).find((x) => x.id === n.gate_id);
            const [label, tone, why] = nodeStatus(n, g);
            return (
              <div key={n.id} className="flex flex-wrap items-center gap-2 text-body">
                <StatusLamp tone={tone} />
                <ObjectLink to={{ type: "node", id: n.id, name: n.name }} />
                <span className="text-[var(--weir-ink-2)]">{label}</span>
                <small className="w-full text-[var(--weir-ink-3)]">{why}</small>
              </div>
            );
          })}
          {watching && !done && <small className="text-[var(--weir-ink-3)]">{expired ? "等了 90 秒还没有结果：可以打开上面的详情，或让 dsh 查内核诊断。" : "正在等 VPS 回报…"}</small>}
          {secrets.length > 0 && (
            <div className="stack">
              <strong className="text-body">新用户的初始密码</strong>
              <small className="text-[var(--weir-ink-3)]">只显示这一次，请现在交给用户；dsh 看不到这些密码。</small>
              <ul className="stack">
                {secrets.map((o) => (
                  <li key={o.email} className="flex flex-wrap items-center gap-2 text-body"><strong>{o.email}</strong><code>{o.password}</code><CopyButton value={o.password ?? ""}>复制</CopyButton></li>
                ))}
              </ul>
            </div>
          )}
        </div>
        <DialogFooter><Button onClick={close}>完成</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// DraftReadFailure：读不到待确认的草稿时在顶部说明原因并给「重新加载」——dsh 说已经提交，这边不能什么也不显示。
export function DraftReadFailure() {
  const q = useDrafts();
  if (!q.isError || q.data) return null;
  return (
    <div className="origin-banner">
      <Notice tone="bad" role="alert">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="min-w-0 flex-1">读取 dsh 提交的待确认草稿失败：{clause(q.error)}。dsh 提交的草稿要在这里确认才会生效。</span>
          <Button size="sm" variant="outline" disabled={q.isFetching} busy={q.isFetching} busyText="正在读取…" onClick={() => void q.refetch()}>重新加载</Button>
        </div>
      </Notice>
    </div>
  );
}

export function DraftConfirm() {
  const toast = useToast();
  const client = useQueryClient();
  const q = useDrafts();
  const [later, setLater] = useState<string[]>([]);
  const [edits, setEdits] = useState<Edits>({});
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  useEffect(() => {
    const reopen = () => setLater([]);
    window.addEventListener(reopenEvent, reopen);
    return () => window.removeEventListener(reopenEvent, reopen);
  }, []);
  // 本次要处理的草稿与决定：用 ref 传给请求，避免读到上一次渲染的值
  const target = useRef<{ draft: Draft; apply: boolean } | null>(null);
  const decision = useFormAction(async (track) => {
    const { draft, apply } = target.current!;
    if (apply) {
      const r = (await api(`/admin/ai/drafts/${draft.id}/apply`, "POST", { edits }, track)) as { outputs?: Outcome["outputs"]; node_ids?: number[]; gate_ids?: number[] };
      setOutcome({ nodes: r.node_ids ?? [], gates: r.gate_ids ?? [], outputs: r.outputs ?? [] });
    } else {
      await api(`/admin/ai/drafts/${draft.id}`, "DELETE");
      toast("已放弃草稿", { tone: "info" });
    }
    setEdits({});
    await client.invalidateQueries();
  }, {
    wait: () => {
      const t = target.current;
      const n = t?.draft.steps?.length ?? 0;
      return t?.apply ? { label: n > 1 ? `正在按顺序执行 ${n} 步` : "正在执行" } : { label: "正在放弃草稿" };
    },
  });
  // 页面感知：正在确认哪份草稿、改过哪些字段（在提前返回之前声明，hook 次数不变）
  const open = (q.data ?? []).find((x) => !later.includes(x.id));
  useConsoleContext(open && !outcome ? { dialog: { title: "确认 dsh 草稿", step: `${open.steps?.length ?? 0} 步` }, form: [field("draft", "草稿", open.id), field("edited", "管理员改过的字段", Object.values(edits).reduce((n, e) => n + Object.keys(e).length, 0) + " 个")] } : null);
  if (outcome) return <DraftOutcome outcome={outcome} close={() => setOutcome(null)} />;
  const busy = decision.busy;
  // 执行期间中心不再列出这份草稿：窗口留着它，直到结果出来
  const d = (busy ? target.current?.draft : undefined) ?? (q.data ?? []).find((x) => !later.includes(x.id));
  if (!d) return null;
  function decide(apply: boolean) {
    target.current = { draft: d!, apply };
    void decision.submit();
  }
  const steps = d.steps ?? [];
  const pending = (q.data ?? []).length;
  const setEdit = (i: number) => (key: string, v: unknown) => setEdits((e) => ({ ...e, [i]: { ...(e[i] ?? {}), [key]: v } }));
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && setLater((l) => [...l, d.id])}>
      <DialogContent size="lg" layer="top">
        <DialogTitle className="flex flex-wrap items-center gap-2">
          {steps.length > 1 ? `确认 dsh 草稿 · ${steps.length} 步` : `确认 dsh 草稿 · ${steps[0]?.title ?? ""}`}
          <Badge variant="soft" tone="neutral">{d.source === "analysis" ? "dsh 后台分析" : "dsh 对话"}</Badge>
          {pending > 1 && <Badge variant="soft" tone="warn">还有 {pending - 1} 份待确认</Badge>}
        </DialogTitle>
        <DialogDescription>确认之前不会生效；多步草稿按顺序执行，任一步失败，能撤回的全部撤回。要你填写的内容在确认时会真连一次，不通就不生效。<span className="whitespace-nowrap">{minutesLeft(d)} · 编号 <code>{d.id.slice(0, 8)}</code></span></DialogDescription>
        {/* 弹窗正文自己会滚动，这里不另设内层滚动区 */}
        <div className="stack">
          {d.explain && (
            <div className="stack">
              <small className="text-[var(--weir-ink-3)]">dsh 的说明</small>
              <p className="whitespace-pre-wrap break-words text-body">{d.explain}</p>
            </div>
          )}
          {steps.map((st, i) => <StepCard key={i} step={st} index={i} total={steps.length} edits={edits[i] ?? {}} setEdit={setEdit(i)} />)}
          {d.danger && <Notice tone="bad">这份草稿里有不能撤回的危险操作，请核对后再确认。</Notice>}
        </div>
        <DialogFooter error={decision.error} status={decision.wait && <WaitText wait={decision.wait} />}>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setLater((l) => [...l, d.id])}>稍后</Button>
          <Button type="button" variant="outline" disabled={busy} onClick={() => decide(false)}>放弃</Button>
          <Button type="button" variant={d.danger ? "destructive" : "default"} disabled={busy} onClick={() => decide(true)} busy={busy} busyText="正在执行…">确认生效</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
