// Laya 运维闭环的草稿记录：体检、断线、实测、内核异常后系统自动判断与起草的每一次经过。
// 草稿本身仍在铃铛和确认窗里处理；这里只看来龙去脉，不提供绕过确认的执行入口。
import { Badge, Button, Collapsible, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { date } from "../../lib/models";
import { State } from "../../components/feedback";
import { ObjectLink } from "../../components/object-link";
import { reopenDrafts } from "../../components/draft-confirm";

type OpsRecord = {
  id: string;
  gate_id: number;
  gate: string;
  origin: string;
  detail: string;
  trace_id: string;
  status: string;
  reason?: string;
  confidence?: number;
  conclusion?: string;
  tool_calls?: string[];
  draft_id?: string;
  created_at: string;
};

const origins: Record<string, string> = { "gate.offline": "VPS 断线", "kernel.crash": "内核异常退出", "node.test": "节点实测失败", "host.health": "体检异常" };
type Tone = "neutral" | "ok" | "warn" | "bad" | "info";
const states: Record<string, [string, Tone]> = {
  judging: ["Laya 判断中", "info"],
  drafting: ["dsh 起草中", "info"],
  not_needed: ["不需要处理", "neutral"],
  no_draft: ["没有起草", "neutral"],
  unknown: ["未知", "warn"],
  pending: ["待确认", "warn"],
  expired: ["已过期", "neutral"],
  discarded: ["已放弃", "neutral"],
  blocked: ["复判拦下", "warn"],
  applied: ["已执行", "ok"],
  rolled_back: ["已回退", "bad"],
};

export function OpsRecords() {
  const q = useQuery({ queryKey: ["ops-records"], queryFn: async ({ signal }) => ((await api("/admin/ops/records", "GET", undefined, { signal })) as { items: OpsRecord[] }).items });
  return (
    <Collapsible title="运维草稿记录" data-tour="ops-records">
      <p className="text-meta text-[var(--weir-ink-3)]">断线、内核异常、实测失败或体检发现 DNS 污染时，系统先让 Laya 判断，需要处理才让 dsh 起草宿主修复。草稿要你确认；执行前 Laya 复判，执行后复测，变差自动回退。同一台 VPS 同一原因 30 分钟内只处理一次，记录保留 7 天。</p>
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : q.data.length === 0 ? <State empty="还没有自动判断的记录" /> : (
        <Table>
          <TableHeader><TableRow><TableHead>时间与原因</TableHead><TableHead>结果</TableHead><TableHead>dsh 结论</TableHead></TableRow></TableHeader>
          <TableBody>
            {q.data.map((r) => {
              const [label, tone] = states[r.status] ?? ["未知", "warn"];
              return (
                <TableRow key={r.id}>
                  <TableCell>
                    {date(r.created_at)} · {origins[r.origin] ?? r.origin}
                    <small className="block"><ObjectLink to={{ type: "gate", id: r.gate_id, name: r.gate }} /></small>
                    <small className="block break-all text-meta">{r.detail}</small>
                  </TableCell>
                  <TableCell>
                    <Badge variant="soft" tone={tone}>{label}</Badge>
                    {r.reason && <small className="block text-meta">{r.reason}</small>}
                    {r.confidence ? <small className="block text-meta">Laya 置信度 {Math.round(r.confidence * 100)}%</small> : null}
                    {r.status === "pending" && <Button variant="ghost" size="sm" onClick={reopenDrafts}>打开确认窗</Button>}
                  </TableCell>
                  <TableCell>
                    {r.conclusion ? <Collapsible title="查看对话结论"><p className="whitespace-pre-wrap">{r.conclusion}</p>{(r.tool_calls?.length ?? 0) > 0 && <small className="block text-meta">调用的工具：{r.tool_calls!.join("、")}</small>}</Collapsible> : <span className="text-meta">{r.status === "judging" || r.status === "not_needed" ? "没有调用 dsh" : "未知"}</span>}
                    <small className="block break-all text-meta">关联 {r.trace_id}</small>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </Collapsible>
  );
}
