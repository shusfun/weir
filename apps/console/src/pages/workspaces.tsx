// 按功能拆分后的工作区：订阅流水线、分发与公开、系统设置、邮件队列。各区内部子页都是真实路由。
import { Navigate, NavLink, useParams, useSearchParams } from "react-router-dom";
import { Steps, Step, PageHeader, Segmented } from "@migate/ui";
import { ChevronRight } from "lucide-react";
import { InventoryPage } from "./inventory";
import { SettingsPage, MailQueue } from "./settings";
import { SubscriptionDefaultsEditor } from "../components/subscription-defaults";
import { SubscriptionNamingEditor } from "../components/subscription-naming";
import { ClientCatalogEditor } from "../components/client-catalog";
import { ClientMirrorsEditor } from "../components/client-mirrors";
import { AnnouncementEditor } from "../components/global-announcement";
import { TabNav } from "../shell/tab-nav";
import { subscriptionSteps, subscriptionKinds, distributionSections, systemSections } from "../shell/navigation";

function PipelineNav({ current }: { current: string }) {
  return (
    <nav aria-label="订阅流水线">
      <Steps orientation="horizontal">
        {subscriptionSteps.map((step, index) => (
          <Step
            key={step.kind}
            index={index}
            label={step.label}
            caption={step.caption}
            status={step.kind === current ? "current" : "upcoming"}
            separator={<ChevronRight size={16} />}
            render={<NavLink to={"/subscriptions/" + step.kind} />}
          />
        ))}
      </Steps>
    </nav>
  );
}

const operatorViews = [
  ["chains", "处理链"],
  ["defaults", "全局默认处理"],
  ["naming", "前缀与地区"],
] as const;

export function SubscriptionsWorkspace() {
  const { kind = "" } = useParams();
  const [params, setParams] = useSearchParams();
  if (!subscriptionKinds.includes(kind)) return <Navigate to="/subscriptions/sources" replace />;
  const view = kind === "operators" ? (operatorViews.find(([k]) => k === params.get("view"))?.[0] ?? "chains") : "chains";
  return (
    <div className="stack">
      <PipelineNav current={kind} />
      {kind === "operators" && (
        <Segmented aria-label="处理链视图" value={view} options={operatorViews} onValueChange={(v) => setParams(v === "chains" ? {} : { view: v })} />
      )}
      {view === "defaults" ? (
        <SubscriptionDefaultsEditor />
      ) : view === "naming" ? (
        <SubscriptionNamingEditor />
      ) : (
        <InventoryPage key={kind} kind={kind} />
      )}
    </div>
  );
}

export function DistributionWorkspace() {
  const { section = "" } = useParams();
  if (!distributionSections.some((s) => s.key === section)) return <Navigate to="/distribution/pages" replace />;
  return (
    <div className="stack">
      <TabNav label="分发分区" items={distributionSections.map((s) => ({ to: "/distribution/" + s.key, label: s.label }))} />
      {section === "pages" ? <InventoryPage key="public-page" kind="public-page" /> : section === "clients" ? <div className="stack"><ClientMirrorsEditor /><ClientCatalogEditor /></div> : <AnnouncementEditor />}
    </div>
  );
}

export function SystemWorkspace({ loginRequired }: { loginRequired: () => void }) {
  const { section = "" } = useParams();
  if (!systemSections.some((s) => s.key === section)) return <Navigate to="/system/security" replace />;
  return (
    <div className="stack">
      <TabNav label="系统设置分区" items={systemSections.map((s) => ({ to: "/system/" + s.key, label: s.label }))} />
      <SettingsPage key={section} section={section} loginRequired={loginRequired} />
    </div>
  );
}

export function MailPage() {
  return <MailQueue />;
}
