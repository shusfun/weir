import { useState } from "react";
import { ActionGroup, Button, Collapsible, Field, Form, FormGrid, Input, Notice, SwitchField, Textarea } from "@weir/ui";
import { api } from "../../api";
import { useQuery, queryClient } from "../../lib/query";
import { useFormAction, useRowAction } from "../../lib/action";
import { field, useConsoleContext } from "../../lib/console-context";
import { useUnsavedChanges } from "../../lib/dirty";
import { reopenDrafts } from "../../components/draft-confirm";
import { State } from "../../components/feedback";
import { Section } from "../../components/system-settings";

type Values = {
  enabled: boolean;
  hourly_limit: number;
  max_minutes: number;
  protected_networks: string[];
  protected_domains: string[];
  protected_fields: string[];
};
type Document = { schema: number; external: Partial<Values>; site: Partial<Values> };
type Response = {
	defaults: Values;
  policy: { revision: string; document: Document; effective: Values; sources: Record<string, string> };
  hourly_used: number;
  hourly_limit: number;
};
const lines = (text: string) => text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);

export function ProtectionPolicyCard() {
  const q = useQuery({ queryKey: ["protection-policy"], queryFn: () => api("/admin/defense/policy") as Promise<Response> });
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />;
  return <PolicyForm key={q.data.policy.revision} value={q.data} />;
}

function PolicyForm({ value }: { value: Response }) {
	const history = useQuery({ queryKey: ["protection-policy-history"], queryFn: async () =>
		(await api("/admin/defense/policy/history")).items as { revision: string; document: Document }[] });
	const restore = useRowAction<string>();
  const [site, setSite] = useState(value.policy.document.site);
  const effective = { ...value.defaults, ...value.policy.document.external, ...site };
  const set = (patch: Partial<Values>) => setSite((previous) => ({ ...previous, ...patch }));
  useUnsavedChanges(JSON.stringify(site) !== JSON.stringify(value.policy.document.site));
  const save = useFormAction(() => api("/admin/defense/policy/draft", "POST", {
    text: JSON.stringify({ ...value.policy.document, site }),
  }), {
    ok: "已生成完整差异，管理员确认后生效",
    fields: { protection_policy_invalid: "policy" },
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts(); },
  });
  useConsoleContext({ tab: "自动安全处置", form: [
    field("enabled", "自动收紧", effective.enabled ? "开启" : "关闭"),
    field("hourly_limit", "滚动一小时上限", effective.hourly_limit),
    field("max_minutes", "最长期限（分钟）", effective.max_minutes),
  ] });
  return <Section title="自动安全处置护栏" description="仅安全临时收紧可由宿主执行；优化、放宽和 AI 自修改始终需要管理员确认。">
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <Notice>当前生效版本 {value.policy.revision}。滚动一小时自动操作 {value.hourly_used} / {value.hourly_limit} 次。</Notice>
      <Field label="自动安全处置策略" name="policy">
        <SwitchField label="允许受限的安全临时收紧" checked={effective.enabled}
          onCheckedChange={(enabled) => set({ enabled })} />
      </Field>
      <FormGrid>
        <Field label="滚动一小时最多操作数" hint="1–5 次；撤销不会退还次数，超限只产生草稿。">
          <Input type="number" required min={1} max={5} value={effective.hourly_limit}
            onChange={(e) => set({ hourly_limit: Number(e.target.value) })} />
        </Field>
        <Field label="最长生效时间（分钟）" hint="5–1440 分钟；到期后恢复底层策略。">
          <Input type="number" required min={5} max={1440} value={effective.max_minutes}
            onChange={(e) => set({ max_minutes: Number(e.target.value) })} />
        </Field>
      </FormGrid>
      <Field label="受保护网段" hint="每行一个 CIDR，例如 203.0.113.0/24；本机、私网和链路本地地址始终受保护。">
        <Textarea rows={3} value={effective.protected_networks.join("\n")}
          onChange={(e) => set({ protected_networks: lines(e.target.value) })} />
      </Field>
      <Field label="受保护域名" hint="每行一个规范化域名；禁止自动追加会影响这些域名的拒绝规则。">
        <Textarea rows={3} value={effective.protected_domains.join("\n")}
          onChange={(e) => set({ protected_domains: lines(e.target.value) })} />
      </Field>
      <Field label="禁止自动调整的策略字段" hint="每行一个已登记账户策略字段名。">
        <Textarea rows={3} value={effective.protected_fields.join("\n")}
          onChange={(e) => set({ protected_fields: lines(e.target.value) })} />
      </Field>
      <Collapsible title="配置来源与继承">
        <ul className="stack">{Object.keys(effective).map((key) => <li key={key} className="row-wrap">
          <span>{key} · {Object.hasOwn(site, key) ? "站点覆盖" : value.policy.sources[key] === "external" ? "外部来源" : "内置默认"}</span>
          {Object.hasOwn(site, key) && <Button type="button" size="sm" variant="ghost"
            onClick={() => setSite((previous) => { const next = { ...previous }; delete next[key as keyof Values]; return next; })}>
            恢复继承
          </Button>}
        </li>)}</ul>
      </Collapsible>
      <Notice>关闭总开关会暂停已有自动处置；管理员确认的临时或永久处置需单独撤销。保护名单、规则边界和期限由服务端执行。</Notice>
      <Button type="submit" busy={save.busy} busyText="正在预演…">预演护栏修改</Button>
      {save.error && <Notice tone="bad">{save.error}</Notice>}
    </Form>
		<Collapsible title="自动安全处置策略版本">
			{history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={() => history.refetch()} /> :
			 history.data.length === 0 ? <State empty="尚无已确认的护栏修改" /> : history.data.map((version) => <div key={version.revision} className="row-wrap">
				<span className="min-w-0 break-all font-mono text-meta">{version.revision}</span>
				<Button size="sm" variant="outline" disabled={version.revision === value.policy.revision || restore.isBusy()}
					busy={restore.isBusy(version.revision)} onClick={() => void restore.run(version.revision, async () => {
						await api("/admin/defense/policy/draft", "POST", { text: JSON.stringify(version.document) });
						void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts();
					}, "恢复草稿已生成，确认后生效")}>预演恢复护栏</Button>
			</div>)}
		</Collapsible>
  </Section>;
}
