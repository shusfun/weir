// 系统设置：按分区用标签页组织（版本与更新 / AI 与 dsh / 安全防御 / 登录与令牌 / 通知 / 节点证书 / 备份迁移 / 分发），分区记在地址栏 ?tab=。
import { Card, CardContent, PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { useSearchParams } from "react-router-dom";
import { AnnouncementEditor } from "../components/global-announcement";
import { ClientCatalogEditor } from "../components/client-catalog";
import { GitHubSourcesCard } from "../components/github-sources";
import { BlocksCard, EventsCard, LayaCard } from "../components/defense";
import { DshSettingsCard, useDsh } from "../components/dsh";
import { APITokensCard, BackupCard, NotifyCard, SecurityCard, SMTPCard } from "../components/system-settings";
import { NodeCertCard } from "../components/node-certs";
import { UpdateCard } from "../components/self-update";
import { systemSections } from "../shell/navigation";
import { field, useConsoleContext } from "../lib/console-context";
import { LogoSettings } from "../components/logo-settings";

export function SystemPage() {
  const [params, setParams] = useSearchParams();
  const tab = systemSections.some((s) => s.key === params.get("tab")) ? params.get("tab")! : systemSections[0].key;
  const dsh = useDsh();
  useConsoleContext({ page: "系统设置", tab: systemSections.find((s) => s.key === tab)?.label });
  return (
    <div className="stack">
      <PageHeader title="系统设置" description="版本更新、AI 与 dsh、安全、登录、通知、证书、备份与分发。" />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="系统设置分区">
          {systemSections.map((s) => <TabsTab key={s.key} value={s.key}>{s.label}</TabsTab>)}
        </TabsList>
        <TabsPanel value="update" className="stack">
          <UpdateCard />
          <Card><CardContent><GitHubSourcesCard /></CardContent></Card>
        </TabsPanel>
        <TabsPanel value="ai" className="stack"><DshSettingsCard /></TabsPanel>
        <TabsPanel value="defense" className="stack">
          <EventsCard dshReady={!!dsh.data?.ready} />
          <BlocksCard />
          <LayaCard />
        </TabsPanel>
        <TabsPanel value="login" className="stack">
          <SecurityCard />
          <APITokensCard />
        </TabsPanel>
        <TabsPanel value="notify" className="stack">
          <SMTPCard />
          <NotifyCard />
        </TabsPanel>
        <TabsPanel value="certs" className="stack"><NodeCertCard /></TabsPanel>
        <TabsPanel value="backup" className="stack"><BackupCard /></TabsPanel>
        <TabsPanel value="distribution" className="stack">
          <Card><CardContent><LogoSettings /></CardContent></Card>
          <Card><CardContent><AnnouncementEditor /></CardContent></Card>
          <Card><CardContent><ClientCatalogEditor /></CardContent></Card>
        </TabsPanel>
      </Tabs>
    </div>
  );
}
