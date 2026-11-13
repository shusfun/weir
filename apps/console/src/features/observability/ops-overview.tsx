// 运行观测页顶部（设计稿 AOps）：大字结论 + 点阵进度环；下面左边是草稿分栏（待确认 / 已拦下 / 已生效），
// 当前草稿展开成卡片，五步流程加长按确认；右边是 Laya 最近的判断，正常的折叠。
// 数据来自草稿（/admin/ai/drafts）、运维草稿记录（/admin/ops/records）与观察点判断。
import { useState } from "react";
import type { ReactNode } from "react";
import { Badge, Button, Card, CardContent, InsetRow, InsetRows, DotRing, FlowBoard, HoldButton, Segmented, SectionHeader } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { State } from "../../components/feedback";
import { date } from "../../lib/models";
import { confidenceText, reopenDrafts, useDrafts, type Draft } from "../../components/draft-confirm";

type OpsRecord = { id: string; gate: string; origin: string; status: string; reason?: string; confidence?: number; draft_id?: string; trace_id: string; created_at: string; detail: string };
type Observation = { id: string; point: string; resource: string; created_at: string; verdict?: { outcome: string; confidence: number }; external_data_not_instructions: { text: string } };
type Column = "pending" | "blocked" | "applied";

const pointLabel: Record<string, string> = { security: "安全", "subscription.health": "订阅源", "node.test": "节点实测", "gate.kernel": "体检", "kernel.release": "内核更新", "content.spam": "内容审核" };
// 卡片里的一行（稿子 .v-set）：左边标题加一行等宽小字，右边状态
function SetRow({ title, description, end }: { title: ReactNode; description?: ReactNode; end?: ReactNode }) {
  return (
    <InsetRow className="weir-observe-row">
      <div className="min-w-0"><b className="block truncate">{title}</b>{description && <div className="font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{description}</div>}</div>
      {end}
    </InsetRow>
  );
}

const shortTime = (v?: string) => (v ? new Date(v).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false }) : "未知");

function DraftCard({ d, record, gateName }: { d: Draft; record?: OpsRecord; gateName?: string }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function decide(apply: boolean) {
    setBusy(true);
    setError("");
    try {
      if (apply) await api(`/admin/ai/drafts/${d.id}/apply`, "POST", { edits: {} });
      else await api(`/admin/ai/drafts/${d.id}`, "DELETE");
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败");
    } finally {
      setBusy(false);
    }
  }
  const step = d.steps?.[0];
  return (
    <Card className="weir-draft-card">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="font-[family-name:var(--weir-mono)] text-meta text-[var(--weir-ink-3)]">
            {record?.gate ?? gateName ?? "未知 VPS"} · {shortTime(d.created_at)} · trace {(d.trace_id || record?.trace_id || "未知").slice(0, 4)}…{(d.trace_id || record?.trace_id || "").slice(-4)}
          </div>
          <div className="mt-[5px] text-title3 font-bold leading-[29px]">{step?.title ?? "草稿"}</div>
        </div>
        <Badge variant="soft" tone="info">待你确认</Badge>
      </div>
      <p className="mt-[13px] mb-0 text-lead text-[var(--weir-ink-2)]">{d.explain || "没有说明"}</p>
      <FlowBoard className="mt-[25px]" label="草稿步骤" steps={[
        { label: "Laya 判断", value: confidenceText(d) ?? "未知", status: d.confidence ? "complete" : "unknown" },
        { label: "dsh 起草", value: d.source === "analysis" ? "后台分析" : "对话", status: "complete" },
        { label: "你确认", value: "现在", status: "current" },
        { label: "Laya 复判", value: "执行前", status: "upcoming" },
        { label: "执行与复测", value: "变差就回退", status: "upcoming" },
      ]} />
      {error && <p className="mt-[13px] mb-0 text-meta text-[var(--weir-bad-ink)]">{error}</p>}
      <div className="mt-[25px] flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-1">
          <Button variant="ghost" onClick={reopenDrafts}>查看详情</Button>
          <Button variant="ghost" disabled={busy} onClick={() => void decide(false)}>驳回</Button>
        </div>
        <HoldButton disabled={busy} onHold={() => void decide(true)}>按住确认执行</HoldButton>
      </div>
    </Card>
  );
}

