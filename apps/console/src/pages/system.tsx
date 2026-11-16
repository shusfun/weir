// 系统设置：一页分组列表。开关在行上生效，要填的项显示当前值并在弹窗里改。
import { Dialog, DialogContent, DialogDescription, DialogTitle, InsetGroup, PageHeader } from "@weir/ui";
import { SecurityPage } from "./security";
import type { ReactNode } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import { useEffect } from "react";
import { GitHubSourcesCard } from "../components/github-sources";
import { LayaCard } from "../components/defense";
import { DshSettingsCard } from "../components/dsh";
import { APITokensCard, BackupCard, NotifyCard, SecurityCard, SMTPCard } from "../components/system-settings";
import { NodeCertCard } from "../components/node-certs";
import { UpdateCard } from "../components/self-update";
import { systemSections } from "../shell/navigation";
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

type SettingEntry = { key: string; section: string; title: string; description: string; content: () => ReactNode };
const entries: SettingEntry[] = [
  { key: "account", section: "me", title: "我的账户", description: "密码、二次验证、通行密钥与已登录设备", content: () => <SecurityPage loginRequired={() => window.location.reload()} /> },
  { key: "appearance", section: "appearance", title: "外观与动效", description: "主题与省电档", content: () => <AppearancePanel /> },
  { key: "update", section: "update", title: "版本与更新", description: "中心版本与升级", content: () => <UpdateCard /> },
  { key: "github", section: "update", title: "下载来源", description: "GitHub 来源与下载策略", content: () => <GitHubSourcesCard /> },
  { key: "logo", section: "login", title: "登录页标识", description: "站点名称与图标", content: () => <LogoSettings /> },
  { key: "security", section: "login", title: "登录与注册", description: "全局登录方式与安全策略", content: () => <SecurityCard /> },
  { key: "accounts", section: "login", title: "注册与账号策略", description: "注册、验证与账户限制", content: () => <AccountPolicyCard /> },
  { key: "turnstile", section: "login", title: "人机验证", description: "Turnstile 配置", content: () => <TurnstileSettings /> },
  { key: "smtp", section: "login", title: "邮件服务", description: "登录与通知邮件发送", content: () => <SMTPCard /> },
  { key: "notify", section: "notify", title: "通知渠道", description: "接收告警与运行通知", content: () => <NotifyCard /> },
  { key: "protection", section: "defense", title: "防御", description: "基础防御与访问策略", content: () => <ProtectionPolicyCard /> },
  { key: "dsh", section: "ai", title: "AI 助手", description: "dsh 服务与模型设置", content: () => <DshSettingsCard /> },
  { key: "laya", section: "ai", title: "Laya", description: "部署与模型防御", content: () => <LayaCard /> },
  { key: "ai-profile", section: "ai", title: "AI 偏好", description: "助手使用的个人信息与偏好", content: () => <AIProfileCard /> },
  { key: "certs", section: "certs", title: "证书", description: "证书来源、申请与续签", content: () => <NodeCertCard /> },
  { key: "retention", section: "data", title: "数据保留", description: "历史记录的保留周期", content: () => <RetentionSettings /> },
  { key: "backup", section: "data", title: "备份与恢复", description: "导出、本地导入预览与 WebDAV", content: () => <BackupCard /> },
  { key: "api", section: "data", title: "API 令牌", description: "创建与撤销接口访问凭据", content: () => <APITokensCard /> },
];

export function SystemPage() {
  const [params] = useSearchParams();
  const settingDialog = useStringDialogParam("setting");
  const navigate = useNavigate();
  const rawTab = params.get("tab");
  const selected = entries.find((entry) => entry.key === settingDialog.value);
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
    <PageHeader size="data" title="设置" description="开关在这一页生效。要填写的项显示当前值，点开后修改。" />
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
    {selected && <Dialog open onOpenChange={(open) => { if (!open) settingDialog.close(); }}>
      <DialogContent size="xl"><DialogTitle>{selected.title}</DialogTitle><DialogDescription>{selected.description}</DialogDescription><div className="stack" key={selected.key}>{selected.content()}</div></DialogContent>
    </Dialog>}
  </div>;
}
