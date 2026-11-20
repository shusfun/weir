import { useQueryClient } from "@tanstack/react-query";
import { ActionGroup, Button, Collapsible, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, SwitchGroup, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Textarea } from "@weir/ui";
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
  const q = useQuery({ queryKey: ["ai-profile"], queryFn: () => api("/admin/ai/profile") as Promise<ProfileView> });
  useConsoleContext({ notices: ["AI 外置配置，修改必须完整预演并由管理员确认。"] });
  if (q.isPending || q.error) return <Section title="AI 偏好与观察点" description="选择 Laya 在后台盯哪些事。修改先预演完整差异，管理员确认后生效。"><State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /></Section>;
  return <AIProfileForm view={q.data} onRefresh={() => void q.refetch()} />;
}

function AIProfileForm({ view, onRefresh }: { view: ProfileView; onRefresh: () => void }) {
  const queryClient = useQueryClient();
  const history = useQuery({ queryKey: ["ai-profile-history"], queryFn: () => api("/admin/ai/profile/history") as Promise<{ items: History[] }> });
  const draft = useDraft(JSON.stringify(view.document, null, 2));
  const text = draft.value;
  let parsed: ProfileDocument | null = null;
  try {
    const candidate = JSON.parse(text) as ProfileDocument;
    if (candidate && typeof candidate === "object" && candidate.site && typeof candidate.site === "object") parsed = candidate;
  } catch {
    parsed = null;
  }
  const site = parsed?.site ?? view.document.site;
  const effectiveTimezone = site.timezone ?? parsed?.external?.timezone ?? view.effective.timezone ?? "UTC";
  const effectiveCompat = site.laya_compatibility ?? parsed?.external?.laya_compatibility ?? view.effective.laya_compatibility ?? { verified_range: "", out_of_range: "base_only" as const };
  const effectiveObs: Record<string, Observation> = {
    ...(view.effective.observations ?? {}),
    ...(parsed?.external?.observations ?? {}),
    ...(site.observations ?? {}),
  };

  const updateSite = (mutator: (nextSite: Layer) => void) => {
    const base = parsed ?? view.document;
    const nextSite: Layer = { ...base.site, schema: base.site.schema || 1 };
    mutator(nextSite);
    draft.set(JSON.stringify({ ...base, site: nextSite }, null, 2));
  };

  const patchObservation = (name: string, patch: Partial<Observation>) => {
    const current = effectiveObs[name];
    if (!current) return;
    updateSite((nextSite) => {
      nextSite.observations = { ...(nextSite.observations ?? {}), [name]: { ...current, ...patch } };
    });
  };

  const resetObservation = (name: string) => {
    updateSite((nextSite) => {
      if (!nextSite.observations) return;
      const copy = { ...nextSite.observations };
      delete copy[name];
      nextSite.observations = Object.keys(copy).length ? copy : undefined;
    });
  };

  useConsoleContext({ form: [{ key: "ai_profile_document", label: "待提交的配置文件", secret: true, filled: draft.changed }] });
  const saved = () => { void queryClient.invalidateQueries({ queryKey: ["ai-profile"] }); void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts(); };
  const submit = useFormAction(() => api("/admin/ai/profile/draft", "POST", { text }), {
    ok: "已提交草稿，确认后才生效", fields: { ai_profile_invalid: "text" }, onSuccess: () => { draft.commit(); saved(); },
  });
  const rollback = useRowAction<string>();

  // 主界面只回答「哪些观察点要开」；时区、兼容策略、置信度、三层来源与完整 JSON 收进高级选项
  const observationRows = Object.entries(effectiveObs);
  return <Section title="AI 偏好与观察点" description="选择 Laya 在后台盯哪些事。修改先预演完整差异，管理员确认后生效。">
    <div className="stack" data-tour="ai-profile">
      <Form busy={submit.busy} errors={submit.errors} onSubmit={() => void submit.submit()}>
        <SwitchGroup>
          {observationRows.map(([name, value]) => <SwitchField key={name} placement="end" label={observationNames[name] ?? name} description={name === "content.spam" ? "只做分类，不调用 dsh" : undefined} checked={value.enabled} disabled={!parsed} onCheckedChange={(enabled) => patchObservation(name, { enabled })} />)}
        </SwitchGroup>
        <Collapsible variant="section" title="高级选项" summary={`汇总时区 ${effectiveTimezone}`}>
          <div className="stack">
            <FormGrid>
              <Field label="每日汇总时区" hint="IANA 时区名，如 Asia/Shanghai、UTC">
                <Input disabled={!parsed} placeholder="Asia/Shanghai" value={effectiveTimezone} onChange={(e) => updateSite((s) => { s.timezone = e.target.value; })} />
              </Field>
              <Field label="Laya 超出已验证版本时" hint={`已验证范围：${effectiveCompat.verified_range || "未知"}`}>
                <Select value={effectiveCompat.out_of_range} disabled={!parsed} onValueChange={(v) => updateSite((s) => { s.laya_compatibility = { verified_range: effectiveCompat.verified_range, out_of_range: v as "base_only" | "reject" }; })}>
                  <SelectTrigger aria-label="Laya 超出已验证版本时"><SelectValue>{effectiveCompat.out_of_range === "reject" ? "拒绝调用 Laya" : "仅保留基础分类，暂停后台交给 dsh"}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="base_only">仅保留基础分类，暂停后台交给 dsh</SelectItem>
                    <SelectItem value="reject">拒绝调用 Laya</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </FormGrid>
            <Table>
              <TableHeader><TableRow><TableHead>观察点</TableHead><TableHead>置信度下限（0–1）</TableHead><TableHead>取值来源</TableHead></TableRow></TableHeader>
              <TableBody>
                {observationRows.map(([name, value]) => {
                  const inSite = Boolean(site.observations && Object.hasOwn(site.observations, name));
                  const src = inSite ? "site" : view.sources[`observations/${name}`];
                  return (
                    <TableRow key={name}>
                      <TableCell>{observationNames[name] ?? name}</TableCell>
                      <TableCell className="max-w-[141px]">
                        <Input type="number" min={0} max={1} step={0.05} aria-label={`${observationNames[name] ?? name} 置信度下限`} disabled={!parsed} value={value.confidence} onChange={(e) => patchObservation(name, { confidence: Number(e.target.value) })} />
                      </TableCell>
                      <TableCell>
                        <span className="inline-flex flex-wrap items-center gap-1.5">
                          <span className="text-meta text-[var(--weir-ink-3)]">{sourceNames[src] ?? "未知"}</span>
                          {inSite && <Button type="button" size="xs" variant="ghost" disabled={!parsed} onClick={() => resetObservation(name)}>恢复默认</Button>}
                        </span>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
            {!parsed && <Notice tone="warn">下方 JSON 语法有误，上面的控件暂时锁定；修正 JSON 或点「放弃编辑」即可恢复。</Notice>}
            <Field label="完整配置文件（JSON）" name="text" required hint="含提问词、判断标准（normal / suspicious / unknown）与 skills；不要放入密钥或订阅令牌。">
              <Textarea mono rows={10} required spellCheck={false} value={text} onChange={(e) => draft.set(e.target.value)} />
            </Field>
            <ActionGroup><span className="min-w-0 break-all font-mono text-meta">生效版本 {view.revision}</span><Button type="button" size="sm" variant="outline" onClick={() => { onRefresh(); void history.refetch(); }}>重新读取</Button></ActionGroup>
          </div>
        </Collapsible>
        <ActionGroup>
          <Button type="submit" disabled={!draft.changed || submit.busy} busy={submit.busy} busyText="正在预演…">预演并提交草稿</Button>
          {draft.changed && <Button type="button" variant="ghost" onClick={draft.reset}>放弃编辑</Button>}
        </ActionGroup>
        {submit.error && <Notice tone="bad">{submit.error}</Notice>}
      </Form>
      <Collapsible title="已确认的配置版本">
        {history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={() => history.refetch()} /> : history.data.items.length === 0 ? <State empty="还没有已确认的修改" /> : <div className="stack">
          {history.data.items.map((item) => <ActionGroup key={item.key}><span className="min-w-0">{item.data.created_at ? new Date(item.data.created_at).toLocaleString("zh-CN", { hour12: false }) : "确认时间未知"}{item.key === view.revision ? "（当前）" : ""}</span><Button size="sm" variant="outline" disabled={item.key === view.revision} busy={rollback.isBusy(item.key)} onClick={() => void rollback.run(item.key, async () => { await api("/admin/ai/profile/draft", "POST", { text: JSON.stringify(item.data.document) }); saved(); }, "恢复草稿已提交，确认后才生效")}>预演恢复</Button></ActionGroup>)}
        </div>}
      </Collapsible>
    </div>
  </Section>;
}