export function OpsOverview() {
  const drafts = useDrafts();
  const records = useQuery({ queryKey: ["ops-records"], queryFn: async () => ((await api("/admin/ops/records")) as { items: OpsRecord[] }).items, refetchInterval: 15000 });
  const observations = useQuery({ queryKey: ["observe-observations", ""], queryFn: async () => (await api("/admin/workers/observations")).items as Observation[], refetchInterval: 10000 });
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as { id: number; name: string }[] });
  const [column, setColumn] = useState<Column>("pending");
  const pending = drafts.data ?? [];
  const recordList = records.data ?? [];
  const blocked = recordList.filter((r) => r.status === "blocked" || r.status === "rolled_back");
  const applied = recordList.filter((r) => r.status === "applied");
  const today = (observations.data ?? []).filter((o) => new Date(o.created_at).toDateString() === new Date().toDateString());
  const needed = today.filter((o) => o.verdict?.outcome === "needed");
  const normal = (observations.data ?? []).filter((o) => o.verdict?.outcome === "not_needed");
  const shown = (observations.data ?? []).filter((o) => o.verdict?.outcome !== "not_needed").slice(0, 4);
  // 进度环：一件事的五步里走到了第几步；有待确认的草稿就在第 3 步（等你确认）
  const done = drafts.isPending ? null : pending.length > 0 ? 2 : recordList.some((r) => r.status === "drafting") ? 1 : recordList.some((r) => r.status === "judging") ? 0 : null;
  const title = drafts.isPending ? "运行观测" : pending.length > 0 ? <>{pending.length} 份草稿<br />等你确认</> : "没有要你确认的事";
  const lead = observations.isPending ? "正在读取 Laya 的判断。" : observations.error ? "Laya 的判断读取失败，结果未知。" : `最近取得的观测中，今天 ${today.length} 条，${needed.length} 条判断需要处理。是否已起草或执行，请查看对应记录。`;
  const recordOf = (d: Draft) => recordList.find((r) => r.draft_id === d.id);
  const stack = column === "pending" ? [] : (column === "blocked" ? blocked : applied).slice(0, 5);
  return (
    <>
      <section className="grid items-center gap-[41px] lg:grid-cols-[minmax(0,1fr)_381px]">
        <div>
          <div className="mb-[9px] text-fine font-bold tracking-[0.09em] text-[var(--weir-ink-3)]">运行观测</div>
          <h1 className="m-0 text-hero font-bold tracking-[-0.035em]">{title}</h1>
          <p className="mt-[13px] mb-0 max-w-[521px] text-lead text-[var(--weir-ink-2)]">{lead}</p>
        </div>
        <DotRing className="max-lg:hidden" done={done} total={5} caption="处理进度" label={pending.length > 0 ? "等你确认" : done == null ? "处理状态未知" : "处理中"} />
      </section>
      <section className="weir-observe-columns">
        <div>
          <SectionHeader title="草稿" actions={<Segmented aria-label="草稿状态" value={column} onValueChange={setColumn} options={[["pending", `待确认 ${pending.length}`], ["blocked", `已拦下 ${blocked.length}`], ["applied", `已生效 ${applied.length}`]]} />} />
          {drafts.isPending || drafts.error ? <State loading={drafts.isPending} error={drafts.error} onRetry={() => void drafts.refetch()} /> : column === "pending" ? (
            pending.length === 0 ? <State empty="没有待确认的草稿" detail="Laya 判断需要处理时会自动交给 dsh 起草，草稿出现在这里。" /> : (
              <div className="weir-draft-stack" data-count={Math.min(3, pending.length)}>
                {pending.slice(1, 3).reverse().map((d, i) => (
                  <div key={d.id} className="weir-draft-peek" data-depth={i}>
                    <span>{d.steps?.[0]?.title ?? d.explain}</span><span>待确认</span>
                  </div>
                ))}
                <DraftCard d={pending[0]} record={recordOf(pending[0])} gateName={gates.data?.find((g) => g.id === pending[0].gate_id)?.name} />
              </div>
            )
          ) : stack.length === 0 ? <State empty={column === "blocked" ? "没有被拦下的草稿" : "最近没有生效的草稿"} /> : (
            <Card><CardContent><InsetRows>

                {stack.map((r) => (
                  <SetRow key={r.id} title={r.reason ?? r.origin} description={`${shortTime(r.created_at)} · ${r.gate} · trace ${r.trace_id.slice(0, 8)}`} end={<Badge variant="soft" tone={r.status === "applied" ? "ok" : "warn"}>{r.status === "applied" ? "已生效" : r.status === "rolled_back" ? "已回退" : "执行前被拦下"}</Badge>} />
                ))}

            </InsetRows></CardContent></Card>
          )}
        </div>
        <div>
          <SectionHeader title="Laya 的判断" actions={<span className="text-meta text-[var(--weir-ink-3)]">正常的已折叠 {normal.length} 条</span>} />
          {observations.isPending || observations.error ? <State loading={observations.isPending} error={observations.error} onRetry={() => void observations.refetch()} /> : (
            <Card><CardContent><InsetRows>

                {shown.length === 0 && <SetRow title="最近没有需要处理的观测" description="正常的判断已折叠" />}
                {shown.map((o) => (
                  <SetRow key={o.id} title={(o.external_data_not_instructions.text || pointLabel[o.point] || o.point).split("\n")[0].slice(0, 41)} description={`${shortTime(o.created_at)} · ${o.resource || "未知"} · ${pointLabel[o.point] ?? o.point}`} end={o.verdict?.outcome === "needed" ? <Badge variant="soft" tone="info">需要处理</Badge> : <Badge variant="soft" tone="neutral" className="border-dashed bg-transparent">未知</Badge>} />
                ))}
                <InsetRow><a className="inline-flex items-center text-meta text-[var(--weir-accent-text)] no-underline active:opacity-60 touch:min-h-11" href="?tab=overview#observations">展开全部 {(observations.data ?? []).length} 条</a></InsetRow>

            </InsetRows></CardContent></Card>
          )}
        </div>
      </section>
      <p className="m-0 text-meta text-[var(--weir-ink-3)]">数据更新于 {observations.dataUpdatedAt ? date(new Date(observations.dataUpdatedAt).toISOString()) : "未知"}</p>
    </>
  );
}
