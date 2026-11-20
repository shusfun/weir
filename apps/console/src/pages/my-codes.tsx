import { useState } from "react";
import { Badge, Button, Card, PageHeader, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm } from "@weir/ui";
import { Plus } from "lucide-react";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { CodeForm } from "../features/accounts/code-form";
import type { AccessCode, CodePage, Plan } from "../features/accounts/types";

export function MyCodesPage() {
  const [open, setOpen] = useState(false);
  const plans = useQuery({ queryKey: ["my-access-code-plans"], queryFn: () => api("/me/access-code-plans") as Promise<{ items: Plan[]; daily_limit: number }> });
  const codes = useQuery({ queryKey: ["my-access-codes"], queryFn: () => api("/me/access-codes?page=1") as Promise<CodePage> });
  const revoke = useRowAction<number>({ refetch: codes.refetch });
  const confirm = useConfirm();
  const revokeCode = async (id: number) => {
    if (await confirm({ title: `撤销兑换码 #${id}？`, description: "撤销后不能再使用，已经兑换的授权不受影响。", tone: "danger", confirmText: "撤销" })) revoke.run(id, () => api(`/me/access-codes/${id}`, "DELETE"));
  };
  return <div className="stack">
    <PageHeader title="发放兑换码" description={plans.data ? `今日上限 ${plans.data.daily_limit} 个` : "管理已授权套餐的兑换码"} actions={<Button disabled={!plans.data || plans.data.items.length === 0} onClick={() => setOpen(true)}><Plus />生成兑换码</Button>} />
    {plans.error ? <State empty="暂无发码权限" detail="请联系管理员为你的账户授权套餐和每日上限。" /> : codes.error ? <State error={codes.error} onRetry={() => void codes.refetch()} /> : codes.isPending ? <State loading /> : <Card className="overflow-hidden p-0"><Table><TableHeader><TableRow><TableHead>编号</TableHead><TableHead>状态</TableHead><TableHead>到期时间</TableHead><TableHead>生成时间</TableHead><TableHead className="text-right"><span className="sr-only">操作</span></TableHead></TableRow></TableHeader><TableBody>{(codes.data?.items ?? []).map((code: AccessCode) => <TableRow key={code.id}><TableCell>#{code.id}</TableCell><TableCell><Badge variant="status" tone={code.status === "unused" ? "ok" : code.status === "revoked" || code.status === "expired" ? "warn" : "neutral"}>{code.status === "unused" ? "未使用" : code.status === "used" ? "已使用" : code.status === "revoked" ? "已撤销" : "已过期"}</Badge></TableCell><TableCell>{code.expires_at ? new Date(code.expires_at).toLocaleString() : "不限"}</TableCell><TableCell>{new Date(code.created_at).toLocaleString()}</TableCell><TableCell className="text-right">{code.status === "unused" ? <Button size="sm" variant="ghost" disabled={revoke.isBusy()} busy={revoke.isBusy(code.id)} onClick={() => void revokeCode(code.id)}>撤销</Button> : <span className="text-meta text-[var(--weir-ink-3)]">—</span>}</TableCell></TableRow>)}</TableBody></Table></Card>}
    {open && plans.data && <CodeForm plans={plans.data.items} basePath="/me/access-codes" close={() => setOpen(false)} saved={() => void codes.refetch()} />}
  </div>;
}
