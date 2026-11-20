import { ActionGroup, Badge, Button, Card, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { Pencil, Ticket } from "lucide-react";
import { ObjectLink, dataObject } from "../../components/object-link";
import { State } from "../../components/feedback";
import { GrantSummary } from "./grant";
import type { Plan } from "./types";

export function PlanList({ plans, edit, issue, history, canEdit }: { plans: Plan[]; edit: (p: Plan) => void; issue?: (p: Plan) => void; history: (p: Plan) => void; canEdit: boolean }) {
  if (!plans.length) return <State empty="还没有套餐预设" detail="配置流量额度、有效天数和授权节点后，即可一键签发兑换码。" />;
  return <Card className="overflow-hidden p-0"><Table><TableHeader><TableRow><TableHead>套餐预设</TableHead><TableHead>发放权益</TableHead><TableHead>状态与版本</TableHead><TableHead className="text-right"><span className="sr-only">操作</span></TableHead></TableRow></TableHeader><TableBody>{plans.map((p) => <TableRow key={p.id} {...dataObject({ type: "account_plan", id: p.id })}>
    <TableCell><b><ObjectLink to={{ type: "account_plan", id: p.id, name: p.name }} /></b></TableCell>
    <TableCell><GrantSummary grant={p.grant} /></TableCell>
    <TableCell className="whitespace-nowrap"><Badge variant="status" tone={p.enabled ? "ok" : "neutral"}>{p.enabled ? "启用" : "停用"}</Badge></TableCell>
    <TableCell className="text-right"><ActionGroup align="end" wrap={false}>{issue && p.enabled && <Button size="sm" variant="outline" onClick={() => issue(p)}><Ticket />发码</Button>}<Button size="sm" variant="ghost" disabled={!canEdit} onClick={() => edit(p)}><Pencil />编辑</Button><Button size="sm" variant="ghost" onClick={() => history(p)}>发放记录</Button></ActionGroup></TableCell>
  </TableRow>)}</TableBody></Table></Card>;
}
