import { useState } from "react";
import { Badge, Button, Card, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Notice, ScrollArea, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { api } from "../../../api";
import { useFormAction } from "../../../lib/action";
import { useQuery } from "../../../lib/query";
import type { NodeTestCheck } from "../../../lib/core";
import type { Profile } from "../../../lib/subscriptions";
import { field, useConsoleContext } from "../../../lib/console-context";
import { ProbeLocation } from "../../../components/probe-location";
import { JobLine, type Job } from "../../../components/job";

type Item = { index: number; name: string; check: NodeTestCheck };
type Result = { location: string; execution_gate_id: number; complete: boolean; checks: Item[] };

export function ProfileProbe({ profile, close }: { profile: Profile; close: () => void }) {
  const [gate, setGate] = useState(0);
  const [job, setJob] = useState<Job>();
  const [result, setResult] = useState<Result>();
  const previous = useQuery({ queryKey: ["subscription-probe", profile.id], queryFn: () => api(`/admin/subscriptions/profiles/${profile.id}/test`) as Promise<{ result: Result | null }> });
  useConsoleContext({ dialog: { title: `实测订阅组「${profile.name}」` }, object: { type: "profile", id: profile.id, name: profile.name }, form: [field("gate_id", "测试位置 VPS ID（0 为中心）", gate)] });
  const save = useFormAction((track) => api(`/admin/subscriptions/profiles/${profile.id}/test`, "POST", { gate_id: gate }, track) as Promise<Result>, { onJob: setJob, onSuccess: (out) => { setResult(out); void previous.refetch(); }, onError: () => { void previous.refetch(); } });
  const output = job?.output as { checks?: Item[] } | undefined;
  const checks = save.busy ? output?.checks ?? [] : result?.checks ?? previous.data?.result?.checks ?? output?.checks ?? [];
  const current = result ?? previous.data?.result;
  return <Dialog open onOpenChange={(open) => !open && close()}><DialogContent size="lg">
    <DialogTitle>实测订阅组 · {profile.name}</DialogTitle>
    <DialogDescription>按同一处理链取得节点并分批完成握手测试，不修改订阅内容。</DialogDescription>
    <ProbeLocation value={gate} change={setGate} disabled={save.busy} />
    {job && <JobLine job={job} idle="" />}
    {!save.busy && current && <Notice>{current.location === "gate" ? `VPS #${current.execution_gate_id}` : "控制中心"} · {current.complete ? "已完成全部测试" : "仅有部分结果"}</Notice>}
    {checks.length === 0 ? (
      <div className="rounded-[var(--weir-radius-card)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-4 py-8 text-center text-meta text-[var(--weir-ink-3)]">
        选择发起位置后点下方「开始实测」，逐节点握手结果与延迟会显示在这里。
      </div>
    ) : (
      <Card className="overflow-hidden p-0">
        <ScrollArea className="max-h-80"><Table><TableHeader><TableRow><TableHead>节点</TableHead><TableHead>结果</TableHead><TableHead>延迟</TableHead><TableHead>说明</TableHead></TableRow></TableHeader><TableBody>
          {checks.map((item) => <TableRow key={item.index}><TableCell className="font-medium">{item.name}</TableCell><TableCell><Badge variant="soft" tone={item.check.status === "passed" ? "ok" : item.check.status === "failed" ? "bad" : "neutral"}>{item.check.status === "passed" ? "连通" : item.check.status === "failed" ? "未连通" : "未知"}</Badge></TableCell><TableCell className="font-mono">{item.check.status === "passed" ? `${item.check.latency_ms} ms` : "未知"}</TableCell><TableCell className="text-meta text-[var(--weir-ink-3)]">{item.check.why || "—"}</TableCell></TableRow>)}
        </TableBody></Table></ScrollArea>
      </Card>
    )}
    <DialogFooter error={save.error || (previous.error instanceof Error ? previous.error.message : "")}><Button variant="ghost" onClick={close}>关闭</Button><Button busy={save.busy} busyText="正在实测…" onClick={() => { setJob(undefined); setResult(undefined); void save.submit(); }}>开始实测</Button></DialogFooter>
  </DialogContent></Dialog>;
}
