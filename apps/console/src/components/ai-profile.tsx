import { useQueryClient } from "@tanstack/react-query";
import { ActionGroup, Button, Collapsible, Field, Form, Notice, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction, useRowAction } from "../lib/action";
import { useDraft } from "../lib/dirty";
import { reopenDrafts } from "./draft-confirm";
import { State } from "./feedback";
import { Section } from "./system-settings";
import { useConsoleContext } from "../lib/console-context";

type Observation = { enabled: boolean; confidence: number; question: string; skill?: string; criteria?: Record<"normal" | "suspicious" | "unknown", string> };
type Layer = { schema: number; timezone?: string; instructions?: string; plugin?: Record<string, string>; skills?: Record<string, string>; observations?: Record<string, Observation>; laya_compatibility?: { verified_range: string; out_of_range: "reject" | "base_only" } };
type ProfileDocument = { schema: number; external: Layer; site: Layer };
type ProfileView = { revision: string; document: ProfileDocument; effective: Layer; sources: Record<string, string> };
type History = { key: string; data: { document: ProfileDocument; created_at: string } };
const observationNames: Record<string, string> = { security: "安全与注册滥用", "subscription.health": "订阅源健康", "node.test": "节点实测", "gate.kernel": "VPS 与内核", "kernel.release": "内核发布说明", "content.spam": "公开内容垃圾判断" };
const sourceNames: Record<string, string> = { default: "内置默认", external: "外部导入", site: "站点覆盖" };

export function AIProfileCard() {
  const queryClient = useQueryClient();
  const q = useQuery({ queryKey: ["ai-profile"], queryFn: () => api("/admin/ai/profile") as Promise<ProfileView> });
  const history = useQuery({ queryKey: ["ai-profile-history"], queryFn: () => api("/admin/ai/profile/history") as Promise<{ items: History[] }> });
  const draft = useDraft(q.data ? JSON.stringify(q.data.document, null, 2) : "");
  const text = draft.value;
  useConsoleContext({ notices: ["AI 外置配置，修改必须完整预演并由管理员确认。"], form: [{ key: "ai_profile_document", label: "待提交的配置文件", secret: true, filled: draft.changed }] });
  const saved = () => { void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts(); };
  const submit = useFormAction(() => api("/admin/ai/profile/draft", "POST", { text }), {
    ok: "已提交草稿，确认后才生效", fields: { ai_profile_invalid: "text" }, onSuccess: () => { draft.reset(); saved(); },
  });
  const rollback = useRowAction<string>();
  return <Section title="AI 外置配置" description="指令、插件、skills 和判断配置按内置默认、外部导入、站点覆盖叠加。每次修改均先预演，再由管理员确认完整差异。">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <div className="stack" data-tour="ai-profile">
      <ActionGroup><span className="min-w-0 break-all font-mono text-meta">生效版本 {q.data.revision}</span><Button size="sm" variant="outline" onClick={() => { void q.refetch(); void history.refetch(); }}>刷新生效状态</Button></ActionGroup>
      <p>每日汇总时区：{q.data.effective.timezone ?? "未知"} · {sourceNames[q.data.sources.timezone] ?? "未知"}</p>
      <p>Laya 已验证范围：{q.data.effective.laya_compatibility?.verified_range ?? "未知"} · {sourceNames[q.data.sources.laya_compatibility] ?? "未知"} · 范围外：{q.data.effective.laya_compatibility?.out_of_range === "base_only" ? "仅基础能力，暂停后台分析" : q.data.effective.laya_compatibility?.out_of_range === "reject" ? "拒绝调用" : "未知"}</p>
      <Table><TableHeader><TableRow><TableHead>已登记观察点</TableHead><TableHead>置信度下限</TableHead><TableHead>配置来源</TableHead></TableRow></TableHeader><TableBody>
        {Object.entries(q.data.effective.observations ?? {}).map(([name, value]) => <TableRow key={name}><TableCell>{observationNames[name] ?? name}{!value.enabled && "（已停用）"}</TableCell><TableCell>{value.confidence}</TableCell><TableCell>{sourceNames[q.data.sources[`observations/${name}`]] ?? "未知"}</TableCell></TableRow>)}
      </TableBody></Table>
      <Notice>观察点由程序登记；配置不能新增采集入口。公开内容判断不会调用 dsh。配置已启用不代表对应流程已经采集或检测。</Notice>
      <Form busy={submit.busy} errors={submit.errors} onSubmit={() => void submit.submit()}>
        <Field label="版本化配置文件（JSON）" name="text" required hint="timezone 使用 IANA 时区；criteria 说明各答案的判断标准；laya_compatibility 设置已验证版本范围和范围外策略（base_only 或 reject）。不要放入密钥或订阅令牌。">
          <Textarea mono rows={12} required spellCheck={false} value={text} onChange={(e) => draft.set(e.target.value)} />
        </Field>
        <ActionGroup>
          <Button type="submit" busy={submit.busy} busyText="正在预演…">预演并提交草稿</Button>
          <Button type="button" variant="outline" onClick={() => draft.set(JSON.stringify({ ...q.data.document, site: q.data.effective }, null, 2))}>以生效配置编辑站点层</Button>
          <Button type="button" variant="ghost" disabled={!draft.changed} onClick={draft.reset}>放弃编辑</Button>
        </ActionGroup>
        {submit.error && <Notice tone="bad">{submit.error}</Notice>}
      </Form>
      <Collapsible title="已确认的配置版本">
        {history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={() => history.refetch()} /> : history.data.items.length === 0 ? <State empty="还没有已确认的修改" /> : <div className="stack">
          {history.data.items.map((item) => <ActionGroup key={item.key}><span className="min-w-0 break-all font-mono text-meta">{item.key}</span><Button size="sm" variant="outline" disabled={item.key === q.data.revision} busy={rollback.isBusy(item.key)} onClick={() => void rollback.run(item.key, async () => { await api("/admin/ai/profile/draft", "POST", { text: JSON.stringify(item.data.document) }); saved(); }, "恢复草稿已提交，确认后才生效")}>预演恢复</Button></ActionGroup>)}
        </div>}
      </Collapsible>
    </div>}
  </Section>;
}
