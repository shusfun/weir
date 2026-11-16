import { useState } from "react";
import { Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Notice, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { bytes, date } from "../lib/models";
import { ObjectLink, useStringDialogParam } from "./object-link";
import { State } from "./feedback";
import { WaitText } from "./wait";

interface Candidate {
  domain: string; bytes: number; users: number | null; observed_at: string; ignored: boolean;
  nodes: { gate_id: number; node_id: number; bytes: number }[];
  days: { day: string; bytes: number }[];
  decisions: { user_id: number; at: string }[];
}
interface Candidates {
  items: Candidate[]; total: number; reason: string;
  coverage: { gate_id: number; engine: string; status: string; stale: boolean; reason: string; observed_at: string | null }[];
}

export function DomainCandidates({ gateId }: { gateId?: number }) {
  const [showIgnored, setShowIgnored] = useState(false);
  const dialog = useStringDialogParam("domain");
  const q = useQuery({ queryKey: ["domain-candidates", gateId], queryFn: () => api(`/admin/domain-candidates${gateId ? `?gate=${gateId}` : ""}`) as Promise<Candidates>, refetchInterval: 30000 });
  const selected = useQuery({ queryKey: ["domain-candidate", dialog.value, gateId], queryFn: () => api(`/admin/domain-candidates?domain=${encodeURIComponent(dialog.value!)}${gateId ? `&gate=${gateId}` : ""}`) as Promise<Candidates>, enabled: !!dialog.value });
  const items = q.data?.items.filter(d => showIgnored || !d.ignored) ?? [];
  return <section className="stack" data-tour="domain-candidates">
    <SectionHeader flush title="国内域名优化" description="核对已观测流量，再决定是否加入订阅直连规则。" actions={<SwitchField label="显示已忽略" checked={showIgnored} onCheckedChange={setShowIgnored} />} />
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <>
      <Notice>{q.data.reason}</Notice>
      <Card><CardContent className="stack">
        {q.data.coverage.length === 0 ? <State empty="观测覆盖未知" detail="尚无支持的内核观测回执。" /> : q.data.coverage.map(c => <div key={`${c.gate_id}:${c.engine}`}><ObjectLink to={{ type: "gate", id: c.gate_id }} /> · {c.engine} <Badge variant="status" tone={c.stale || c.status !== "partial" ? "neutral" : "warn"}>{c.stale ? "观测过期" : c.status === "partial" ? "部分覆盖" : "未知"}</Badge><p>{c.reason} · {c.observed_at ? date(c.observed_at) : "尚无观测"}</p></div>)}
      </CardContent></Card>
      {items.length === 0 ? <State empty="暂无可核对的国内域名" detail="未观测到不代表流量为零。" /> : <Card><CardContent><Table><TableHeader><TableRow><TableHead>域名</TableHead><TableHead>已观测流量</TableHead><TableHead>已识别人数下限</TableHead><TableHead>最近观测</TableHead><TableHead>操作</TableHead></TableRow></TableHeader><TableBody>{items.map(d => <TableRow key={d.domain}><TableCell>{d.domain}{d.ignored && "（已忽略）"}</TableCell><TableCell>{bytes(d.bytes)}</TableCell><TableCell>{d.users ?? "未知"}</TableCell><TableCell>{date(d.observed_at)}</TableCell><TableCell><Button variant="outline" onClick={() => dialog.open(d.domain)}>趋势与直连</Button></TableCell></TableRow>)}</TableBody></Table></CardContent></Card>}
      {q.data.total > q.data.items.length && <Notice>显示流量最高的 {q.data.items.length} 个域名，共 {q.data.total} 个。</Notice>}
    </>}
    {dialog.value && (selected.data?.items[0] ? <CandidateDetail key={dialog.value} value={selected.data.items[0]} close={dialog.close} /> : <Dialog open onOpenChange={o => !o && dialog.close()}><DialogContent><DialogTitle>国内域名观测</DialogTitle><DialogDescription>查看近期已观测流量与覆盖范围。</DialogDescription><State loading={selected.isPending} error={selected.error} onRetry={() => selected.refetch()} empty={!selected.isPending && !selected.error ? "该域名已不在当前观测范围" : undefined} /></DialogContent></Dialog>)}
  </section>;
}

