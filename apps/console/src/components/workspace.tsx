// 控制台外壳：左侧控制条（导航 + 左下角个人中心）、顶栏（标题 / 快速跳转 / 任务托盘 / 主题）、移动端侧边面板。
import { useEffect, useRef, useState, type ReactNode } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { IconTile, Button, Kbd, Menu, MenuTrigger, MenuContent, MenuItem, MenuSeparator, MenuToggle, Sheet, SheetClose, SheetContent, StatusLamp, Tooltip, TooltipProvider } from "@migate/ui";
import { PanelLeftClose, PanelLeftOpen, LogOut, ShieldCheck, Link2, Search, Activity, ChevronsUpDown } from "lucide-react";
import { ThemeControl } from "../theme";
import { navigation, navGroups, currentNav } from "../shell/navigation";
import { CommandPalette } from "../shell/command-palette";
import { DshDrawer } from "./dsh";
import { DraftConfirm } from "./draft-confirm";

const collapseKey = "migate-rail-collapsed";
function readCollapsed() {
  try {
    return localStorage.getItem(collapseKey) === "1";
  } catch {
    return false;
  }
}

// 左下角个人中心：头像 + 账户，点开为我的订阅 / 账户安全 / 退出
function AccountMenu({ admin, email, onLogout }: { admin: boolean; email: string; onLogout: () => void }) {
  const navigate = useNavigate();
  return (
    <Menu>
      <MenuTrigger
        render={
          <button type="button" className="mg-key rail-account"aria-label={"个人中心：" + email}>
            <IconTile size="sm" tone="neutral">{email.slice(0, 1).toUpperCase() || "?"}</IconTile>
            <span className="rail-account-text">
              <strong>{email}</strong>
              <small>{admin ? "管理员" : "个人账户"}</small>
            </span>
            <ChevronsUpDown />
          </button>
        }
      />
      <MenuContent side="top" align="start">
        <MenuItem onClick={() => navigate("/subscription")}><Link2 />我的订阅</MenuItem>
        <MenuItem onClick={() => navigate("/security")}><ShieldCheck />账户安全</MenuItem>
        <MenuSeparator />
        <MenuItem onClick={onLogout}><LogOut />退出登录</MenuItem>
      </MenuContent>
    </Menu>
  );
}

export function Workspace({ admin, email, children, onLogout }: { admin: boolean; email: string; children: ReactNode; onLogout: () => void }) {
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const [drawer, setDrawer] = useState(false);
  const drawerTrigger = useRef<HTMLButtonElement>(null);
  const [palette, setPalette] = useState(false);
  const current = currentNav(location.pathname);

  useEffect(() => {
    try {
      localStorage.setItem(collapseKey, collapsed ? "1" : "0");
    } catch {
      // 仅本次生效
    }
  }, [collapsed]);
  useEffect(() => setDrawer(false), [location.pathname]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // 管理员的个人页面收进左下角个人中心；普通用户只有个人页面，保留在导航中
  const nav = (compact: boolean) =>
    navGroups.map((group) => {
      const items = navigation.filter((item) => item.group === group && (admin ? item.admin : !item.admin));
      if (!items.length) return null;
      return (
        <div className="rail-group" key={group}>
          <span className="rail-group-label mg-engraved">{group}</span>
          {items.map(({ key, path, label, icon: Icon }) => {
            const link = (
              <NavLink key={key} to={path} className={({ isActive }) => "mg-key rail-item" + (isActive ? " active is-pressed" : "")}>
                {({ isActive }) => (
                  <>
                    <Icon />
                    <span className="rail-label">{label}</span>
                    <StatusLamp className="rail-led" size="sm" tone={isActive ? "active" : "off"} />
                  </>
                )}
              </NavLink>
            );
            return compact ? <Tooltip key={key} content={label}>{link}</Tooltip> : link;
          })}
        </div>
      );
    });

  return (
    <TooltipProvider delay={300}>
      <div className="shell" data-collapsed={collapsed}>
        <aside className="rail">
          <NavLink to={admin ? "/gates" : "/subscription"} className="rail-brand" aria-label="MiGate 首页">
            <span className="rail-mark"><Activity size={19} /></span>
            <strong>MiGate</strong>
          </NavLink>
          <nav className="rail-nav" aria-label="主导航">{nav(collapsed)}</nav>
          <div className="rail-foot">
            <Button className="rail-collapse justify-start in-data-[collapsed=true]:justify-center in-data-[collapsed=true]:px-0" variant="ghost" size="sm" aria-label={collapsed ? "展开导航" : "收起导航"} onClick={() => setCollapsed((v) => !v)}>
              {collapsed ? <PanelLeftOpen /> : <><PanelLeftClose /><span>收起导航</span></>}
            </Button>
            <AccountMenu admin={admin} email={email} onLogout={onLogout} />
          </div>
        </aside>
        <div className="shell-body">
          <header className="topbar">
            <Button ref={drawerTrigger} className="hidden max-[900px]:inline-flex" variant="ghost" size="icon-sm" aria-label="打开导航" aria-expanded={drawer} onClick={() => setDrawer(true)}>
              <MenuToggle open={drawer} />
            </Button>
            <div className="topbar-title">
              <h1>{current?.label ?? "MiGate"}</h1>
            </div>
            <div className="topbar-tools">
              <Button className="command-trigger min-w-[220px] justify-start text-[var(--mg-ink-3)] max-[1100px]:min-w-0" variant="outline" size="sm" onClick={() => setPalette(true)} aria-label="快速跳转">
                <Search />
                <span>快速跳转…</span>
                <Kbd>Ctrl K</Kbd>
              </Button>
              <ThemeControl />
            </div>
          </header>
          {children}
        </div>
        <Sheet open={drawer} onOpenChange={setDrawer}>
          <SheetContent
            title="MiGate"
            className="[&_[data-slot=sheet-header]]:pl-[34px]"
            // 关闭后焦点回到顶栏的同一位置
            finalFocus={drawerTrigger}
            // 关闭键与顶栏的打开键在同一位置：抽屉滑到位后三条线在原地变成叉，关闭时倒序变回
            close={
              <SheetClose render={<Button variant="ghost" size="icon-sm" className="absolute top-[11.5px] left-3.5" aria-label="关闭导航" />}>
                <MenuToggle follow />
              </SheetClose>
            }
          >
            <div className="flex h-full flex-col">
              <nav className="rail-nav" aria-label="移动导航">{nav(false)}</nav>
              <div className="rail-foot">
                <AccountMenu admin={admin} email={email} onLogout={onLogout} />
              </div>
            </div>
          </SheetContent>
        </Sheet>
        <CommandPalette open={palette} onOpenChange={setPalette} admin={admin} />
        {admin && <DshDrawer />}
        {admin && <DraftConfirm />}
      </div>
    </TooltipProvider>
  );
}
