import { Badge, Button, Card, PageState, Pager, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm } from "@weir/ui";
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
const statuses = { unused: "待使用", used: "已领用", revoked: "已撤销", expired: "已过期" };
export type CodeFilters = { kind: string; status: string; plan_id: string; id: string; page: number };

export function CodeList({ plans, filters, onChange, onCreatePlan }: { plans: Plan[]; filters: CodeFilters; onChange: (next: Partial<CodeFilters>) => void; onCreatePlan?: () => void }) {
  const query = new URLSearchParams({ page: String(filters.page) });
  for (const key of ["kind", "status", "plan_id", "id"] as const) if (filters[key]) query.set(key, filters[key]);
  const codes = useQuery({ queryKey: ["access-codes", query.toString()], queryFn: () => api(`/admin/access-codes?${query}`) as Promise<CodePage> });
  // 领用人显示用户名：与用户页共用缓存；用户已删除时如实说明
  const users = useQuery({ queryKey: ["users"], enabled: !!codes.data?.items.some((c) => c.used_by), queryFn: async () => (await api("/admin/users")).items as { id: number; name: string; email: string }[] });
  const userName = (id: number) => { const u = users.data?.find((x) => x.id === id); return u ? u.name || u.email : users.isPending ? "正在读取用户" : "已删除的用户"; };
  const revoke = useRowAction<number>({ refetch: codes.refetch });
  const confirm = useConfirm();
  useConsoleContext({ list: { total: codes.data?.total, items: (codes.data?.items ?? []).map((c) => ({ type: "access_code", id: c.id, name: `${codeKinds[c.kind]} #${c.id}` })) } });
  const revokeCode = async (c: AccessCode) => {
    if (await confirm({ title: `撤销${codeKinds[c.kind]} #${c.id}？`, description: "撤销后不能再用，已经发放的授权不受影响。", tone: "danger", confirmText: "撤销" })) revoke.run(c.id, () => api(`/admin/access-codes/${c.id}`, "DELETE"));
  };
  return <div className="stack" data-tour="account-codes">
    <div className="flex flex-wrap items-center gap-[var(--weir-gap-9)]">
      <Segmented
        aria-label="类型筛选"
        value={filters.kind}
        onValueChange={(kind) => onChange({ kind, page: 1 })}
        options={[["", "全部类型"], ["redeem", "兑换码"], ["invite", "邀请码"]] as const}
      />
      <Segmented
        aria-label="状态筛选"
        value={filters.status}
        onValueChange={(status) => onChange({ status, page: 1 })}
        options={[["", "全部状态"], ["unused", "待使用"], ["used", "已领用"], ["expired", "已过期"], ["revoked", "已撤销"]] as const}
      />
      {plans.length > 0 && (
        <Select value={filters.plan_id || "all"} onValueChange={(v) => onChange({ plan_id: v === "all" ? "" : v ?? "", page: 1 })}>
          <SelectTrigger aria-label="按套餐预设筛选" className="min-w-[161px]">
            <SelectValue>{plans.find((p) => String(p.id) === filters.plan_id)?.name ?? "全部套餐预设"}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部套餐预设</SelectItem>
            {plans.map((p) => <SelectItem key={p.id} value={String(p.id)}>{p.name}</SelectItem>)}
          </SelectContent>
        </Select>
      )}
    </div>
    {codes.isPending || codes.error ? <State loading={codes.isPending} error={codes.error} onRetry={() => codes.refetch()} /> : !codes.data.items.length ? <PageState kind="empty" title={plans.length === 0 ? "还没有兑换码与套餐预设" : "没有符合条件的兑换码"} detail={plans.length === 0 ? "先新建一份套餐预设（流量、时长与节点），即可批量生成一次性兑换码；注册邀请码也可直接生成。" : "调整上方筛选条件，或点右上角生成新的兑换码与邀请码。"} action={plans.length === 0 && onCreatePlan ? <Button variant="outline" onClick={onCreatePlan}>新建套餐预设</Button> : undefined} /> :
      <Card className="overflow-hidden p-0"><Table><TableHeader><TableRow><TableHead>编号与类型</TableHead><TableHead>包含权益</TableHead><TableHead>状态与领用</TableHead><TableHead>时间</TableHead><TableHead className="text-right"><span className="sr-only">操作</span></TableHead></TableRow></TableHeader><TableBody>{codes.data.items.map((c) => <TableRow key={c.id} {...dataObject({ type: "access_code", id: c.id })}>
        <TableCell className="whitespace-nowrap">
          <span className="inline-flex items-center gap-2 font-mono">
            <ObjectLink to={{ type: "access_code", id: c.id, name: `#${c.id}` }} />
            <Badge variant="soft" tone={c.kind === "redeem" ? "info" : "neutral"}>{codeKinds[c.kind]}</Badge>
          </span>
        </TableCell>
        <TableCell>
          <div className="space-y-0.5">
            {c.plan_id ? <span className="text-body font-medium"><ObjectLink to={{ type: "account_plan", id: c.plan_id, name: plans.find((p) => p.id === c.plan_id)?.name ?? "已删除的套餐预设" }} />{(() => { const plan = plans.find((p) => p.id === c.plan_id); return plan && plan.revision !== c.plan_revision ? <small className="ml-1.5 text-[var(--weir-ink-3)]">签发后预设已修改，按签发时内容发放</small> : null; })()}</span> : <span className="text-meta text-[var(--weir-ink-3)]">仅注册资格（不附带套餐）</span>}
            <div className="text-meta text-[var(--weir-ink-3)]"><GrantSummary grant={c.grant} /></div>
          </div>
        </TableCell>
        <TableCell>
          <div className="space-y-0.5">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="status" tone={c.status === "unused" ? "ok" : c.status === "expired" || c.status === "revoked" ? "warn" : "neutral"}>{statuses[c.status]}</Badge>
              {c.used_by && <ObjectLink to={{ type: "user", id: c.used_by, name: userName(c.used_by) }} />}
            </div>
            {c.used_at && <div className="text-fine text-[var(--weir-ink-3)]"><GrantSyncStatus value={c.synchronization} /></div>}
          </div>
        </TableCell>
        <TableCell className="whitespace-nowrap font-mono text-meta">
          <div>创建 {date(c.created_at).slice(0, 16)}</div>
          <small className="text-[var(--weir-ink-3)]">{c.used_at ? `领用 ${date(c.used_at).slice(0, 16)}` : c.expires_at ? `有效至 ${date(c.expires_at).slice(0, 10)}` : "长期有效"}</small>
        </TableCell>
        <TableCell className="text-right">
          {!c.used_at && !c.revoked ? (
            <Button size="sm" variant="ghost" disabled={revoke.isBusy()} busy={revoke.isBusy(c.id)} onClick={() => void revokeCode(c)}>撤销</Button>
          ) : (
            <span className="text-meta text-[var(--weir-ink-3)]">—</span>
          )}
        </TableCell>
      </TableRow>)}</TableBody></Table></Card>}
    <Pager page={filters.page} pageSize={50} total={codes.data?.total} busy={codes.isFetching} onPage={(page) => onChange({ page })} />
  </div>;
}
