import { ActionGroup, Badge, Button, Card, CardContent, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { ObjectLink, dataObject } from "../../components/object-link";
import { State } from "../../components/feedback";
import { GrantSummary } from "./grant";
import type { Plan } from "./types";

export function PlanList({ plans, edit, history, canEdit }: { plans: Plan[]; edit: (p: Plan) => void; history: (p: Plan) => void; canEdit: boolean }) {
  if (!plans.length) return <State empty="还没有套餐" detail="流量、节点和有效期都可以单独配置。" />;
  return <Card><CardContent><Table><TableHeader><TableRow><TableHead>套餐</TableHead><TableHead>发放权益</TableHead><TableHead>操作</TableHead></TableRow></TableHeader><TableBody>{plans.map((p) => <TableRow key={p.id} {...dataObject({ type: "account_plan", id: p.id })}>
    <TableCell><ObjectLink to={{ type: "account_plan", id: p.id, name: p.name }} /><div><Badge variant="soft" tone="neutral">{p.enabled ? "启用" : "停用"} · v{p.revision}</Badge></div></TableCell>
    <TableCell><GrantSummary grant={p.grant} /></TableCell>
    <TableCell><ActionGroup><Button variant="outline" disabled={!canEdit} onClick={() => edit(p)}>编辑</Button><Button variant="ghost" onClick={() => history(p)}>发放历史</Button></ActionGroup></TableCell>
  </TableRow>)}</TableBody></Table></CardContent></Card>;
}
