// 通用确认窗：dsh（窗口里的对话或后台分析）提交、系统试运行通过的草稿，在任何页面都由 Weir 自己弹出。
// 内容从 Weir 接口读取，不接收 dsh 页面发来的任何消息；按参数说明生成字段：中文名与单位、引用显示名称、改前 / 改后。
// 管理员可以直接改字段再确认（改过的字段做标记）；危险操作要求当前密码与二次验证码。dsh 无法确认。
import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, Collapsible, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch, useToast } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { emptyReauth, Reauth } from "./system-settings";
import { DiffView, type DiffLine } from "./diff";
import { field, useConsoleContext } from "../lib/console-context";

interface StepField {
  key: string;
  label: string;
  kind: string;
  unit?: string;
  ref?: string;
  options?: [string, string][];
  hint?: string;
  secret?: boolean;
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
function FieldInput({ f, value, onChange }: { f: StepField; value: unknown; onChange: (v: unknown) => void }) {
  const label = f.label + (f.unit ? `（${f.unit}）` : "");
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
    <section className={"stack rounded-[var(--mg-radius)] border p-3 " + (danger ? "border-[var(--mg-bad)]" : "border-[var(--mg-line)]")} aria-label={step.title}>
      <div className="flex flex-wrap items-center gap-2">
        {total > 1 && <Badge variant="soft" tone="neutral">第 {index + 1} / {total} 步</Badge>}
        <strong className="text-sm">{step.title}</strong>
        {danger && <Badge variant="soft" tone="bad">危险操作</Badge>}
        {step.fields.some(editable) && (
          <Button className="ml-auto" size="xs" variant="ghost" aria-expanded={editing} onClick={() => setEditing((v) => !v)}>{editing ? "收起修改" : "修改字段"}</Button>
        )}
      </div>
      {step.fields.length > 0 && (
        <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-3 gap-y-1.5 text-[13px]">
          {step.fields.map((f) => {
            const edited = f.key in edits;
            return (
              <div key={f.key} className="contents">
                <dt className="text-[var(--mg-ink-3)]">{f.label}{f.unit ? `（${f.unit}）` : ""}</dt>
                <dd className="min-w-0 break-words">
                  {editing && editable(f) ? (
                    <FieldInput f={f} value={edited ? edits[f.key] : f.value} onChange={(v) => setEdit(f.key, v)} />
                  ) : f.before !== undefined && f.changed ? (
                    <span><span className="text-[var(--mg-ink-3)] line-through">{f.before}</span> → <strong>{edited ? String(edits[f.key]) : f.display}</strong></span>
                  ) : (
                    <span>{edited ? String(edits[f.key]) : f.display}</span>
                  )}
                  {edited && <Badge className="ml-2" variant="soft" tone="warn">已改</Badge>}
                </dd>
              </div>
            );
          })}
        </dl>
      )}
      {step.result && typeof step.result.latency_ms === "number" && <Notice tone="ok">实测通过（{step.result.stage ?? "连接"}，{step.result.latency_ms} ms）。</Notice>}
      {step.diff && <DiffView lines={step.diff} />}
      {(step.preview ?? []).length > 0 && (
        <ul className="m-0 list-disc space-y-0.5 pl-5 text-[13px] text-[var(--mg-ink-2)]">{step.preview!.map((p) => <li key={p}>{p}</li>)}</ul>
      )}
    </section>
  );
}

export function DraftConfirm() {
  const toast = useToast();
  const client = useQueryClient();
  // 只在页面可见时每 4 秒查一次待确认的草稿。
  const q = useQuery({ queryKey: ["ai-drafts"], queryFn: async () => (await api("/admin/ai/drafts")).drafts as Draft[], refetchInterval: 4000, refetchIntervalInBackground: false });
  const [later, setLater] = useState<string[]>([]);
  const [edits, setEdits] = useState<Edits>({});
  const [re, setRe] = useState(emptyReauth);
  const [outputs, setOutputs] = useState<{ step: string; email?: string; password?: string }[] | null>(null);
  // 本次要处理的草稿与决定：用 ref 传给请求，避免读到上一次渲染的值
  const target = useRef<{ draft: Draft; apply: boolean } | null>(null);
  const decision = useFormAction(async () => {
    const { draft, apply } = target.current!;
    if (apply) {
      const r = (await api(`/admin/ai/drafts/${draft.id}/apply`, "POST", { edits, ...re })) as { outputs?: { step: string; email?: string; password?: string }[] };
      toast("草稿已生效", { tone: "ok" });
      const secrets = (r.outputs ?? []).filter((o) => o.password);
      if (secrets.length > 0) setOutputs(secrets);
    } else {
      await api(`/admin/ai/drafts/${draft.id}`, "DELETE");
      toast("已放弃草稿", { tone: "info" });
    }
    setEdits({});
    setRe(emptyReauth);
    await client.invalidateQueries();
  });
  // 页面感知：正在确认哪份草稿、改过哪些字段（在提前返回之前声明，hook 次数不变）
  const open = (q.data ?? []).find((x) => !later.includes(x.id));
  useConsoleContext(open && !outputs ? { dialog: { title: "确认 dsh 草稿", step: `${open.steps?.length ?? 0} 步` }, form: [field("draft", "草稿", open.id), field("edited", "管理员改过的字段", Object.values(edits).reduce((n, e) => n + Object.keys(e).length, 0) + " 个")] } : null);
  if (outputs) {
    return (
      <Dialog open onOpenChange={(o) => !o && setOutputs(null)}>
        <DialogContent size="md" layer="top">
          <DialogTitle>新用户的初始密码</DialogTitle>
          <DialogDescription>只显示这一次，请现在交给用户；dsh 看不到这些密码。</DialogDescription>
          <ul className="stack">
            {outputs.map((o) => (
              <li key={o.email} className="flex flex-wrap items-center gap-2 text-sm"><strong>{o.email}</strong><code>{o.password}</code><CopyButton value={o.password ?? ""}>复制</CopyButton></li>
            ))}
          </ul>
          <DialogFooter><Button onClick={() => setOutputs(null)}>完成</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }
  const busy = decision.busy;
  const d = (q.data ?? []).find((x) => !later.includes(x.id));
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
        <DialogDescription>草稿由 dsh 提交，已用手动操作的同一套校验试运行通过。确认之前不会生效；多步草稿整体生效，任一步失败全部撤回。dsh 无法替你确认。</DialogDescription>
        <div className="stack max-h-[62vh] overflow-y-auto">
          {d.explain && (
            <div className="stack">
              <small className="text-[var(--mg-ink-3)]">dsh 的说明</small>
              <p className="whitespace-pre-wrap break-words text-sm">{d.explain}</p>
            </div>
          )}
          {steps.map((st, i) => <StepCard key={i} step={st} index={i} total={steps.length} edits={edits[i] ?? {}} setEdit={setEdit(i)} />)}
          {d.danger && (
            <>
              <Notice tone="bad">这份草稿里有不能撤回的危险操作，请输入当前密码（与二次验证码）确认。</Notice>
              <Reauth value={re} onChange={setRe} />
            </>
          )}
        </div>
        <DialogFooter error={decision.error}>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setLater((l) => [...l, d.id])}>稍后</Button>
          <Button type="button" variant="outline" disabled={busy} onClick={() => decide(false)}>放弃</Button>
          <Button type="button" variant={d.danger ? "destructive" : "default"} disabled={busy || (d.danger && !re.password)} onClick={() => decide(true)} busy={busy} busyText="正在执行…">确认生效</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
