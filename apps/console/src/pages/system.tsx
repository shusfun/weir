// 系统设置：按分区用标签页组织（AI 与 dsh / 安全防御 / 登录与令牌 / 通知 / 备份迁移 / 分发），分区记在地址栏 ?tab=。
import { Card, CardContent, PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@migate/ui";
import { useSearchParams } from "react-router-dom";
import { AnnouncementEditor } from "../components/global-announcement";
import { ClientCatalogEditor } from "../components/client-catalog";
import { ClientMirrorsEditor } from "../components/client-mirrors";
import { BlocksCard, EventsCard, LayaCard } from "../components/defense";
import { DshSettingsCard, useDsh } from "../components/dsh";
import { APITokensCard, BackupCard, NotifyCard, SecurityCard, SMTPCard } from "../components/system-settings";
import { systemSections } from "../shell/navigation";

export function SystemPage() {
  const [params, setParams] = useSearchParams();
  const tab = systemSections.some((s) => s.key === params.get("tab")) ? params.get("tab")! : systemSections[0].key;
  const dsh = useDsh();
  return (
    <div className="stack">
      <PageHeader eyebrow="系统" title="系统设置" />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="系统设置分区">
          {systemSections.map((s) => <TabsTab key={s.key} value={s.key}>{s.label}</TabsTab>)}
        </TabsList>
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
        <TabsPanel value="backup" className="stack"><BackupCard /></TabsPanel>
        <TabsPanel value="distribution" className="stack">
          <Card><CardContent><AnnouncementEditor /></CardContent></Card>
          <Card><CardContent><ClientCatalogEditor /></CardContent></Card>
          <Card><CardContent><ClientMirrorsEditor /></CardContent></Card>
        </TabsPanel>
      </Tabs>
    </div>
  );
}
