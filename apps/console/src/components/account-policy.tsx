import { useQueryClient } from "@tanstack/react-query";
import { ActionGroup, Button, Collapsible, Field, Form, Notice, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction, useRowAction } from "../lib/action";
import { useDraft } from "../lib/dirty";
import { useConsoleContext } from "../lib/console-context";
import { reopenDrafts } from "./draft-confirm";
import { State } from "./feedback";
import { Section } from "./system-settings";

type Values = Record<string, unknown>;
type Document = { schema: number; external: Values; site: Values };
type View = { revision: string; document: Document; effective: Values; sources: Record<string, string> };
type Version = { key: string; data: { document: Document } };
const names: Record<string, string> = {
	invite_required: "注册必须邀请码", redeem_enabled: "允许兑换套餐码", access_code_attempts: "每级用码失败次数", access_code_window_seconds: "用码失败计数窗口（秒）", access_code_cooldown_seconds: "用码锁定三级（秒）",
  human_provider: "注册与邮件验证服务（登录固定本地 ALTCHA）",
  human_challenge_ip_ten_minutes: "每 IP 十分钟挑战上限", human_challenge_site_minute: "全站每分钟挑战上限",
  human_pow_cost: "ALTCHA 每次推导成本", human_pow_counter: "ALTCHA 最大搜索次数", human_lifetime_seconds: "人机挑战有效期（秒）", human_min_seconds: "最短填写时间（秒）", human_login_failures: "登录失败后要求验证的次数",
  code_lifetime_seconds: "验证码有效期（秒）", code_attempts: "每码最多尝试次数", code_cooldown_seconds: "重发冷却三级（秒）",
  mail_email_hour: "每邮箱每小时", mail_ip_hour: "每 IP 每小时", mail_network_hour: "每网段每小时（IPv4 /24、IPv6 /64）", mail_domain_hour: "每邮件域名每小时", mail_site_hour: "全站每小时",
  email_allow_suffixes: "允许注册的邮箱后缀", email_deny_suffixes: "禁止注册的邮箱后缀", email_disposable_exceptions: "一次性邮箱例外", email_disposable_additions: "额外一次性邮箱域名", email_alias_rules: "邮箱别名规则",
  email_disposable_snapshot: "内置一次性邮箱名单摘要（只读）",
};
const sources: Record<string, string> = { default: "内置默认", external: "外部导入", site: "站点覆盖" };

export function AccountPolicyCard() {
  const client = useQueryClient();
  const q = useQuery({ queryKey: ["account-policy"], queryFn: () => api("/admin/settings/account-policy") as Promise<View> });
  const history = useQuery({ queryKey: ["account-policy-history"], queryFn: () => api("/admin/settings/account-policy/history") as Promise<{ items: Version[] }> });
  const draft = useDraft(q.data ? JSON.stringify(q.data.document, null, 2) : "");
  const text = draft.value;
  useConsoleContext({ notices: ["账户发码策略按三层覆盖，所有修改需管理员确认；配置不能改变原子计数和失败拒绝机制。"], form: [{ key: "account_policy", label: "待提交的账户策略", secret: true, filled: draft.changed }] });
  const submitted = () => { void client.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts(); };
  const submit = useFormAction(() => api("/admin/settings/account-policy/draft", "POST", { text }), {
    ok: "已提交草稿，确认后才生效", fields: { account_policy_invalid: "text" }, onSuccess: () => { draft.reset(); submitted(); },
  });
  const rollback = useRowAction<string>();
  return <Section title="账户验证与发信策略" description="内置默认 → 外部导入 → 站点覆盖；版本化配置即时生效，支持预演和恢复。">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <div className="stack" data-tour="account-policy">
      <ActionGroup><span className="min-w-0 break-all font-mono text-meta">生效版本 {q.data.revision}</span><Button size="sm" variant="outline" onClick={() => { void q.refetch(); void history.refetch(); }}>刷新策略状态</Button></ActionGroup>
      <Table><TableHeader><TableRow><TableHead>策略项</TableHead><TableHead>生效值</TableHead><TableHead>来源</TableHead></TableRow></TableHeader><TableBody>
        {Object.entries(q.data.effective).map(([key, value]) => <TableRow key={key}><TableCell>{names[key] ?? key}</TableCell><TableCell className="break-all">{Array.isArray(value) ? value.length === 0 ? "空" : value.map((v) => typeof v === "object" ? JSON.stringify(v) : String(v)).join(" / ") : String(value)}</TableCell><TableCell>{sources[q.data.sources[key]] ?? "未知"}</TableCell></TableRow>)}
      </TableBody></Table>
      <Notice>发码尝试按 UTC 整点小时计数，包含不存在账户和冷却中的请求。错误猜码不延长有效期；存储故障时拒绝。已有验证码保留发放时的有效期和尝试上限。</Notice>
      <Notice>注册与注册发码都会检查邮箱后缀、一次性邮箱和 MX。黑名单优先，白名单为空表示不限；@域名只匹配自身，*.域名只匹配子域名。别名只用于防止重复注册，不改登录或收信地址。</Notice>
      <Notice>注册、发码和密码重置默认使用本地 ALTCHA；配置密钥后，可将 human_provider 改为 turnstile。登录连续失败后始终用本地 ALTCHA。挑战按操作和 IP 绑定、仅可使用一次；服务端检查最短填写时间和蜜罐，验证故障时拒绝操作。</Notice>
      <Form busy={submit.busy} errors={submit.errors} onSubmit={() => void submit.submit()}>
        <Field label="账户策略文件（JSON）" name="text" required hint="删除 site 中的字段以恢复继承；external 用于导入外部配置。只接受已登记字段、名单及边界内的数值，不接受密钥或可执行内容。">
          <Textarea mono rows={8} required spellCheck={false} value={text} onChange={(e) => draft.set(e.target.value)} />
        </Field>
        <ActionGroup>
          <Button type="submit" busy={submit.busy} busyText="正在预演…">预演账户策略</Button>
          <Button type="button" variant="outline" onClick={() => draft.set(JSON.stringify({ ...q.data.document, site: q.data.effective }, null, 2))}>以生效策略编辑站点层</Button>
          <Button type="button" variant="ghost" disabled={!draft.changed} onClick={draft.reset}>放弃编辑</Button>
        </ActionGroup>
        {submit.error && <Notice tone="bad">{submit.error}</Notice>}
      </Form>
      <Collapsible title="账户策略版本">
        {history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={() => history.refetch()} /> : history.data.items.length === 0 ? <State empty="还没有已确认的账户策略修改" /> : <div className="stack">
          {history.data.items.map((v) => <ActionGroup key={v.key}><span className="min-w-0 break-all font-mono text-meta">{v.key}</span><Button size="sm" variant="outline" disabled={v.key === q.data.revision} busy={rollback.isBusy(v.key)} onClick={() => void rollback.run(v.key, async () => { await api("/admin/settings/account-policy/draft", "POST", { text: JSON.stringify(v.data.document) }); submitted(); }, "恢复草稿已提交，确认后生效")}>预演恢复账户策略</Button></ActionGroup>)}
        </div>}
      </Collapsible>
    </div>}
  </Section>;
}
