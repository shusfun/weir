// 控制台外壳：左侧控制条（导航 + 左下角个人中心）、顶栏（标题 / 快速跳转 / 任务托盘 / 主题）、移动端侧边面板。
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { IconTile, Button, Drawer, DrawerContent, GlobeBackground, ScrollArea, Kbd, Menu, MenuTrigger, MenuContent, MenuItem, MenuSeparator, MenuToggle, Tooltip, TooltipProvider } from "@weir/ui";
import { PanelLeftClose, PanelLeftOpen, LogOut, ShieldCheck, Link2, Search, ChevronsUpDown } from "lucide-react";
import { BrandMark } from "./brand-mark";
import { ThemeControl } from "../theme";
import { navigation, navGroups, currentNav, visibleFor } from "../shell/navigation";
import { CommandPalette } from "../shell/command-palette";
import { DshWindow } from "./dsh";
import { DraftConfirm, DraftReadFailure } from "./draft-confirm";
import { CenterOriginBanner } from "./center-origin";
import { NoticesButton } from "./notices";
import { ConsoleEvents, JobWatcher } from "./job";
import { UpdateWatcher } from "./update-watcher";
import { FocusFromURL } from "./object-link";
import { useQuery } from "../lib/query";
import { api } from "../api";
import type { LinkMap } from "../lib/linkmap";
import { GuideButton, GuideWelcome } from "./guide";
import { AppearanceWatch } from "./appearance-panel";
import { readGlobe } from "../appearance";

const collapseKey = "weir-rail-collapsed";
function readCollapsed() {
  try {
    return localStorage.getItem(collapseKey) === "1";
  } catch {
    return false;
  }
}

