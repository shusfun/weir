// 系统设置：按分区用标签页组织，分区记在地址栏 ?tab=。
import { PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { useSearchParams, useNavigate } from "react-router-dom";
import { useEffect } from "react";
import { useConfirm } from "@weir/ui";
import { hasUnsavedChanges } from "../lib/dirty";
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
  const navigate = useNavigate();
  const confirm = useConfirm();
  const rawTab = params.get("tab");
  useEffect(() => {
    if (rawTab === "distribution") navigate("/pages?tab=clients", { replace: true });
  }, [navigate, rawTab]);
  const tab = rawTab === "backup" ? "data" : systemSections.some((s) => s.key === rawTab) ? rawTab! : systemSections[0].key;
  const dsh = useDsh();
  useConsoleContext({ page: "系统设置", tab: systemSections.find((s) => s.key === tab)?.label });
  if (rawTab === "distribution") return null;
  return (
    <div className="stack">
      <PageHeader title="系统设置" description="管理中心版本来源、登录安全、通知、防御、AI 服务、节点证书与订阅数据。" />
      <Tabs value={tab} onValueChange={async (v) => { if (v === tab || !hasUnsavedChanges() || await confirm({ title: "放弃未保存修改？", description: "切换分区会丢失当前表单中的修改。", confirmText: "放弃修改", tone: "danger" })) setParams({ tab: String(v) }, { replace: true }); }}>
        <TabsList aria-label="系统设置分区">
          {systemSections.map((s) => <TabsTab key={s.key} value={s.key}>{s.label}</TabsTab>)}
        </TabsList>
        <TabsPanel value="update" className="stack">
          <UpdateCard />
          <GitHubSourcesCard />
        </TabsPanel>
        <TabsPanel value="login" className="stack">
          <LogoSettings />
          <SecurityCard />
          <SMTPCard />
        </TabsPanel>
        <TabsPanel value="notify" className="stack">
          <NotifyCard />
        </TabsPanel>
        <TabsPanel value="defense" className="stack">
          <LayaCard />
          <EventsCard dshReady={!!dsh.data?.ready} />
          <BlocksCard />
        </TabsPanel>
        <TabsPanel value="ai" className="stack"><DshSettingsCard /></TabsPanel>
        <TabsPanel value="certs" className="stack"><NodeCertCard /></TabsPanel>
        <TabsPanel value="data" className="stack"><BackupCard /><APITokensCard /></TabsPanel>
      </Tabs>
    </div>
  );
}
