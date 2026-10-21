import { useState, type ReactNode, type ComponentType } from "react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogTitle,
  DialogDescription,
} from "@migate/ui";
import {
  Menu,
  ChevronLeft,
  ChevronRight,
  LogOut,
  Activity,
  ShieldCheck,
  ArrowUpRight,
} from "lucide-react";
import { NavLink } from "react-router-dom";
type Navigation = readonly (readonly [
  string,
  string,
  ComponentType<{ size?: number }>,
])[];
const sections = [
  ["工作空间", ["overview", "users", "devices", "grants"]],
  ["订阅与分发", ["subscriptions", "subscription"]],
  [
    "运维与安全",
    ["routing", "tasks", "audit", "events", "settings", "security"],
  ],
] as const;
export function Workspace({
  navigation,
  admin,
  email,
  children,
  onLogout,
}: {
  navigation: Navigation;
  admin: boolean;
  email: string;
  children: ReactNode;
  onLogout: () => void;
}) {
  const [collapsed, C] = useState(false);
  const [open, O] = useState(false);
  const links = (
    <>
      {sections.map(([title, keys]) => {
        const items = navigation.filter(
          ([key]) =>
            (keys as readonly string[]).includes(key) &&
            (admin || ["subscription", "security"].includes(key)),
        );
        return items.length ? (
          <div className="nav-section" key={title}>
            <span className="nav-section-label">{title}</span>
            {items.map(([key, label, Icon]) => (
              <NavLink
                key={key}
                to={"/" + key}
                className={({ isActive }) =>
                  "nav-item" + (isActive ? " active" : "")
                }
                onClick={() => O(false)}
                title={label}
              >
                <Icon size={18} />
                <span>{label}</span>
              </NavLink>
            ))}
          </div>
        ) : null;
      })}
    </>
  );
  return (
    <div className={"workspace" + (collapsed ? " is-collapsed" : "")}>
      <aside className="desktop-sidebar">
        <NavLink to={admin ? "/overview" : "/subscription"} className="brand">
          <span className="brand-mark">
            <Activity size={22} />
          </span>
          <span>
            MiGate<span className="brand-caption">CONTROL CONSOLE</span>
          </span>
        </NavLink>
        <nav aria-label="主导航">{links}</nav>
        <div className="sidebar-bottom">
          <div className="environment">
            <span className="status-dot" />
            <span>统一控制中心</span>
            <ShieldCheck size={14} />
          </div>
          <div className="account-identity">
            <span className="avatar">{email.slice(0, 1).toUpperCase()}</span>
            <span className="account-text">
              <strong>{admin ? "管理员" : "个人账户"}</strong>
              <small>{email}</small>
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="退出登录"
              onClick={onLogout}
            >
              <LogOut size={16} />
            </Button>
          </div>
        </div>
        <Button
          className="collapse-control"
          variant="outline"
          size="icon-sm"
          aria-label={collapsed ? "展开导航" : "收起导航"}
          onClick={() => C((v) => !v)}
        >
          {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
        </Button>
      </aside>
      <div className="workspace-body">
        <div className="global-bar">
          <div className="actions">
            <Button
              className="mobile-menu"
              variant="ghost"
              size="icon-sm"
              aria-label="打开导航"
              onClick={() => O(true)}
            >
              <Menu size={20} />
            </Button>
            <span className="breadcrumb">
              工作空间 <span>/</span> {admin ? "管理控制台" : "用户中心"}
            </span>
          </div>
          <div className="actions">
            <span className="console-label">
              <span className="status-dot" />
              安全会话
            </span>
            <NavLink
              to="/security"
              className="header-account"
              aria-label="账户安全"
            >
              {email.slice(0, 1).toUpperCase()}
            </NavLink>
          </div>
        </div>
        {children}
        <footer className="workspace-footer">
          <span>MiGate · 独立节点，统一管理</span>
          <a
            href="https://github.com/imzyb/MiGate"
            target="_blank"
            rel="noreferrer"
          >
            项目 <ArrowUpRight size={12} />
          </a>
        </footer>
      </div>
      <Dialog open={open} onOpenChange={O}>
        <DialogContent
          className="navigation-drawer"
          style={{ translate: "none" }}
        >
          <DialogTitle>MiGate</DialogTitle>
          <DialogDescription>工作空间导航</DialogDescription>
          <nav aria-label="移动导航">{links}</nav>
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onLogout}>
              退出登录
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
