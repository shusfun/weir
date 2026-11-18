// 系统设置：一页分组列表。开关在行上生效，要填的项显示当前值并在弹窗里改。
import { Dialog, DialogContent, DialogDescription, DialogTitle, InsetGroup, PageHeader } from "@weir/ui";
import { SecurityPage } from "./security";
import type { ReactNode } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import { useEffect } from "react";
import { GitHubSourcesCard } from "../components/github-sources";
import { LayaCard } from "../components/defense";
import { DshSettingsCard } from "../components/dsh";
import { APITokensCard, BackupCard, NotifyCard, SecurityCard, SettingsDialogProvider, SMTPCard } from "../components/system-settings";
import { NodeCertCard } from "../components/node-certs";
import { UpdateCard } from "../components/self-update";
import { systemSections, systemSettingItems } from "../shell/navigation";
import { useConsoleContext } from "../lib/console-context";
import { LogoSettings } from "../components/logo-settings";
import { AIProfileCard } from "../components/ai-profile";
import { AccountPolicyCard } from "../components/account-policy";
import { TurnstileSettings } from "../components/turnstile-settings";
import { RetentionSettings } from "../components/retention-settings";
import { ProtectionPolicyCard } from "../features/protection/policy";
import { AppearancePanel } from "../components/appearance-panel";
import { useStringDialogParam } from "../components/object-link";
import { AccountRows, AIRows, AppearanceRows, CertRows, DataRows, DefenseRows, LoginRows, NotifyRows, UpdateRows } from "./system-rows";

const settingCards: Record<string, () => ReactNode> = {
  account: () => <SecurityPage loginRequired={() => window.location.reload()} />,
  appearance: () => <AppearancePanel />,
  update: () => <UpdateCard />,
  github: () => <GitHubSourcesCard />,
  logo: () => <LogoSettings />,
  security: () => <SecurityCard />,
  accounts: () => <AccountPolicyCard />,
  turnstile: () => <TurnstileSettings />,
  smtp: () => <SMTPCard />,
  notify: () => <NotifyCard />,
  protection: () => <ProtectionPolicyCard />,
  dsh: () => <DshSettingsCard />,
  laya: () => <LayaCard />,
  "ai-profile": () => <AIProfileCard />,
  certs: () => <NodeCertCard />,
  retention: () => <RetentionSettings />,
  backup: () => <BackupCard />,
  api: () => <APITokensCard />,
};

export function SystemPage() {
  const [params] = useSearchParams();
  const settingDialog = useStringDialogParam("setting");
  const navigate = useNavigate();
  const rawTab = params.get("tab");
  const selected = systemSettingItems.find((entry) => entry.key === settingDialog.value);
  const renderSelected = selected ? settingCards[selected.key] : undefined;
  useEffect(() => {
    if (rawTab === "distribution") { navigate("/pages?tab=clients", { replace: true }); return; }
    if (rawTab) document.getElementById("settings-" + (rawTab === "backup" ? "data" : rawTab))?.scrollIntoView({ block: "start" });
  }, [navigate, rawTab]);
  useConsoleContext({ page: "系统设置", tab: selected?.title, dialog: selected ? { title: selected.title } : undefined });
  const rows: Record<string, ReactNode> = {
    me: <AccountRows open={settingDialog.open} />,
    appearance: <AppearanceRows open={settingDialog.open} />,
    update: <UpdateRows open={settingDialog.open} />,
    login: <LoginRows open={settingDialog.open} />,
    notify: <NotifyRows open={settingDialog.open} />,
    defense: <DefenseRows open={settingDialog.open} />,
    ai: <AIRows open={settingDialog.open} />,
    certs: <CertRows open={settingDialog.open} />,
    data: <DataRows open={settingDialog.open} />,
  };
  return <div className="stack">
    <PageHeader size="data" title="设置" />
    <div className="settings-board">
      <nav className="settings-index" aria-label="设置分区">{systemSections.map((section) => <a key={section.key} href={"#settings-" + section.key}>{section.label}</a>)}</nav>
      <div className="stack">
        {systemSections.map((section) => <section id={"settings-" + section.key} key={section.key} data-tour={"settings-" + section.key}>
          <InsetGroup title={section.label}>
            <div className="settings-group">{rows[section.key]}</div>
          </InsetGroup>
        </section>)}
      </div>
    </div>
    {selected && renderSelected && <Dialog open onOpenChange={(open) => { if (!open) settingDialog.close(); }}>
      <DialogContent size="xl"><DialogTitle>{selected.title}</DialogTitle><DialogDescription>{selected.description}</DialogDescription><SettingsDialogProvider value={true}><div className="stack" key={selected.key}>{renderSelected()}</div></SettingsDialogProvider></DialogContent>
    </Dialog>}
  </div>;
}
