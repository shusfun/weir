import { useEffect, useState } from "react";
import { useSearchParams, Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { TextLink, Button, Notice, PageHeader, Tabs, TabsPanel, TabsList, TabsTab } from "@weir/ui";
import { Plus } from "lucide-react";
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
  const [presetPlanId, setPresetPlanId] = useState<number | undefined>();
  const rawTab = params.get("tab");
  const tab = rawTab === "plans" || params.has("plan") || params.get("create") === "plan" ? "plans" : "codes";
  const selectedCode = params.get("code") ?? "";
  const kind = selectedCode ? "" : params.get("kind") ?? (rawTab === "invite" || rawTab === "redeem" ? rawTab : "");
  const filters: CodeFilters = { kind, status: params.get("status") ?? "", plan_id: params.get("plan_id") ?? "", id: selectedCode, page: Math.max(1, Number(params.get("page")) || 1) };
  useEffect(() => {
    if (rawTab !== "invite" && rawTab !== "redeem") return;
    setParams((p) => { p.set("tab", "codes"); if (!selectedCode) p.set("kind", rawTab); return p; }, { replace: true });
  }, [rawTab, selectedCode, setParams]);
  const plans = useQuery({ queryKey: ["account-plans"], queryFn: () => api("/admin/account-plans") as Promise<{ items: Plan[] }> });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => ((await api("/admin/nodes")).items ?? []) as NodeItem[] });
  const profiles = useQuery({ queryKey: ["account-profiles"], queryFn: async () => ((await api("/admin/subscriptions/profiles")).items ?? []) as { id: number; name: string }[] });
  const edit = useDialogParam("plan", plans.data?.items, !plans.isFetching && !plans.error);
  const create = useStringDialogParam("create");
  const form = edit.current ?? (create.value === "plan" ? "new" : null);
  const issuing = ["code", "invite", "redeem"].includes(create.value ?? "");
  const canEdit = !!nodes.data && !!profiles.data && !nodes.error && !profiles.error;
  const hasEnabledPlan = (plans.data?.items ?? []).some((p) => p.enabled);
  const changeTab = (next: string) => setParams((p) => { p.set("tab", next); for (const key of ["page", "code", "focus", "plan", "create"]) p.delete(key); return p; });
  const changeFilters = (next: Partial<CodeFilters>) => setParams((p) => { for (const [key, value] of Object.entries(next)) { const name = key === "id" ? "code" : key; if (value) p.set(name, String(value)); else p.delete(name); } return p; });
  useConsoleContext({ page: "兑换码", tab: tab === "plans" ? "套餐预设" : "兑换码", ...(tab === "plans" ? { list: { total: plans.data?.items.length, items: (plans.data?.items ?? []).map((p) => ({ type: "account_plan", id: p.id, name: p.name })) } } : {}) });
  return <div className="stack">
    <PageHeader size="data" title="兑换码" description={plans.data ? <span className="weir-sum"><span><b>{plans.data.items.length}</b> 个套餐预设</span><span>入口开关见 <TextLink render={<Link to="/system?tab=login&setting=accounts" />}>注册与账号策略</TextLink></span></span> : "签发一次性兑换码与邀请码。"} actions={<Button disabled={!!plans.error || plans.isPending || (tab === "plans" && !canEdit)} onClick={() => { setPresetPlanId(undefined); create.open(tab === "plans" ? "plan" : kind === "invite" ? "invite" : "redeem"); }}><Plus />{tab === "plans" ? "新建套餐预设" : kind === "invite" ? "生成邀请码" : "生成兑换码"}</Button>} />
    {selectedCode && <Notice>正在筛选单个兑换码 #{selectedCode}。<Button size="xs" variant="ghost" onClick={() => changeFilters({ id: "", kind: "", status: "", plan_id: "", page: 1 })}>查看全部</Button></Notice>}
    <Tabs value={tab} onValueChange={(v) => changeTab(String(v))}><TabsList aria-label="兑换码分区"><TabsTab value="codes">兑换码</TabsTab><TabsTab value="plans">套餐预设{plans.data ? `（${plans.data.items.length}）` : ""}</TabsTab></TabsList>
      <TabsPanel value="codes">{tab === "codes" && <CodeList plans={plans.data?.items ?? []} filters={filters} onChange={changeFilters} onCreatePlan={() => { changeTab("plans"); create.open("plan"); }} />}</TabsPanel>
      <TabsPanel value="plans"><div data-tour="account-plans">
        {plans.isPending || plans.error ? <State loading={plans.isPending} error={plans.error} onRetry={() => plans.refetch()} /> : <PlanList plans={plans.data.items} edit={edit.open} canEdit={canEdit} issue={(p) => { setPresetPlanId(p.id); create.open("redeem"); }} history={(p) => setParams({ tab: "codes", plan_id: String(p.id) })} />}
        {(nodes.error || profiles.error) && <State error={nodes.error || profiles.error} onRetry={() => { void nodes.refetch(); void profiles.refetch(); }} />}
      </div></TabsPanel>
    </Tabs>
    {form && !plans.isPending && !plans.error && canEdit && <PlanForm key={form === "new" ? "new" : form.id} value={form} nodes={nodes.data ?? []} profiles={profiles.data ?? []} close={form === "new" ? create.close : edit.close} saved={() => { if (form === "new") create.close(); else edit.close(); void plans.refetch(); }} />}
    {issuing && <CodeForm initialKind={(create.value === "invite" || kind === "invite" || (!hasEnabledPlan && create.value !== "redeem") ? "invite" : "redeem") as CodeKind} initialPlanId={presetPlanId} plans={plans.data?.items ?? []} onCreatePlan={() => { create.close(); changeTab("plans"); setTimeout(() => create.open("plan"), 0); }} close={() => { setPresetPlanId(undefined); create.close(); }} saved={() => void client.invalidateQueries({ queryKey: ["access-codes"] })} />}
  </div>;
}
