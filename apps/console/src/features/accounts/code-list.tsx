import { Badge, Button, Card, CardContent, Field, FormGrid, Pager, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { useRowAction } from "../../lib/action";
import { useConsoleContext } from "../../lib/console-context";
import { ObjectLink, dataObject } from "../../components/object-link";
import { State } from "../../components/feedback";
import { date } from "../../lib/models";
import { GrantSummary, GrantSyncStatus } from "./grant";
import type { AccessCode, CodePage, Plan } from "./types";

export const codeKinds = { invite: "邀请码", redeem: "兑换码" };
const statuses = { unused: "待使用", used: "已使用", revoked: "已撤销", expired: "已过期" };
export type CodeFilters = { kind: string; status: string; plan_id: string; id: string; page: number };

function Filter({ label, value, options, onChange }: { label: string; value: string; options: { value: string; label: string }[]; onChange: (value: string) => void }) {
  return <Field label={label}><Select value={value || "all"} onValueChange={(v) => onChange(v === "all" ? "" : v ?? "")}><SelectTrigger><SelectValue>{options.find((o) => o.value === value)?.label ?? "全部"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">全部</SelectItem>{options.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}</SelectContent></Select></Field>;
}

export function CodeList({ plans, filters, onChange }: { plans: Plan[]; filters: CodeFilters; onChange: (next: Partial<CodeFilters>) => void }) {
  const query = new URLSearchParams({ page: String(filters.page) });
  for (const key of ["kind", "status", "plan_id", "id"] as const) if (filters[key]) query.set(key, filters[key]);
  const codes = useQuery({ queryKey: ["access-codes", query.toString()], queryFn: () => api(`/admin/access-codes?${query}`) as Promise<CodePage> });
  const revoke = useRowAction<number>({ refetch: codes.refetch });
  const confirm = useConfirm();
  useConsoleContext({ list: { total: codes.data?.total, items: (codes.data?.items ?? []).map((c) => ({ type: "access_code", id: c.id, name: `${codeKinds[c.kind]} #${c.id}` })) } });
  const revokeCode = async (c: AccessCode) => {
    if (await confirm({ title: `撤销${codeKinds[c.kind]} #${c.id}？`, description: "撤销后不能再用，已经发放的授权不受影响。", tone: "danger", confirmText: "撤销" })) revoke.run(c.id, () => api(`/admin/access-codes/${c.id}`, "DELETE"));
  };
  return <div className="stack" data-tour="account-codes">
    <FormGrid cols={3}>
      <Filter label="类型" value={filters.kind} options={Object.entries(codeKinds).map(([value, label]) => ({ value, label }))} onChange={(kind) => onChange({ kind, page: 1 })} />
      <Filter label="状态" value={filters.status} options={Object.entries(statuses).map(([value, label]) => ({ value, label }))} onChange={(status) => onChange({ status, page: 1 })} />
      <Filter label="套餐" value={filters.plan_id} options={plans.map((p) => ({ value: String(p.id), label: p.name }))} onChange={(plan_id) => onChange({ plan_id, page: 1 })} />
    </FormGrid>
    {codes.isPending || codes.error ? <State loading={codes.isPending} error={codes.error} onRetry={() => codes.refetch()} /> : !codes.data.items.length ? <State empty="没有符合条件的记录" detail="调整筛选，或生成一次性邀请码与兑换码。" /> :
      <Card><CardContent><Table><TableHeader><TableRow><TableHead>账户码</TableHead><TableHead>签发权益</TableHead><TableHead>状态</TableHead><TableHead>时间</TableHead><TableHead>操作</TableHead></TableRow></TableHeader><TableBody>{codes.data.items.map((c) => <TableRow key={c.id} {...dataObject({ type: "access_code", id: c.id })}>
        <TableCell><ObjectLink to={{ type: "access_code", id: c.id, name: `#${c.id}` }} /><div><Badge variant="soft" tone="neutral">{codeKinds[c.kind]}</Badge></div></TableCell>
        <TableCell><div className="stack-tight">{c.plan_id ? <span><ObjectLink to={{ type: "account_plan", id: c.plan_id, name: plans.find((p) => p.id === c.plan_id)?.name ?? `套餐 #${c.plan_id}` }} /> · 签发版本 {c.plan_revision}</span> : <span>不附带套餐</span>}<GrantSummary grant={c.grant} /></div></TableCell>
        <TableCell><div className="stack-tight"><Badge variant="status" tone={c.status === "expired" ? "warn" : "neutral"}>{statuses[c.status]}</Badge>{c.used_at && <GrantSyncStatus value={c.synchronization} />}{c.used_by && <ObjectLink to={{ type: "user", id: c.used_by }} />}</div></TableCell>
        <TableCell><div className="stack-tight"><span>生成 {date(c.created_at)}</span>{c.used_at && <span>使用 {date(c.used_at)}</span>}<small>码到期 {c.expires_at ? date(c.expires_at) : "不限"}</small></div></TableCell>
        <TableCell><Button variant="outline" disabled={!!c.used_at || c.revoked || revoke.isBusy()} busy={revoke.isBusy(c.id)} onClick={() => void revokeCode(c)}>撤销</Button></TableCell>
      </TableRow>)}</TableBody></Table></CardContent></Card>}
    <Pager page={filters.page} pageSize={50} total={codes.data?.total} busy={codes.isFetching} onPage={(page) => onChange({ page })} />
  </div>;
}
