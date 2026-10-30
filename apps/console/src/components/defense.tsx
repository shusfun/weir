// 安全防御：Laya 判断安全事件 → 可疑事件交给 dsh 分析 → dsh 只能提交防御草稿，由 Weir 弹窗确认后封禁。
// 规则防线（限流、失败登录计数）不依赖任何模型；Laya 或 dsh 不可用时事件照常记录，判断显示为「未知」。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Field, Form, FormGrid, Input, Notice, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, PasswordInput } from "@weir/ui";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { State } from "./feedback";
import { emptyReauth, Reauth, Section, useReauthAction } from "./system-settings";

interface LayaView {
  enabled: boolean;
  url: string;
  model: string;
  key_set: boolean;
}

type Verdict = { verdict: { category: string; confidence: number } };

export function LayaCard() {
  const q = useQuery({ queryKey: ["laya"], queryFn: () => api("/admin/defense/laya") as Promise<LayaView> });
  const [draft, setDraft] = useState<LayaView | null>(null);
  const [key, setKey] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const v = draft ?? q.data;
  const save = useReauthAction(() => api("/admin/defense/laya", "PUT", { ...v, key, ...re }), {
    ok: "已保存",
    fields: { invalid_laya_url: "url" },
    onSuccess: () => {
      setDraft(null);
      setKey("");
      setRe(emptyReauth);
      void q.refetch();
    },
  });
  const { run, isBusy } = useRowAction();
  const test = () =>
    run("test", () => api("/admin/defense/laya/test", "POST"), (r) => {
      const { verdict } = r as Verdict;
      return `Laya 可用：合成事件判为「${verdicts[verdict.category] ?? verdict.category}」`;
    }, { detail: (r) => `置信度 ${Math.round((r as Verdict).verdict.confidence * 100)}%`, fail: "Laya 测试未通过" });
  return (
    <Section title="Laya 安全观察" description="Laya 只判断安全事件是否可疑（原生 /v1/systemone 接口）。发给它的事件只有类型与次数，不含来源地址、账号和日志原文。Key 加密保存，不会再显示。">
      {q.isPending || q.error || !v ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <SwitchField label="启用 Laya 判断" checked={v.enabled} onCheckedChange={(on) => setDraft({ ...v, enabled: on })} />
          <FormGrid>
            <Field label="Laya 地址" name="url" hint="HTTPS；会自动补上 /v1/systemone。"><Input type="url" placeholder="https://laya.example.com" value={v.url} onChange={(e) => setDraft({ ...v, url: e.target.value })} /></Field>
            <Field label="模型"><Input placeholder="observer" value={v.model} onChange={(e) => setDraft({ ...v, model: e.target.value })} /></Field>
          </FormGrid>
          <Field label="API Key" hint={v.key_set ? "已保存；留空则沿用。" : "加密保存，不会再显示。"}>
            <PasswordInput autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} />
          </Field>
          <Reauth value={re} onChange={setRe} />
          <div className="actions">
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
            <Button type="button" variant="outline" disabled={isBusy() || !q.data?.enabled} onClick={() => void test()} busy={isBusy()} busyText="正在测试…">测试</Button>
          </div>
          {save.error && <Notice tone="bad">{save.error}</Notice>}
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
  laya?: { category: string; confidence: number };
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
  return (
    <Section title="安全事件" description="规则发现的异常（连续登录失败、触发限流）。Laya 判为可疑后可以交给 dsh 分析；dsh 认为需要处置时，会弹窗请你确认封禁。">
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : q.data.events.length === 0 ? <State empty="还没有安全事件" /> : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader><TableRow><TableHead>时间</TableHead><TableHead>事件</TableHead><TableHead>来源</TableHead><TableHead>Laya</TableHead><TableHead>状态</TableHead><TableHead>操作</TableHead></TableRow></TableHeader>
            <TableBody>
              {q.data.events.map((e) => {
                const [label, tone] = statuses[e.status] ?? [e.status, "neutral"];
                return (
                  <TableRow key={e.id}>
                    <TableCell className="whitespace-nowrap text-xs">{date(e.created_at)}</TableCell>
                    <TableCell>{kinds[e.kind] ?? e.kind}<small className="block text-[var(--weir-ink-3)]">10 分钟内失败登录 {e.failed_logins_10m} 次</small></TableCell>
                    <TableCell><code className="text-xs">{e.source_ip || "未知"}</code></TableCell>
                    <TableCell>{e.laya ? <Badge variant="soft" tone={e.laya.category === "suspicious" ? "bad" : e.laya.category === "normal" ? "ok" : "neutral"}>{verdicts[e.laya.category]} {Math.round(e.laya.confidence * 100)}%</Badge> : <small className="text-[var(--weir-ink-3)]" title={e.laya_error}>未知</small>}</TableCell>
                    <TableCell><Badge variant="soft" tone={tone}>{label}</Badge>{e.analysis && <small className="mt-1 block max-w-[28ch] whitespace-pre-wrap break-words">{e.analysis}</small>}</TableCell>
                    <TableCell>
                      {(e.status === "new" || e.status === "no_action") && (
                        <div className="flex flex-wrap gap-1">
                          <Button size="xs" variant="outline" disabled={!dshReady || !!q.data.analyzing || isBusy(e.id)} title={dshReady ? undefined : "dsh 未运行"} onClick={() => void act(e.id, `/admin/defense/events/${e.id}/analyze`, "已交给 dsh 分析")}>让 dsh 分析</Button>
                          <Button size="xs" variant="ghost" disabled={isBusy(e.id)} onClick={() => void act(e.id, `/admin/defense/events/${e.id}/dismiss`, "已忽略")}>忽略</Button>
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
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
              <Button size="sm" variant="outline" disabled={isBusy(b.key)} onClick={() => void unblock(b)}>解封</Button>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}
