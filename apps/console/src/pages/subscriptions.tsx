// 订阅管理（MiSub）：订阅源 → 手动节点 → 订阅组 → 规则模板 → 默认设置。
import { Card, CardContent, PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@migate/ui";
import { useSearchParams } from "react-router-dom";
import { SourcesPanel } from "../components/misub-sources";
import { NodesPanel } from "../components/misub-nodes";
import { ProfilesPanel } from "../components/misub-profiles";
import { SettingsPanel, TemplatesPanel } from "../components/misub-settings";

const tabs = [["sources", "订阅源"], ["nodes", "手动节点"], ["profiles", "订阅组"], ["templates", "规则模板"], ["settings", "默认设置"]] as const;

export function SubscriptionsPage() {
  const [params, setParams] = useSearchParams();
  const tab = tabs.some(([k]) => k === params.get("tab")) ? params.get("tab")! : "sources";
  return (
    <div className="stack">
      <PageHeader eyebrow="订阅与分发" title="订阅管理" description="汇集机场订阅与手动节点，按订阅组处理后分配给用户或分享。" />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="订阅管理">
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
