// 系统设置：按要办的事分组的一页列表。开关在行上生效，要填的项打开同名弹窗；行尾只写需要关注的状态。
import { Dialog, DialogContent, DialogDescription, DialogTitle, InsetGroup, PageHeader, useToast } from "@weir/ui";
import { SaveStatusBar, useSaveStatus } from "../components/save-status";
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
import { AccountRows, AIRows, DataRows, DefenseRows, LoginRows, NodeRows, UpdateRows } from "./system-rows";

// 旧分区地址映射到现在的分组，保留已分享的 ?tab= 链接
const legacyTabs: Record<string, string> = { backup: "data", appearance: "me", notify: "update" };

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
    if (rawTab) document.getElementById("settings-" + (legacyTabs[rawTab] ?? rawTab))?.scrollIntoView({ block: "start" });
  }, [navigate, rawTab]);
  useConsoleContext({ page: "系统设置", tab: selected?.title, dialog: selected ? { title: selected.title } : undefined });
  const rows: Record<string, ReactNode> = {
    me: <AccountRows open={settingDialog.open} />,
    login: <LoginRows open={settingDialog.open} />,
    defense: <DefenseRows open={settingDialog.open} />,
    certs: <NodeRows open={settingDialog.open} />,
    ai: <AIRows open={settingDialog.open} />,
    data: <DataRows open={settingDialog.open} />,
    update: <UpdateRows open={settingDialog.open} />,
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
    {selected && renderSelected && <SettingDialog key={selected.key} title={selected.title} description={selected.description} close={settingDialog.close}>{renderSelected()}</SettingDialog>}
  </div>;
}

// 设置弹窗外壳：底部固定显示操作状态；操作进行中不关闭，避免看不到结果。
function SettingDialog({ title, description, close, children }: { title: string; description?: ReactNode; close: () => void; children: ReactNode }) {
  const save = useSaveStatus();
  const toast = useToast();
  return <Dialog open onOpenChange={(open) => {
    if (open) return;
    if (save.busy) { toast("操作还没完成，完成后再关闭", { tone: "warn" }); return; }
    close();
  }}>
    <DialogContent size="xl"><DialogTitle>{title}</DialogTitle><DialogDescription>{description}</DialogDescription>
      <SettingsDialogProvider value={true}><div className="stack">{children}</div></SettingsDialogProvider>
      <SaveStatusBar {...save} close={close} />
    </DialogContent>
  </Dialog>;
}
