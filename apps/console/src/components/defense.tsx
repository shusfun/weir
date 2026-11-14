// 安全防御：Laya 判断安全事件 → 可疑事件交给 dsh 分析 → dsh 只能提交防御草稿，由 Weir 弹窗确认后封禁。
// 规则防线（限流、失败登录计数）不依赖任何模型；Laya 或 dsh 不可用时事件照常记录，判断显示为「未知」。
import { useState } from "react";
import { useQuery } from "../lib/query";
import { Badge, Button, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, PasswordInput } from "@weir/ui";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { ActionsWait } from "./wait";
import { date } from "../lib/models";
import { State } from "./feedback";
import { Section } from "./system-settings";
import { useConsoleContext } from "../lib/console-context";
import { useDraft, useUnsavedChanges } from "../lib/dirty";
import { LayaHosting } from "../features/ai/laya-hosting";
import { GateLayaHosting } from "../features/ai/laya-gate-hosting";
import { OpsRecords } from "../features/ai/ops-records";
import { LayaDiscovery } from "../features/ai/laya-discovery";

interface LayaView {
	mode?: "center" | "gate";
	gate_id?: number;
  enabled: boolean;
  verified?: boolean;
  url: string;
  model: string;
  key_set: boolean;
  warmup_seconds?: number;
  compatibility?: { installed_version?: string; verified_range: string; verified: boolean; enable_background: boolean; code?: string; reason?: string };
}

type Verdict = { verdict: { category: string; confidence: number } };

