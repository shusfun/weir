import { Notice, SectionHeader, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { bytes, date } from "../lib/models";
import { ObjectLink } from "./object-link";
import { State } from "./feedback";

interface UsageRank { user_id: number; node_id: number; chain_id: number; bytes: number; observed_at: string }
export function UsageRanking({ path, byUser = false }: { path: string; byUser?: boolean }) {
  const q = useQuery({ queryKey: ["usage-ranking", path], queryFn: () => api(path) as Promise<{items: UsageRank[]; reason: string}>, refetchInterval: 30000 });
  return <section className="stack"><SectionHeader flush title="近 30 天已观测用量" />
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <>
      <Notice>{q.data.reason}</Notice>
      {q.data.items.length === 0 ? <State empty="用量未知" detail="尚未收到这一范围的凭据计数。" /> : <Table><TableHeader><TableRow><TableHead>{byUser ? "用户" : "节点"}</TableHead><TableHead>链式</TableHead><TableHead>用量</TableHead><TableHead>最后观测</TableHead></TableRow></TableHeader><TableBody>{q.data.items.map(r => <TableRow key={`${r.user_id}:${r.node_id}:${r.chain_id}`}><TableCell><ObjectLink to={{type: byUser ? "user" : "node", id: byUser ? r.user_id : r.node_id}} /></TableCell><TableCell>{r.chain_id ? <ObjectLink to={{type: "chain", id: r.chain_id}} /> : "直连"}</TableCell><TableCell>{bytes(r.bytes)}</TableCell><TableCell>{date(r.observed_at)}</TableCell></TableRow>)}</TableBody></Table>}
    </>}
  </section>;
}