// 左下角个人中心：头像 + 账户，点开为订阅链接 / 账户安全 / 退出
function AccountMenu({ admin, email, onLogout }: { admin: boolean; email: string; onLogout: () => void }) {
  const navigate = useNavigate();
  return (
    <Menu>
      <MenuTrigger
        render={
          <button type="button" className="weir-key weir-key-ghost rail-account" aria-label={"个人中心：" + email}>
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
        <MenuItem onClick={() => navigate("/links")}><Link2 />订阅链接</MenuItem>
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
  const [globe, setGlobe] = useState(readGlobe);
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
    const on = () => setGlobe(readGlobe());
    window.addEventListener("weir-appearance", on);
    return () => window.removeEventListener("weir-appearance", on);
  }, []);
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

  // 管理员的个人页面收进左下角个人中心；代理与订阅链接是共用页面，两种账户都在导航里
  const nav = (compact: boolean) =>
    navGroups.map((group) => {
      const items = navigation.filter((item) => item.group === group && visibleFor(item, admin));
      if (!items.length) return null;
      return (
        <div className="rail-group" key={group}>
          <span className="rail-group-label weir-engraved">{group}</span>
          {items.map(({ key, path, label, icon: Icon }) => {
            const link = (
              <NavLink key={key} to={path} className={({ isActive }) => "weir-key weir-key-ghost rail-item" + (isActive ? " active" : "")}>
                <Icon />
                <span className="rail-label">{label}</span>
              </NavLink>
            );
            return compact ? <Tooltip key={key} content={label}>{link}</Tooltip> : link;
          })}
        </div>
      );
    });

  const map = useQuery({
    queryKey: ["globe", admin],
    queryFn: async () => api(admin ? "/admin/linkmap" : "/me/linkmap") as Promise<LinkMap>,
    refetchInterval: 60_000,
  });
  const globeNodes = useMemo(() => {
    const gates = (map.data?.gates ?? []).flatMap((g) => g.place?.known && g.place.lat != null && g.place.lon != null ? [{ id: `g${g.id}`, lat: g.place.lat, lon: g.place.lon, tone: g.online ? "ok" as const : "unknown" as const, label: g.name }] : []);
    // 出口在别处时也画一个点：航线从入口 VPS 飞到实际出口的位置
    const exits = (map.data?.exits ?? []).flatMap((e) => e.place?.known && e.place.lat != null && e.place.lon != null ? [{ id: `e${e.id}`, lat: e.place.lat, lon: e.place.lon, tone: "info" as const, label: e.label }] : []);
    return [...gates, ...exits];
  }, [map.data]);
  // 航线只来自真实链路：入口 VPS → 出口所在地；同一对只画一条
  const globeLinks = useMemo(() => {
    const seen = new Set<string>();
    const out: { from: string; to: string }[] = [];
    for (const p of map.data?.paths ?? []) {
      const exit = map.data?.exits.find((e) => e.id === p.exit_id);
      if (!exit?.place?.known) continue;
      const key = `g${p.gate_id}>e${exit.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push({ from: `g${p.gate_id}`, to: `e${exit.id}` });
    }
    return out;
  }, [map.data]);
  return (
    <TooltipProvider delay={300}>
      <div className="shell" data-collapsed={collapsed}>
        {globe && <GlobeBackground nodes={globeNodes} links={globeLinks} />}
        <aside className="rail">
          <NavLink to={admin ? "/gates" : "/links"} className="rail-brand weir-press" aria-label="Weir 首页">
            <BrandMark className="rail-mark" />
            <strong>Weir</strong>
          </NavLink>
          <ScrollArea className="rail-nav" viewportClassName="rail-nav-viewport">
            <nav aria-label="主导航">{nav(collapsed)}</nav>
          </ScrollArea>
          <div className="rail-foot">
            {collapsed ? (
              <Tooltip content="展开导航">
                <Button className="rail-collapse justify-center px-0" variant="ghost" size="sm" aria-label="展开导航" onClick={() => setCollapsed(false)}><PanelLeftOpen /></Button>
              </Tooltip>
            ) : (
              <Button className="rail-collapse justify-start" variant="ghost" size="sm" onClick={() => setCollapsed(true)}><PanelLeftClose /><span>收起导航</span></Button>
            )}
            <AccountMenu admin={admin} email={email} onLogout={onLogout} />
          </div>
        </aside>
        <div className="shell-body">
          <header className="topbar">
            <Button ref={drawerTrigger} className="hidden max-[900px]:inline-flex" variant="ghost" size="icon-sm" aria-label="打开导航" aria-expanded={drawer} onClick={() => setDrawer(true)}>
              <MenuToggle open={drawer} />
            </Button>
            {/* 窄屏才显示；页面标题（h1）在 PageHeader，窄屏下留给读屏，所以这里对读屏隐藏 */}
            <div className="topbar-title" aria-hidden="true">
              <span>{current?.label ?? "Weir"}</span>
            </div>
            {/* 设计稿 .v-cmd：搜索框在左，状态与工具在右 */}
            <button type="button" className="command-trigger weir-key" onClick={() => setPalette(true)} aria-label="搜索、跳转或执行操作">
              <Search />
              <span>搜索、跳转或执行操作</span>
              <Kbd>Ctrl K</Kbd>
            </button>
            <div className="topbar-tools">
              <GuideButton />
              {admin && <NoticesButton />}
              <ThemeControl />
            </div>
          </header>
          {admin && <CenterOriginBanner />}
          {admin && <DraftReadFailure />}
          <GuideWelcome />
          {children}
        </div>
        <Drawer open={drawer} onOpenChange={setDrawer}>
          <DrawerContent title="Weir" footer={<AccountMenu admin={admin} email={email} onLogout={onLogout} />}>
            <nav aria-label="移动导航">{nav(false)}</nav>
          </DrawerContent>
        </Drawer>
        <CommandPalette open={palette} onOpenChange={setPalette} admin={admin} />
        {admin && <DshWindow />}
        {admin && <DraftConfirm />}
        {admin && <JobWatcher />}
        {admin && <UpdateWatcher />}
        <AppearanceWatch />
        <ConsoleEvents />
        <FocusFromURL />
      </div>
    </TooltipProvider>
  );
}
