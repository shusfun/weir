import { useQueryClient } from "@tanstack/react-query";
import { ActionGroup, Button, Collapsible, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Textarea } from "@weir/ui";
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
  invite_required: "注册必须邀请码",
  redeem_enabled: "允许兑换套餐码",
  access_code_attempts: "每级用码失败次数",
  access_code_window_seconds: "用码失败计数窗口（秒）",
  access_code_cooldown_seconds: "用码锁定三级（秒）",
  human_provider: "注册与邮件人机验证服务",
  human_challenge_ip_ten_minutes: "每 IP 十分钟挑战上限",
  human_challenge_site_minute: "全站每分钟挑战上限",
  human_pow_cost: "ALTCHA 每次推导成本",
  human_pow_counter: "ALTCHA 最大搜索次数",
  human_lifetime_seconds: "人机挑战有效期（秒）",
  human_min_seconds: "最短填写时间（秒）",
  human_login_failures: "登录失败后要求验证的次数",
  code_lifetime_seconds: "验证码有效期（秒）",
  code_attempts: "每码最多尝试次数",
  code_cooldown_seconds: "重发冷却三级（秒）",
  mail_email_hour: "每邮箱每小时发码上限",
  mail_ip_hour: "每 IP 每小时发码上限",
  mail_network_hour: "每网段每小时发码上限",
  mail_domain_hour: "每邮件域名每小时发码上限",
  mail_site_hour: "全站每小时发码上限",
  email_allow_suffixes: "允许注册的邮箱后缀",
  email_deny_suffixes: "禁止注册的邮箱后缀",
  email_disposable_exceptions: "一次性邮箱例外",
  email_disposable_additions: "额外一次性邮箱域名",
  email_alias_rules: "邮箱别名规则",
  email_disposable_snapshot: "内置一次性邮箱名单摘要（只读）",
};
const toLines = (text: string) => text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
const asLines = (val: unknown) => (Array.isArray(val) ? val.map(String).join("\n") : "");
const asNum = (val: unknown, fallback: number) => (typeof val === "number" && Number.isFinite(val) ? val : fallback);

export function AccountPolicyCard() {
  const q = useQuery({ queryKey: ["account-policy"], queryFn: () => api("/admin/settings/account-policy") as Promise<View> });
  useConsoleContext({ notices: ["账户发码策略按三层覆盖，所有修改需管理员确认；配置不能改变原子计数和失败拒绝机制。"] });
  if (q.isPending || q.error) return <Section title="注册与账号策略" description="修改先预演完整差异，管理员确认后生效。"><State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /></Section>;
  return <AccountPolicyForm view={q.data} onRefresh={() => void q.refetch()} />;
}