function CandidateDetail({ value: d, close }: { value: Candidate; close: () => void }) {
  const [userId, setUserId] = useState("");
  const confirm = useConfirm();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as { id: number; email: string }[] });
  const policy = useQuery({ queryKey: ["proxy", userId], queryFn: () => api(`/proxy?user=${userId}`) as Promise<{ custom: { revision: number } }>, enabled: !!userId });
  const ignore = useRowAction({ invalidate: [["domain-candidates"], ["domain-candidate"]] });
  const save = useFormAction(async track => {
    if (!policy.data) return false;
    if (!await confirm({ title: `将 ${d.domain} 加入直连规则？`, description: "追加到所选用户的订阅清单末尾，保留现有匹配顺序。前面的规则可能优先匹配；客户端更新订阅后生效。", confirmText: "确认加入" })) return false;
    return api(`/admin/domain-candidates/${encodeURIComponent(d.domain)}/direct`, "POST", { user_id: Number(userId), revision: policy.data.custom.revision, confirmed: true }, track);
  }, { invalidate: [["domain-candidates"], ["domain-candidate"], ["proxy"]] });
  useConsoleContext({ dialog: { title: "国内域名优化" }, notices: [d.domain, "仅已观测流量；不参与额度扣费"] });
  return <Dialog open onOpenChange={o => !o && !save.busy && !ignore.isBusy() && close()}><DialogContent size="lg"><DialogTitle>{d.domain}</DialogTitle><DialogDescription>只显示已观测到的日期；空缺日期为未知。规则调整与流量变化存在其他影响因素。</DialogDescription>
    <div className="stack"><p>近 30 天已观测 {bytes(d.bytes)} · 已识别人数下限 {d.users ?? "未知"}</p>
      <SectionHeader flush title="主要节点" size="sm" />{d.nodes.map(n => <p key={`${n.gate_id}:${n.node_id}`}><ObjectLink to={{ type: "gate", id: n.gate_id }} /> · {n.node_id ? <ObjectLink to={{ type: "node", id: n.node_id }} /> : "原始节点未知"} · {bytes(n.bytes)}</p>)}
      <Table><TableHeader><TableRow><TableHead>日期</TableHead><TableHead>已观测流量</TableHead></TableRow></TableHeader><TableBody>{d.days.map(day => <TableRow key={day.day}><TableCell>{day.day}</TableCell><TableCell>{bytes(day.bytes)}</TableCell></TableRow>)}</TableBody></Table>
      {d.decisions.length > 0 && <><SectionHeader flush title="历史规则添加" size="sm" />{d.decisions.map(v => <p key={v.user_id}><ObjectLink to={{ type: "user", id: v.user_id }} /> · {date(v.at)}。当前是否启用请核对该用户清单。</p>)}</>}
      <Button variant="outline" busy={ignore.isBusy()} onClick={() => void ignore.run(d.domain, () => api(`/admin/domain-candidates/${encodeURIComponent(d.domain)}/ignore`, "POST", { ignored: !d.ignored }), d.ignored ? "已恢复候选" : "已忽略候选")}>{d.ignored ? "恢复候选" : "忽略候选"}</Button>
      <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}><Field name="user_id" label="加入哪个用户的订阅直连清单" required><Select value={userId} onValueChange={value => setUserId(value ?? "")}><SelectTrigger><SelectValue placeholder="选择用户" /></SelectTrigger><SelectContent>{users.data?.map(u => <SelectItem key={u.id} value={String(u.id)}>{u.email}</SelectItem>)}</SelectContent></Select></Field>
        {(users.isPending || users.error || userId && (policy.isPending || policy.error)) && <State loading={users.isPending || !!userId && policy.isPending} error={users.error || policy.error} onRetry={() => { void users.refetch(); if (userId) void policy.refetch(); }} />}
        <DialogFooter error={save.error} status={<WaitText wait={save.wait} />}><Button type="button" variant="outline" onClick={close} disabled={save.busy || ignore.isBusy()}>关闭</Button><Button type="submit" disabled={!policy.data || ignore.isBusy()} busy={save.busy} busyText="正在核对并保存…">确认加入直连</Button></DialogFooter>
      </Form>
    </div>
  </DialogContent></Dialog>;
}
