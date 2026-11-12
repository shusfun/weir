import { useEffect } from "react";
import { useSearchParams, Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { TextLink, Button, Notice, PageHeader, Tabs, TabsPanel, TabsList, TabsTab } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { useConsoleContext } from "../../lib/console-context";
import { State } from "../../components/feedback";
import { useDialogParam, useStringDialogParam } from "../../components/object-link";
import type { NodeItem } from "../../lib/core";
import { PlanForm } from "./plan-form";
import { CodeForm } from "./code-form";
import { CodeList, type CodeFilters } from "./code-list";
import { PlanList } from "./plan-list";
import type { CodeKind, Plan } from "./types";

export function AccountsPage() {
  const [params, setParams] = useSearchParams();
  const client = useQueryClient();
  const rawTab = params.get("tab");
  const tab = rawTab === "codes" || rawTab === "invite" || rawTab === "redeem" ? "codes" : "plans";
  const selectedCode = params.get("code") ?? "";
  const kind = selectedCode ? "" : params.get("kind") ?? (rawTab === "invite" || rawTab === "redeem" ? rawTab : "");
  const filters: CodeFilters = { kind, status: params.get("status") ?? "", plan_id: params.get("plan_id") ?? "", id: selectedCode, page: Math.max(1, Number(params.get("page")) || 1) };
  useEffect(() => {
    if (rawTab !== "invite" && rawTab !== "redeem") return;
    setParams((p) => { p.set("tab", "codes"); if (!selectedCode) p.set("kind", rawTab); return p; }, { replace: true });
  }, [rawTab, selectedCode, setParams]);
  const plans = useQuery({ queryKey: ["account-plans"], queryFn: () => api("/admin/account-plans") as Promise<{ items: Plan[] }> });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const profiles = useQuery({ queryKey: ["account-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as { id: number; name: string }[] });
  const edit = useDialogParam("plan", plans.data?.items, !plans.isFetching && !plans.error);
  const create = useStringDialogParam("create");
  const form = edit.current ?? (create.value === "plan" ? "new" : null);
  const issuing = ["code", "invite", "redeem"].includes(create.value ?? "");
  const canEdit = !!nodes.data && !!profiles.data && !nodes.error && !profiles.error;
  const changeTab = (next: string) => setParams((p) => { p.set("tab", next); for (const key of ["page", "code", "focus", "plan", "create"]) p.delete(key); return p; });
  const changeFilters = (next: Partial<CodeFilters>) => setParams((p) => { for (const [key, value] of Object.entries(next)) { const name = key === "id" ? "code" : key; if (value) p.set(name, String(value)); else p.delete(name); } return p; });
  useConsoleContext({ page: "套餐与用码", tab: tab === "plans" ? "套餐" : "用码", ...(tab === "plans" ? { list: { total: plans.data?.items.length, items: (plans.data?.items ?? []).map((p) => ({ type: "account_plan", id: p.id, name: p.name })) } } : {}) });
  return <div className="stack">
    <PageHeader size="data" title="套餐与用码" description="按需组合权益，统一签发和查看一次性码的使用记录。" actions={<Button disabled={!!plans.error || plans.isPending || (tab === "plans" && !canEdit)} onClick={() => create.open(tab === "plans" ? "plan" : "code")}>{tab === "plans" ? "新建套餐" : "生成码"}</Button>} />
    <Notice>注册和兑换入口由<TextLink render={<Link to="/system?tab=login" />}>账户策略</TextLink>控制。旧码保留签发权益；历史中的套餐链接指向当前配置。</Notice>
    {selectedCode && <Notice>正在查看指定账户码。<Button variant="link" onClick={() => changeFilters({ id: "", kind: "", status: "", plan_id: "", page: 1 })}>显示全部</Button></Notice>}
    <Tabs value={tab} onValueChange={(v) => changeTab(String(v))}><TabsList aria-label="套餐与用码分区"><TabsTab value="plans">套餐</TabsTab><TabsTab value="codes">用码</TabsTab></TabsList>
      <TabsPanel value="plans"><div data-tour="account-plans">
        {plans.isPending || plans.error ? <State loading={plans.isPending} error={plans.error} onRetry={() => plans.refetch()} /> : <PlanList plans={plans.data.items} edit={edit.open} canEdit={canEdit} history={(p) => setParams({ tab: "codes", plan_id: String(p.id) })} />}
        {(nodes.error || profiles.error) && <State error={nodes.error || profiles.error} onRetry={() => { void nodes.refetch(); void profiles.refetch(); }} />}
      </div></TabsPanel>
      <TabsPanel value="codes">{tab === "codes" && <CodeList plans={plans.data?.items ?? []} filters={filters} onChange={changeFilters} />}</TabsPanel>
    </Tabs>
    {form && !plans.isPending && !plans.error && canEdit && <PlanForm key={form === "new" ? "new" : form.id} value={form} nodes={nodes.data ?? []} profiles={profiles.data ?? []} close={form === "new" ? create.close : edit.close} saved={() => { if (form === "new") create.close(); else edit.close(); void plans.refetch(); }} />}
    {issuing && <CodeForm initialKind={(create.value === "redeem" || kind === "redeem" ? "redeem" : "invite") as CodeKind} plans={plans.data?.items ?? []} close={create.close} saved={() => void client.invalidateQueries({ queryKey: ["access-codes"] })} />}
  </div>;
}
