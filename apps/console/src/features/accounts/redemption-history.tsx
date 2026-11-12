import { InsetGroup, InsetRows, InsetRow, Pager } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { State } from "../../components/feedback";
import { date } from "../../lib/models";
import { GrantSummary, GrantSyncStatus } from "./grant";
import type { CodePage } from "./types";

export function RedemptionHistory({ page, onPage }: { page: number; onPage: (page: number) => void }) {
  const history = useQuery({ queryKey: ["me-redemptions", page], queryFn: () => api(`/me/redemptions?page=${page}`) as Promise<CodePage>, refetchInterval: (q) => q.state.data?.items.some((c) => c.synchronization === "pending") ? 5000 : false });
  return <InsetGroup title="兑换历史" description="按兑换时间排列，权益以当时签发的套餐快照为准。">
    {history.isPending || history.error ? <State loading={history.isPending} error={history.error} onRetry={() => history.refetch()} /> : !history.data.items.length ? <State empty="还没有兑换记录" detail="成功兑换的权益会保留在这里。" /> : <InsetRows>{history.data.items.map((c) => <InsetRow key={c.id}>
      <div className="account-history-entry"><div className="stack-tight"><strong>兑换 #{c.id}</strong><GrantSummary grant={c.grant} /><small>{date(c.used_at ?? undefined)} · {c.plan_id ? `套餐 #${c.plan_id} · 签发版本 ${c.plan_revision}` : "无附带套餐"}</small></div><GrantSyncStatus value={c.synchronization} /></div>
    </InsetRow>)}</InsetRows>}
    <Pager page={page} pageSize={50} total={history.data?.total} busy={history.isFetching} onPage={onPage} />
  </InsetGroup>;
}
