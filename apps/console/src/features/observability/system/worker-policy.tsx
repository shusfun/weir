import { ActionGroup, Button, Collapsible, Field, Form, FormGrid, Input, Notice } from "@weir/ui";
import { api } from "../../../api";
import { useQuery, queryClient } from "../../../lib/query";
import { useFormAction, useRowAction } from "../../../lib/action";
import { useDraft } from "../../../lib/dirty";
import { field, useConsoleContext } from "../../../lib/console-context";
import { reopenDrafts } from "../../../components/draft-confirm";
import { State } from "../../../components/feedback";
import { date } from "../../../lib/models";

type Budget = { concurrency: number; bandwidth_bytes_per_second: number };
type Policy = { revision: string; document: { schema: number; external: Partial<Budget>; site: Partial<Budget> }; effective: Budget; sources: Record<string, string> };

export function WorkerPolicy() {
  const q = useQuery({ queryKey: ["worker-policy"], queryFn: () => api("/admin/workers/policy") as Promise<Policy> });
  const history = useQuery({ queryKey: ["worker-policy-history"], queryFn: async () => (await api("/admin/workers/policy/history")).items as {revision:string; document:Policy["document"]; created_at:string}[] });
  const restore = useRowAction<string>();
  return <Collapsible title="VPS 后台任务预算">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <BudgetForm value={q.data} />}
    <Collapsible title="任务预算版本">
      {history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={()=>history.refetch()} /> : history.data.length===0 ? <State empty="尚无已确认的预算修改" /> : history.data.map(version=><ActionGroup key={version.revision}>
        <span className="min-w-0 break-all font-mono text-meta">{date(version.created_at)} · {version.revision}</span>
        <Button variant="outline" size="sm" disabled={!q.data || version.revision===q.data.revision || restore.isBusy()} busy={restore.isBusy(version.revision)} onClick={()=>void restore.run(version.revision,async()=>{
          await api("/admin/workers/policy/draft","POST",{text:JSON.stringify(version.document)});
          void queryClient.invalidateQueries({queryKey:["ai-drafts"]});reopenDrafts();
        },"恢复草稿已生成，确认后生效")}>预演恢复预算</Button>
      </ActionGroup>)}
    </Collapsible>
  </Collapsible>;
}

function BudgetForm({ value }: { value: Policy }) {
  const draft = useDraft(value.effective);
  const budget = draft.value, setBudget = draft.set, changed = draft.changed;
  useConsoleContext({ form: [field("concurrency", "VPS 后台并发", budget.concurrency), field("bandwidth_bytes_per_second", "后台收发正文预算", budget.bandwidth_bytes_per_second)] });
  const save = useFormAction(() => api("/admin/workers/policy/draft", "POST", {
    text: JSON.stringify({ ...value.document, site: { ...value.document.site, ...budget } }),
  }), { ok: "预算差异已生成，管理员确认后生效", fields: { worker_policy_invalid: "budget" },
    onSuccess: () => { draft.commit(); void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts(); } });
  return <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
    <Notice>默认后台单并发，大文件始终单并发。带宽限制按每台 VPS 的后台 HTTP 正文收发合计计算；配置同步、心跳和计量独立运行。</Notice>
    <FormGrid>
      <Field label="后台并发" name="budget" required hint="1–32；降低上限不会中断已经运行的任务。">
        <Input type="number" min={1} max={32} required value={budget.concurrency} onChange={(e) => setBudget((old) => ({ ...old, concurrency: Number(e.target.value) }))} />
      </Field>
      <Field label="每秒正文带宽（KiB）" required hint="64–131072；由 VPS 共用限速器执行。">
        <Input type="number" min={64} max={131072} step={1} required value={budget.bandwidth_bytes_per_second / 1024}
          onChange={(e) => setBudget((old) => ({ ...old, bandwidth_bytes_per_second: Number(e.target.value) * 1024 }))} />
      </Field>
    </FormGrid>
    <small className="break-all">生效版本 {value.revision} · 并发来源 {value.sources.concurrency ?? "未知"} · 带宽来源 {value.sources.bandwidth_bytes_per_second ?? "未知"}</small>
    <Button type="submit" disabled={!changed} busy={save.busy} busyText="正在预演…">预演任务预算</Button>
    {save.error && <Notice tone="bad">{save.error}</Notice>}
  </Form>;
}
