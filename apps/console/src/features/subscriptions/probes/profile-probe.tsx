import { useState } from "react";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Notice, ScrollArea, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
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
  return <Dialog open onOpenChange={(open) => !open && close()}><DialogContent size="xl">
    <DialogTitle>实测订阅组「{profile.name}」</DialogTitle>
    <DialogDescription>按同一处理链取得节点，分批完成协议握手与限量请求；不会修改订阅内容。取消后保留已完成条目，未完成显示未知。</DialogDescription>
    <ProbeLocation value={gate} change={setGate} disabled={save.busy} />
    <JobLine job={job} idle="尚未开始本次实测" />
    {!save.busy && current && <Notice>{current.location === "gate" ? `VPS ${current.execution_gate_id}` : "中心"} · {current.complete ? "本次已完成" : "仅有部分结果"}</Notice>}
    <ScrollArea className="max-h-96"><Table><TableHeader><TableRow><TableHead>节点</TableHead><TableHead>结果</TableHead><TableHead>延迟</TableHead><TableHead>原因</TableHead></TableRow></TableHeader><TableBody>
      {checks.map((item) => <TableRow key={item.index}><TableCell>{item.name}</TableCell><TableCell><Badge variant="soft" tone={item.check.status === "passed" ? "ok" : item.check.status === "failed" ? "bad" : "neutral"}>{item.check.status === "passed" ? "连通" : item.check.status === "failed" ? "未连通" : "未知"}</Badge></TableCell><TableCell>{item.check.status === "passed" ? `${item.check.latency_ms} ms` : "未知"}</TableCell><TableCell>{item.check.why || "—"}</TableCell></TableRow>)}
    </TableBody></Table></ScrollArea>
    <DialogFooter error={save.error || (previous.error instanceof Error ? previous.error.message : "")}><Button variant="outline" onClick={close}>关闭</Button><Button busy={save.busy} busyText="正在实测…" onClick={() => { setJob(undefined); setResult(undefined); void save.submit(); }}>开始实测</Button></DialogFooter>
  </DialogContent></Dialog>;
}
