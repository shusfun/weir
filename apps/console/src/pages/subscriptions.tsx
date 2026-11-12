// 订阅源：订阅源 → 手动节点 → 订阅组 → 规则模板 → 默认设置。
import { Card, CardContent, PipelineBoard, PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { api } from "../api";
import { SourcesPanel, useSources } from "../components/subscription-sources";
import { useManualNodes } from "../components/subscription-nodes";
import { useTemplates } from "../components/subscription-profiles";
import { NodesPanel } from "../components/subscription-nodes";
import { ProfilesPanel } from "../components/subscription-profiles";
import { SettingsPanel, TemplatesPanel } from "../components/subscription-settings";
import { field, useConsoleContext } from "../lib/console-context";

const tabs = [["sources", "订阅源"], ["nodes", "手动节点"], ["profiles", "订阅组"], ["templates", "规则模板"], ["settings", "默认设置"]] as const;

function SourcePipeline() {
  const sources = useSources();
  const nodes = useManualNodes();
  const profiles = useQuery({ queryKey: ["subscription-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as unknown[] });
  const templates = useTemplates();
  const steps = [
    { name: "订阅源", value: sources.data?.items.length, note: "上游与用量" },
    { name: "节点", value: nodes.data?.items.length, note: "导入与去重" },
    { name: "处理链", value: sources.data?.items.reduce((n, s) => n + (s.operators?.length ?? 0), 0), note: "筛选与重命名" },
    { name: "输出模板", value: (templates.data?.builtin.length ?? 0) + (templates.data?.custom.length ?? 0) || null, note: "按格式与规则" },
    { name: "订阅组", value: profiles.data?.length, note: "组合与分享" },
  ];
  return (
    <Card className="px-[25px] py-[21px]">
      <p className="m-0 mb-[15px] text-meta text-[var(--weir-ink-2)]">节点从左往右流过每一步，每个点是一个对象；数量缺失时显示未知</p>
      <PipelineBoard label="订阅源流水线" steps={steps} />
    </Card>
  );
}

export function SubscriptionsPage() {
  const [params, setParams] = useSearchParams();
  const tab = tabs.some(([k]) => k === params.get("tab")) ? params.get("tab")! : "sources";
  const sources = useSources();
  useConsoleContext({ page: "订阅源", tab: tabs.find(([k]) => k === tab)?.[1] });
  return (
    <div className="stack">
      <PageHeader size="data" title="订阅源" description={sources.data?.items.some((s) => s.last_error) ? "有订阅源最近一次拉取失败。" : "汇集机场订阅与手动节点，按订阅组处理后分配给用户或分享。"} />
      <SourcePipeline />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="订阅源">
          {tabs.map(([k, label]) => <TabsTab key={k} value={k}>{label}</TabsTab>)}
        </TabsList>
        <Card>
          <CardContent>
            <TabsPanel value="sources"><SourcesPanel /></TabsPanel>
            <TabsPanel value="nodes"><NodesPanel /></TabsPanel>
            <TabsPanel value="profiles"><ProfilesPanel /></TabsPanel>
            <TabsPanel value="templates"><TemplatesPanel /></TabsPanel>
            <TabsPanel value="settings"><SettingsPanel /></TabsPanel>
          </CardContent>
        </Card>
      </Tabs>
    </div>
  );
}