export function LayaCard({ hosting = true }: { hosting?: boolean }) {
  const q = useQuery({ queryKey: ["laya"], queryFn: () => api("/admin/defense/laya") as Promise<LayaView> });
  const draft = useDraft({ mode: q.data?.mode ?? "center", gate_id: q.data?.gate_id ?? 0, enabled: q.data?.enabled ?? false, url: q.data?.url ?? "", model: q.data?.model ?? "", warmup_seconds: q.data?.warmup_seconds ?? 300 });
  const setDraft = draft.set;
  const [key, setKey] = useState("");
  const v = draft.value;
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as { id: number; name: string; online: boolean }[] });
  useUnsavedChanges(key !== "");
  useConsoleContext({ notices: ["Laya 接入设置；判断规则在 AI 外置配置中版本化维护。"], form: [{ key: "laya_key", label: "Laya API Key", secret: true, filled: key !== "" }] });
  const save = useFormAction(() => api("/admin/defense/laya", "PUT", { mode: v?.mode, gate_id: v?.gate_id, enabled: v?.enabled, url: v?.url, model: v?.model, warmup_seconds:v?.warmup_seconds??300, key }), {
    ok: "已保存",
    fields: { invalid_laya_url: "url", invalid_laya_gate: "gate_id", invalid_laya_settings: "model" },
    onSuccess: () => {
      draft.commit();
      setKey("");
      void q.refetch();
    },
  });
  const { run, isBusy, waitFor } = useRowAction();
  const test = () =>
    run("test", async (track) => {const result=await api("/admin/defense/laya/test-async", "POST", undefined, track); void q.refetch(); return result;}, (r) => {
      const { verdict } = r as Verdict;
      return `${verdict.category === "unknown" ? "Laya 判断未知" : "Laya 测试通过"}：合成事件判为「${verdicts[verdict.category] ?? verdict.category}」`;
    }, { detail: (r) => (r as Verdict).verdict.category === "unknown" ? "判断未知，请查看活动记录" : `置信度 ${Math.round((r as Verdict).verdict.confidence * 100)}%`, fail: "Laya 测试未通过", wait: { label: "正在请 Laya 判断一条合成事件", max: v?.warmup_seconds ?? 300 } });
  return (
    <Section title="Laya 安全观察" description="接入官方 ollaya 原生接口。中心可直连已有实例；VPS 模式通过任务通道在指定 VPS 上判断，离线时显示未知。判断问题与可见置信度由 AI 外置配置管理。">
      {q.isPending || q.error || !v ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <div className="settings-group">
            <SwitchField placement="end" label="启用 Laya 判断" checked={v.enabled} disabled={!v.enabled && (!q.data?.verified || draft.changed || key !== "")} onCheckedChange={(on) => setDraft({ ...v, enabled: on })} />
            {!v.enabled && <small>先保存接入配置并通过测试，再开启判断；部署不会自动启用后台分析。</small>}
          </div>
          {q.data?.enabled && <Notice tone={q.data.compatibility?.enable_background ? "info" : "warn"}>实际版本：{q.data.compatibility?.installed_version ?? "未知"}；已验证范围：{q.data.compatibility?.verified_range ?? "未知"}。{q.data.compatibility?.enable_background ? "版本已验证，后台分析仍受总开关和权限限制。" : q.data.compatibility?.reason ?? "版本状态未知，后台分析暂停。"}</Notice>}
          <FormGrid>
            <Field label="推理位置" name="mode"><Select value={v.mode ?? "center"} onValueChange={(mode) => setDraft({ ...v, enabled: false, mode: mode as "center" | "gate" })}><SelectTrigger aria-label="推理位置"><SelectValue>{v.mode === "gate" ? "指定 VPS" : "中心接入或托管"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="center">中心接入或托管</SelectItem><SelectItem value="gate">指定 VPS</SelectItem></SelectContent></Select></Field>
            <Field label="模型" name="model" required><Input required placeholder="laya:multilingual" value={v.model} onChange={(e) => setDraft({ ...v, enabled: false, model: e.target.value })} /></Field>
          <Field label="预热与手动测试时限（秒）" name="warmup_seconds" hint="30 至 1800 秒，默认 300 秒；手动测试包含排队与推理，后台判断仍使用短超时。"><Input required type="number" min={30} max={1800} value={v.warmup_seconds??300} onChange={e=>setDraft({...v,warmup_seconds:Number(e.target.value)})}/></Field>
          </FormGrid>
          {v.mode === "gate" ? <>
            {gates.isPending || gates.error ? <State loading={gates.isPending} error={gates.error} onRetry={() => gates.refetch()} /> : <Field label="执行 VPS" name="gate_id" required hint="目标需完成 Laya 部署批准与模型安装；不会改派到其他 VPS。"><Select value={v.gate_id ? String(v.gate_id) : ""} onValueChange={(id) => setDraft({ ...v, enabled: false, gate_id: Number(id) })}><SelectTrigger aria-label="执行 VPS"><SelectValue placeholder="选择 VPS">{gates.data?.find(g => g.id === v.gate_id)?.name ?? "选择 VPS"}</SelectValue></SelectTrigger><SelectContent>{gates.data.map((g) => <SelectItem key={g.id} value={String(g.id)}>{g.name}{!g.online && "（离线）"}</SelectItem>)}</SelectContent></Select></Field>}
          </> : <>
          <Field label="Laya 地址" name="url" required hint="HTTPS；本机回环允许 HTTP，自动补上 /v1/systemone。"><Input required type="url" placeholder="http://127.0.0.1:11435" value={v.url} onChange={(e) => setDraft({ ...v, enabled: false, url: e.target.value })} /></Field>
          <Field label="API Key" hint={q.data?.key_set ? "已保存；留空则沿用。" : "可选；官方回环实例可以不填。加密保存，不会再显示。"}>
            <PasswordInput autoComplete="off" value={key} onChange={(e) => { setKey(e.target.value); setDraft({ ...v, enabled: false }); }} />
          </Field>
          </>}
          <div className="actions">
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
            <Button type="button" variant="outline" disabled={isBusy() || save.busy || draft.changed || key !== ""} onClick={() => void test()} busy={isBusy()} busyText="正在测试…">测试</Button>
            {(draft.changed || key !== "") && <small>先保存，再测试当前接入设置。</small>}
            <ActionsWait wait={waitFor("test")} />
          </div>
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          {hosting && (v.mode !== "gate" ? <><LayaDiscovery url={v.url} model={v.model} apiKey={key} onAttached={() => { draft.reset(); setKey(""); void q.refetch(); }} /><LayaHosting onUse={(url, model) => setDraft({ ...v, url, model, enabled:false })} /></> : v.gate_id ? <GateLayaHosting key={v.gate_id} gateId={v.gate_id} onUse={()=>setDraft({...v,enabled:false})}/> : null)}
          <OpsRecords />
        </Form>
      )}
    </Section>
  );
}

interface SecurityEvent {
  id: number;
  kind: string;
  scope: string;
  source_ip: string;
  failed_logins_10m: number;
  status: string;
  created_at: string;
  laya?: { category: string; confidence: number; code?: string; profile_version?: string };
  laya_error?: string;
  analysis?: string;
}

const verdicts: Record<string, string> = { suspicious: "可疑", normal: "正常", unknown: "未知" };
const kinds: Record<string, string> = { "repeated-login-failure": "连续登录失败", "rate-limit": "触发限流" };
const statuses: Record<string, [string, "ok" | "warn" | "bad" | "neutral"]> = {
  new: ["待处理", "warn"],
  analyzing: ["dsh 分析中", "neutral"],
  drafted: ["等待确认", "warn"],
  blocked: ["已封禁", "ok"],
  dismissed: ["已忽略", "neutral"],
  no_action: ["dsh 认为无需处置", "ok"],
};

