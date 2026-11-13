// 系统设置：按分区用标签页组织，分区记在地址栏 ?tab=。
import { PageHeader, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { Link, useSearchParams, useNavigate } from "react-router-dom";
import { useEffect } from "react";
import { GitHubSourcesCard } from "../components/github-sources";
import { LayaCard } from "../components/defense";
import { DshSettingsCard } from "../components/dsh";
import { APITokensCard, BackupCard, NotifyCard, SecurityCard, SMTPCard } from "../components/system-settings";
import { NodeCertCard } from "../components/node-certs";
import { UpdateCard } from "../components/self-update";
import { systemSections } from "../shell/navigation";
import { field, useConsoleContext } from "../lib/console-context";
import { LogoSettings } from "../components/logo-settings";
import { AIProfileCard } from "../components/ai-profile";
import { AccountPolicyCard } from "../components/account-policy";
import { TurnstileSettings } from "../components/turnstile-settings";
import { RetentionSettings } from "../components/retention-settings";
import { ProtectionPolicyCard } from "../features/protection/policy";
import { AppearancePanel } from "../components/appearance-panel";

export function SystemPage() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const rawTab = params.get("tab");
  useEffect(() => {
    if (rawTab === "distribution") navigate("/pages?tab=clients", { replace: true });
  }, [navigate, rawTab]);
  const tab = rawTab === "backup" ? "data" : systemSections.some((s) => s.key === rawTab) ? rawTab! : systemSections[0].key;
  useConsoleContext({ page: "系统设置", tab: systemSections.find((s) => s.key === tab)?.label });
  if (rawTab === "distribution") return null;
  return (
    <div className="stack">
      <PageHeader size="data" title="系统设置" description="先选这台浏览器的外观，再管理版本来源、登录安全、通知、防御、AI 服务、节点证书与订阅数据。" />
      <Tabs value={tab} onValueChange={(v) => { if (v !== tab) setParams({ tab: String(v) }, { replace: true }); }}>
        <TabsList aria-label="系统设置分区">
          {systemSections.map((s) => <TabsTab key={s.key} value={s.key}>{s.label}</TabsTab>)}
        </TabsList>
        <TabsPanel value="appearance" className="stack"><AppearancePanel /></TabsPanel>
        <TabsPanel value="update" className="stack">
          <UpdateCard />
          <GitHubSourcesCard />
        </TabsPanel>
        <TabsPanel value="login" className="stack">
          <LogoSettings />
          <SecurityCard />
          <AccountPolicyCard />
          <TurnstileSettings />
          <SMTPCard />
        </TabsPanel>
        <TabsPanel value="notify" className="stack">
          <NotifyCard />
        </TabsPanel>
        <TabsPanel value="defense" className="stack">
          <p>前往 <Link to="/system?tab=ai">AI 服务设置</Link> 部署和配置 Laya；基础防御不依赖模型。</p>
          <ProtectionPolicyCard />
        </TabsPanel>
        <TabsPanel value="ai" className="stack"><DshSettingsCard /><LayaCard /><AIProfileCard /></TabsPanel>
        <TabsPanel value="certs" className="stack"><NodeCertCard /></TabsPanel>
        <TabsPanel value="data" className="stack"><RetentionSettings /><BackupCard /><APITokensCard /></TabsPanel>
      </Tabs>
    </div>
  );
}
