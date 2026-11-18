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
import { UpdateWatcher, UpdateButton } from "./update-watcher";
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
  const current = currentNav(location.pathname, admin);

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

  // 管理员的个人页面收进左下角个人中心；单个条目的收尾组（如系统 → 设置）不重复打印组头小字
  const nav = (compact: boolean) => {
    const visibleGroups = navGroups
      .map((group) => ({ group, items: navigation.filter((item) => item.group === group && visibleFor(item, admin)) }))
      .filter((g) => g.items.length > 0);
    return visibleGroups.map(({ group, items }, idx) => (
      <div className="rail-group" key={group} data-headless={items.length === 1 || visibleGroups.length === 1 || undefined}>
        {items.length > 1 && visibleGroups.length > 1 && idx > 0 && <span className="rail-group-label weir-engraved">{group}</span>}
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
    ));
  };

  const map = useQuery({
    queryKey: ["globe", admin],
    queryFn: async () => api(admin ? "/admin/linkmap" : "/me/linkmap") as Promise<LinkMap>,
    refetchInterval: 60_000,
  });
  const globeNodes = useMemo(() => {
    const gates = (map.data?.gates ?? []).flatMap((g) => g.place?.known && g.place.lat != null && g.place.lon != null ? [{ id: `g${g.id}`, lat: g.place.lat, lon: g.place.lon, tone: !g.online ? "bad" as const : g.state === "failed" || g.state === "pending" ? "warn" as const : "ok" as const, label: g.name || g.place.city }] : []);
    // 出口在别处时也画一个点：航线从入口 VPS 飞到实际出口的位置
    const exits = (map.data?.exits ?? []).flatMap((e) => !e.to_gate && e.kind !== "direct" && e.kind !== "block" && e.place?.known && e.place.lat != null && e.place.lon != null ? [{ id: `e${e.id}`, lat: e.place.lat, lon: e.place.lon, tone: e.probe?.ok === false ? "warn" as const : "info" as const, label: e.label }] : []);
    return [...gates, ...exits];
  }, [map.data]);
  // 航线来自真实中转与用户链路：入口 VPS → 落地 VPS 或外部代理；同一对只画一条
  const globeLinks = useMemo(() => {
    const seen = new Set<string>();
    const out: { from: string; to: string; tone?: "warn" | "ok" | "info" }[] = [];
    const add = (from: string, to: string, warn = false) => {
      if (from === to) return;
      const key = `${from}>${to}`;
      if (seen.has(key)) return;
      seen.add(key);
      out.push({ from, to, tone: warn ? "warn" : undefined });
    };
    for (const e of map.data?.exits ?? []) {
      if (e.kind === "direct" || e.kind === "block") continue;
      if (e.to_gate) add(`g${e.from_gate}`, `g${e.to_gate}`, e.probe?.ok === false);
      else if (e.place?.known) add(`g${e.from_gate}`, `e${e.id}`, e.probe?.ok === false);
    }
    for (const p of map.data?.paths ?? []) {
      const exit = map.data?.exits.find((e) => e.id === p.exit_id);
      if (!exit || exit.kind === "direct" || exit.kind === "block") continue;
      if (exit.to_gate) add(`g${p.gate_id}`, `g${exit.to_gate}`, exit.probe?.ok === false);
      else if (exit.place?.known) add(`g${p.gate_id}`, `e${exit.id}`, exit.probe?.ok === false);
    }
    return out;
  }, [map.data]);
  return (
    <TooltipProvider delay={300}>
      <div className="shell" data-collapsed={collapsed}>
        {globe && <GlobeBackground nodes={globeNodes} links={globeLinks} />}
        <aside className="rail">
          <NavLink to={admin ? "/overview" : "/my/nodes"} className="rail-brand weir-press" aria-label="Weir 首页">
            <BrandMark className="rail-mark" />
            <strong>Weir</strong>
          </NavLink>
          <ScrollArea className="rail-nav" viewportClassName="rail-nav-viewport">
            <nav aria-label="主导航">{nav(collapsed)}</nav>
          </ScrollArea>
          <div className="rail-foot">
            {collapsed ? (
              <Tooltip content="展开导航">
                <Button className="rail-collapse" variant="ghost" size="icon-sm" title="展开导航" aria-label="展开导航" onClick={() => setCollapsed(false)}><PanelLeftOpen /></Button>
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
              {admin && <UpdateButton />}
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
