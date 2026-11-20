// 订阅编排流水线：「订阅源」（订阅源 + 手动节点）管输入，「订阅组与模板」（订阅组 + 规则模板 + 默认设置）管组合与输出。
import { Card, CardContent, PipelineBoard, PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { api } from "../api";
import { SourcesPanel } from "../components/subscription-sources";
import { useSources } from "../lib/subscription-sources";
import { useManualNodes, NodesPanel } from "../components/subscription-nodes";
import { useTemplates, ProfilesPanel } from "../components/subscription-profiles";
import { SettingsPanel, TemplatesPanel } from "../components/subscription-settings";
import { useConsoleContext } from "../lib/console-context";

const sourceTabs = [["sources", "订阅源"], ["nodes", "手动节点"]] as const;
const groupTabs = [["profiles", "订阅组"], ["templates", "规则模板"], ["settings", "默认设置"]] as const;
const sourceTabKeys = new Set<string>(sourceTabs.map(([k]) => k));
const groupTabKeys = new Set<string>(groupTabs.map(([k]) => k));

function SourcePipeline({ activeTab }: { activeTab: string }) {
  const navigate = useNavigate();
  const sources = useSources();
  const nodes = useManualNodes();
  const profiles = useQuery({ queryKey: ["subscription-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as unknown[] });
  const templates = useTemplates();
  // 可用节点合计：只算检测过的源；一个都没检测过时显示未知
  const checked = sources.data?.items.filter((s) => s.health) ?? [];
  const alive = checked.length ? checked.reduce((n, s) => n + (s.health?.alive ?? 0), 0) : null;
  const steps = [
    { name: "拉取", value: sources.data?.items.length, note: "用 VPS 拉订阅源", active: activeTab === "sources", onSelect: () => navigate("/subscriptions?tab=sources") },
    { name: "手动节点", value: nodes.data?.items.length, note: "导入与去重", active: activeTab === "nodes", onSelect: () => navigate("/subscriptions?tab=nodes") },
    { name: "检测", value: alive, note: "可用节点", active: false, onSelect: () => navigate("/subscriptions?tab=sources") },
    { name: "输出模板", value: (templates.data?.builtin.length ?? 0) + (templates.data?.custom.length ?? 0) || null, note: "按格式与规则", active: activeTab === "templates", onSelect: () => navigate("/groups?tab=templates") },
    { name: "挑选输出", value: profiles.data?.length, note: "按地区倍率组合", active: activeTab === "profiles", onSelect: () => navigate("/groups?tab=profiles") },
  ];
  return (
    <Card data-tour="subscription-pipeline" className="px-[var(--weir-gap-25)] py-[var(--weir-gap-21)]">
      <PipelineBoard label="外部订阅流水线" steps={steps} />
    </Card>
  );
}

export function SubscriptionsPage({ groups = false }: { groups?: boolean }) {
  const [params, setParams] = useSearchParams();
  const rawTab = params.get("tab") ?? "";
  const tabs = groups ? groupTabs : sourceTabs;
  const fallback = groups ? "profiles" : "sources";
  const tab = tabs.some(([k]) => k === rawTab) ? rawTab : fallback;
  const sources = useSources();
  const failedCount = (sources.data?.items ?? []).filter((s) => s.last_error).length;
  useConsoleContext({ page: groups ? "外部订阅组" : "外部订阅源", tab: tabs.find(([k]) => k === tab)?.[1] });
  if (!groups && groupTabKeys.has(rawTab)) return <Navigate to={`/groups?${params.toString()}`} replace />;
  if (groups && sourceTabKeys.has(rawTab)) return <Navigate to={`/subscriptions?${params.toString()}`} replace />;
  return (
    <div className="stack">
      <PageHeader
        size="data"
        title={groups ? "外部订阅组" : "外部订阅源"}
        description={groups ? "合并第三方订阅源与手动节点，套用规则模板生成独立订阅链接（不计入 VPS 流量额度）。" : failedCount > 0 ? `${failedCount} 个订阅源最近一次拉取失败。` : "导入第三方机场订阅与零散节点链接，供外部订阅组聚合转发。"}
      />
      <SourcePipeline activeTab={tab} />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label={groups ? "外部订阅组" : "外部订阅源"}>
          {tabs.map(([k, label]) => <TabsTab key={k} value={k}>{label}</TabsTab>)}
        </TabsList>
        {!groups && <TabsPanel value="sources"><SourcesPanel /></TabsPanel>}
        {!groups && <TabsPanel value="nodes"><NodesPanel /></TabsPanel>}
        {groups && <TabsPanel value="profiles"><ProfilesPanel /></TabsPanel>}
        {groups && <TabsPanel value="templates"><TemplatesPanel /></TabsPanel>}
        {groups && <TabsPanel value="settings"><Card><CardContent><SettingsPanel /></CardContent></Card></TabsPanel>}
      </Tabs>
    </div>
  );
}