function AccountPolicyForm({ view, onRefresh }: { view: View; onRefresh: () => void }) {
  const client = useQueryClient();
  const history = useQuery({ queryKey: ["account-policy-history"], queryFn: () => api("/admin/settings/account-policy/history") as Promise<{ items: Version[] }> });
  const draft = useDraft(JSON.stringify(view.document, null, 2));
  const text = draft.value;
  let parsed: Document | null = null;
  try {
    const candidate = JSON.parse(text) as Document;
    if (candidate && typeof candidate === "object" && candidate.site && typeof candidate.site === "object") parsed = candidate;
  } catch {
    parsed = null;
  }
  const site = parsed?.site ?? view.document.site;
  const effective: Values = { ...view.effective, ...(parsed?.external ?? {}), ...site };
  const patchSite = (patch: Values) => {
    const base = parsed ?? view.document;
    draft.set(JSON.stringify({ ...base, site: { ...base.site, ...patch } }, null, 2));
  };
  const resetSiteKey = (key: string) => {
    const base = parsed ?? view.document;
    const nextSite = { ...base.site };
    delete nextSite[key];
    draft.set(JSON.stringify({ ...base, site: nextSite }, null, 2));
  };
  useConsoleContext({ form: [{ key: "account_policy", label: "待提交的账户策略", secret: true, filled: draft.changed }] });
  const submitted = () => { void client.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts(); };
  const submit = useFormAction(() => api("/admin/settings/account-policy/draft", "POST", { text }), {
    ok: "已提交草稿，确认后才生效", fields: { account_policy_invalid: "text" }, onSuccess: () => { draft.commit(); submitted(); },
  });
  const rollback = useRowAction<string>();
  const overriddenKeys = Object.keys(site);

  // 主界面先回答「注册要不要邀请码、要不要开放兑换」；邮箱后缀单独一组，限额、人机验证、三层覆盖和 JSON 收进高级选项
  return <Section title="注册与账号策略" description="修改先预演完整差异，管理员确认后生效。">
    <div className="stack" data-tour="account-policy">
      <Form busy={submit.busy} errors={submit.errors} onSubmit={() => void submit.submit()}>
        <div className="settings-group">
          <SwitchField placement="end" label="注册必须填写邀请码" description="开启后访客自助注册需持有有效邀请码" checked={Boolean(effective.invite_required)} disabled={!parsed} onCheckedChange={(v) => patchSite({ invite_required: v })} />
          <SwitchField placement="end" label="允许用户兑换套餐码" description="开启后已登录用户可在订阅链接页填写兑换码" checked={Boolean(effective.redeem_enabled)} disabled={!parsed} onCheckedChange={(v) => patchSite({ redeem_enabled: v })} />
        </div>
        <Collapsible variant="section" title="邮箱限制" summary={asLines(effective.email_allow_suffixes) ? "只允许指定后缀" : "不限后缀"}>
          <FormGrid>
            <Field label="允许注册的邮箱后缀" hint="每行一项（如 @gmail.com 或 *.edu.cn）；留空表示不限">
              <Textarea rows={3} disabled={!parsed} placeholder="@gmail.com" value={asLines(effective.email_allow_suffixes)} onChange={(e) => patchSite({ email_allow_suffixes: toLines(e.target.value) })} />
            </Field>
            <Field label="禁止注册的邮箱后缀" hint="每行一项；优先于允许名单，一次性邮箱默认拦截">
              <Textarea rows={3} disabled={!parsed} placeholder="@example.org" value={asLines(effective.email_deny_suffixes)} onChange={(e) => patchSite({ email_deny_suffixes: toLines(e.target.value) })} />
            </Field>
          </FormGrid>
        </Collapsible>
        <Collapsible variant="section" title="高级选项" summary={overriddenKeys.length ? `已覆盖 ${overriddenKeys.length} 项默认值` : "全部使用默认值"}>
          <div className="stack">
            <FormGrid>
              <Field label="注册与邮件人机验证" hint="登录连续失败始终使用本地 ALTCHA；选 Turnstile 需先配置密钥">
                <Select value={String(effective.human_provider ?? "altcha")} disabled={!parsed} onValueChange={(v) => patchSite({ human_provider: String(v) })}>
                  <SelectTrigger aria-label="注册与邮件人机验证"><SelectValue>{effective.human_provider === "turnstile" ? "Cloudflare Turnstile" : "本地 ALTCHA"}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="altcha">本地 ALTCHA</SelectItem>
                    <SelectItem value="turnstile">Cloudflare Turnstile</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label="登录失败要求验证次数" hint="连续失败达到此次数后要求人机验证">
                <Input type="number" min={1} max={20} disabled={!parsed} value={asNum(effective.human_login_failures, 3)} onChange={(e) => patchSite({ human_login_failures: Number(e.target.value) })} />
              </Field>
              <Field label="邮箱验证码有效期（秒）" hint="60–1800 秒，默认 600 秒">
                <Input type="number" min={60} max={1800} disabled={!parsed} value={asNum(effective.code_lifetime_seconds, 600)} onChange={(e) => patchSite({ code_lifetime_seconds: Number(e.target.value) })} />
              </Field>
              <Field label="每邮箱每小时发码上限" hint="1–100 次">
                <Input type="number" min={1} max={100} disabled={!parsed} value={asNum(effective.mail_email_hour, 10)} onChange={(e) => patchSite({ mail_email_hour: Number(e.target.value) })} />
              </Field>
              <Field label="每 IP 每小时发码上限" hint="1–1000 次">
                <Input type="number" min={1} max={1000} disabled={!parsed} value={asNum(effective.mail_ip_hour, 30)} onChange={(e) => patchSite({ mail_ip_hour: Number(e.target.value) })} />
              </Field>
              <Field label="全站每小时发码上限" hint="1–1000000 次">
                <Input type="number" min={1} max={1000000} disabled={!parsed} value={asNum(effective.mail_site_hour, 1000)} onChange={(e) => patchSite({ mail_site_hour: Number(e.target.value) })} />
              </Field>
            </FormGrid>
            {overriddenKeys.length > 0 && <ul className="stack" aria-label="已覆盖默认值的项">{overriddenKeys.map((key) => <li key={key} className="row-wrap">
              <span>{names[key] ?? key}</span>
              <Button type="button" size="sm" variant="ghost" disabled={!parsed} onClick={() => resetSiteKey(key)}>恢复默认</Button>
            </li>)}</ul>}
            {!parsed && <Notice tone="warn">下方 JSON 语法有误，上面的控件暂时锁定；修正 JSON 或点「放弃编辑」即可恢复。</Notice>}
            <Field label="完整策略文件（JSON）" name="text" required hint="含冷却阶梯与别名规则；删除 site 中的字段即恢复默认，external 用于导入外部配置">
              <Textarea mono rows={8} required spellCheck={false} value={text} onChange={(e) => draft.set(e.target.value)} />
            </Field>
            <ActionGroup><span className="min-w-0 break-all font-mono text-meta">生效版本 {view.revision}</span><Button type="button" size="sm" variant="outline" onClick={() => { onRefresh(); void history.refetch(); }}>重新读取</Button></ActionGroup>
          </div>
        </Collapsible>
        <ActionGroup>
          <Button type="submit" disabled={!draft.changed || submit.busy} busy={submit.busy} busyText="正在预演…">预演并提交确认</Button>
          {draft.changed && <Button type="button" variant="ghost" onClick={draft.reset}>放弃编辑</Button>}
        </ActionGroup>
        {submit.error && <Notice tone="bad">{submit.error}</Notice>}
      </Form>
      <Collapsible title="已确认的账户策略版本">
        {history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={() => history.refetch()} /> : history.data.items.length === 0 ? <State empty="还没有已确认的账户策略修改" /> : <div className="stack">
          {history.data.items.map((v) => <ActionGroup key={v.key}><span className="min-w-0 break-all font-mono text-meta">{v.key}</span><Button size="sm" variant="outline" disabled={v.key === view.revision} busy={rollback.isBusy(v.key)} onClick={() => void rollback.run(v.key, async () => { await api("/admin/settings/account-policy/draft", "POST", { text: JSON.stringify(v.data.document) }); submitted(); }, "恢复草稿已提交，确认后生效")}>预演恢复</Button></ActionGroup>)}
        </div>}
      </Collapsible>
    </div>
  </Section>;
}
