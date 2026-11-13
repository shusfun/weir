import { Button, Field, Form, FormGrid, Input, Notice } from "@weir/ui";
import { api, isConflict } from "../api";
import { useQuery } from "../lib/query";
import { useDraft } from "../lib/dirty";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import type { components } from "../lib/contracts/public.gen";
import { Section } from "./system-settings";
import { State } from "./feedback";

type Document = components["schemas"]["RetentionDocument"];
type Policy = Document["policy"];
const fields: { key: keyof Policy; label: string; min: number; max: number; hint?: string }[] = [
  { key: "observation_days", label: "观测记录（天）", min: 1, max: 90 },
  { key: "task_days", label: "已结束任务（天）", min: 1, max: 90 },
  { key: "laya_days", label: "Laya 部署历史（天）", min: 1, max: 90 },
  { key: "sample_hours", label: "主机与流量采样（小时）", min: 25, max: 2160 },
  { key: "pull_days", label: "订阅拉取历史（天）", min: 30, max: 90 },
  { key: "audit_days", label: "操作审计（天）", min: 0, max: 3650, hint: "0 表示保留；启用清理时至少保留 30 天。" },
  { key: "max_records", label: "观测与任务历史数量上限", min: 100, max: 1000000 },
];

export function RetentionSettings() {
  const q = useQuery({ queryKey: ["retention-policy"], queryFn: () => api("/admin/settings/retention") as Promise<Document> });
  return <Section title="历史保留策略" description="保存后在下一轮维护生效，分批清理历史；待执行与恢复记录保留。">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <RetentionForm value={q.data} />}
  </Section>;
}

function RetentionForm({ value }: { value: Document }) {
  const draft = useDraft(value);
  const p = draft.value.policy;
  useConsoleContext({ form: fields.map(f => field(f.key, f.label, p[f.key])) });
  const save = useFormAction(() => api("/admin/settings/retention", "PUT", draft.value) as Promise<Document>, {
    ok: "保留策略已保存", invalidate: [["retention-policy"]],
    fields: { retention_audit_days_invalid: "audit_days" },
    onSuccess: result => draft.commit(result),
  });
  const reload = useFormAction(() => api("/admin/settings/retention") as Promise<Document>, {
    invalidate: [["retention-policy"]], onSuccess: result => { draft.commit(result); save.reset(); },
  });
  return <Form busy={save.busy || reload.busy} errors={save.errors} onSubmit={() => void save.submit()} data-tour="retention-policy">
    <Notice>缩短保留期会在后续维护中删除历史，无法撤销。审计默认保留；授权事实、凭据和待恢复任务不按普通历史清理。</Notice>
    <FormGrid>{fields.map(f => <Field key={f.key} name={f.key} label={f.label} required hint={f.hint ?? `${f.min}–${f.max}`}>
      <Input type="number" min={f.min} max={f.max} step={1} required value={p[f.key]} onChange={e => draft.set(old => ({ ...old, policy: { ...old.policy, [f.key]: Number(e.target.value) } }))} />
    </Field>)}</FormGrid>
    <Button type="submit" disabled={!draft.changed} busy={save.busy} busyText="正在保存…">保存保留策略</Button>
    {isConflict(save.raw) && <Button type="button" variant="outline" busy={reload.busy} onClick={() => void reload.submit()}>载入最新策略</Button>}
    {save.error && <Notice tone="bad">{isConflict(save.raw) ? "策略已被其他管理员修改。当前输入已保留，请核对最新策略后再保存。" : save.error}</Notice>}
    {reload.error && <Notice tone="bad">{reload.error}</Notice>}
  </Form>;
}