export function EventsCard({ dshReady }: { dshReady: boolean }) {
  const q = useQuery({ queryKey: ["defense-events"], queryFn: () => api("/admin/defense/events") as Promise<{ events: SecurityEvent[]; analyzing: string }>, refetchInterval: 5000 });
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  const act = (id: number, path: string, ok: string) => run(id, () => api(path, "POST"), ok);
  const [acting, setActing] = useState<"analyze" | "dismiss">("dismiss");
  return (
    <Section title="安全事件" description="规则发现的异常（连续登录失败、触发限流）。Laya 判为可疑后可以交给 dsh 分析；dsh 认为需要处置时，会弹窗请你确认封禁。">
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : q.data.events.length === 0 ? <State empty="还没有安全事件" /> : (
        // Table 自带横向覆盖式滚动区
          <Table>
            <TableHeader><TableRow><TableHead>时间</TableHead><TableHead>事件</TableHead><TableHead>来源</TableHead><TableHead>Laya</TableHead><TableHead>状态</TableHead><TableHead>操作</TableHead></TableRow></TableHeader>
            <TableBody>
              {q.data.events.map((e) => {
                const [label, tone] = statuses[e.status] ?? [e.status, "neutral"];
                return (
                  <TableRow key={e.id}>
                    <TableCell className="whitespace-nowrap text-meta">{date(e.created_at)}</TableCell>
                    <TableCell>{kinds[e.kind] ?? e.kind}<small className="block text-[var(--weir-ink-3)]">10 分钟内失败登录 {e.failed_logins_10m} 次</small></TableCell>
                    <TableCell><code className="text-meta">{e.source_ip || "未知"}</code></TableCell>
                    <TableCell>{e.laya ? <span title={[e.laya_error, e.laya.code, e.laya.profile_version].filter(Boolean).join(" · ")}><Badge variant="soft" tone={e.laya.category === "suspicious" ? "bad" : e.laya.category === "normal" ? "ok" : "neutral"}>{verdicts[e.laya.category]}{e.laya.category !== "unknown" && ` ${Math.round(e.laya.confidence * 100)}%`}</Badge></span> : <small className="text-[var(--weir-ink-3)]" title={e.laya_error}>未知</small>}</TableCell>
                    <TableCell><Badge variant="soft" tone={tone}>{label}</Badge>{e.analysis && <small className="mt-1 block max-w-[28ch] whitespace-pre-wrap break-words">{e.analysis}</small>}</TableCell>
                    <TableCell>
                      {(e.status === "new" || e.status === "no_action") && (
                        <div className="flex flex-wrap gap-1">
                          <Button size="xs" variant="outline" disabled={!dshReady || !!q.data.analyzing || isBusy()} busy={isBusy(e.id) && acting === "analyze"} busyText="正在交给 dsh…" title={dshReady ? undefined : "dsh 未运行"} onClick={() => { setActing("analyze"); void act(e.id, `/admin/defense/events/${e.id}/analyze`, "已交给 dsh 分析"); }}>让 dsh 分析</Button>
                          <Button size="xs" variant="ghost" disabled={isBusy()} busy={isBusy(e.id) && acting === "dismiss"} busyText="正在忽略…" onClick={() => { setActing("dismiss"); void act(e.id, `/admin/defense/events/${e.id}/dismiss`, "已忽略"); }}>忽略</Button>
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
      )}
    </Section>
  );
}

interface Block {
  key: string;
  ip: string;
  until: string;
  event_id: number;
  reason: string;
}

export function BlocksCard() {
  const q = useQuery({ queryKey: ["defense-blocks"], queryFn: async () => (await api("/admin/defense/blocks")).blocks as Block[], refetchInterval: 15000 });
  const { run, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const unblock = (b: Block) => run(b.key, () => api("/admin/defense/blocks/" + b.key, "DELETE"), `已解封 ${b.ip}`, { fail: "解封失败" });
  return (
    <Section title="当前封禁" description="只有你确认过的防御草稿会产生封禁，到期自动解除。VPS 的设备连接不受封禁影响。">
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : q.data.length === 0 ? <State empty="没有封禁中的来源" /> : (
        <ul className="stack">
          {q.data.map((b) => (
            <li key={b.key} className="flex flex-wrap items-center justify-between gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-2">
              <span><code>{b.ip}</code><small className="ml-2 text-[var(--weir-ink-3)]">到 {date(b.until)} · 事件 #{b.event_id}</small><small className="block whitespace-pre-wrap break-words">{b.reason}</small></span>
              <Button size="sm" variant="outline" disabled={isBusy()} busy={isBusy(b.key)} busyText="正在解封…" onClick={() => void unblock(b)}>解封</Button>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}
