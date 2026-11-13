import { ActionGroup, Button, Field, Form, Input, Notice, PasswordInput, Textarea } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { useDraft } from "../lib/dirty";
import { Section } from "./system-settings";
import { State } from "./feedback";

type Config = { site_key: string; secret_set: boolean; hostnames: string[]; configured: boolean };
export function TurnstileSettings() {
  const q = useQuery({ queryKey: ["turnstile-settings"], queryFn: () => api("/admin/settings/turnstile") as Promise<Config> });
  const draft = useDraft({ site_key: q.data?.site_key ?? "", secret: "", hosts: q.data?.hostnames?.join("\n") ?? "" });
  const value = draft.value, setDraft = draft.set;
  useConsoleContext({ form: [{ key: "turnstile_secret", label: "Turnstile Secret", secret: true, filled: !!value.secret || !!q.data?.secret_set }] });
  const save = useFormAction(() => api("/admin/settings/turnstile", "PUT", { site_key: value.site_key, secret: value.secret, hostnames: value.hosts.split(/\s+/).filter(Boolean) }), { ok: "已保存验证密钥", fields: { invalid_turnstile_settings: "site_key" }, onSuccess: () => { draft.commit({ ...value, secret: "" }); void q.refetch(); } });
  return <Section title="可选 Turnstile" description="仅管理员填写密钥；保存后，在账户策略中预演并确认 human_provider=turnstile 才会启用。登录始终用本地 ALTCHA。">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <Form data-tour="turnstile-settings" busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <Field label="Site Key" name="site_key"><Input value={value.site_key} onChange={(e) => setDraft({ ...value, site_key: e.target.value.trim() })} autoComplete="off" /></Field>
      <Field label="Secret" hint={q.data.secret_set ? "已设置，留空保留；密钥不返回页面或提供给 AI。" : "从 Cloudflare 控制台获取。"}><PasswordInput value={value.secret} onChange={(e) => setDraft({ ...value, secret: e.target.value })} autoComplete="new-password" /></Field>
      <Field label="允许的站点域名" hint="每行一个完整域名，不带协议或端口；服务器核验返回的域名及操作用途。"><Textarea rows={3} value={value.hosts} onChange={(e) => setDraft({ ...value, hosts: e.target.value })} /></Field>
      <Notice>{q.data.configured ? "密钥已配置；是否启用由账户策略决定。" : "尚未配置，不加载第三方验证。"} 服务不可用时拒绝操作。</Notice>
      <ActionGroup><Button type="submit" busy={save.busy} busyText="正在保存…">保存 Turnstile 密钥</Button><Button type="button" variant="ghost" disabled={!draft.changed} onClick={draft.reset}>放弃修改</Button></ActionGroup>
      {save.error && <Notice tone="bad">{save.error}</Notice>}
    </Form>}
  </Section>;
}
