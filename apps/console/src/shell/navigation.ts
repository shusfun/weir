// 按功能组织的导航表：侧边栏、顶栏标题、命令面板与旧地址重定向共用这一份配置。
import {
  Gauge,
  Layers,
  Server,
  Users,
  Megaphone,
  ScrollText,
  Activity,
  Settings2,
  Link2,
  ShieldCheck,
  Cpu,
  Sparkles,
  Route,
  Rss,
  Waypoints,
  type LucideIcon,
} from "lucide-react";

// roles：admin 只给管理员；user 只给普通用户；all 两者共用同一个页面，内容按权限显示（管理员可按用户筛选）。
export interface NavItem {
  key: string;
  path: string;
  label: string;
  icon: LucideIcon;
  group: (typeof navGroups)[number];
  roles: "admin" | "user" | "all";
}

export const navGroups = ["概览", "线路", "用户", "订阅", "运维", "系统", "个人", "开发"] as const;

export function visibleFor(item: NavItem, admin: boolean) {
  return item.roles === "all" || (item.roles === "admin") === admin;
}

export const navigation: NavItem[] = [
  { key: "overview", path: "/overview", label: "概览", icon: Gauge, group: "概览", roles: "admin" },
  { key: "gates", path: "/gates", label: "节点", icon: Server, group: "线路", roles: "admin" },
  { key: "proxies", path: "/proxies", label: "代理", icon: Waypoints, group: "线路", roles: "admin" },
  { key: "rules", path: "/rules", label: "规则", icon: Route, group: "线路", roles: "admin" },
  { key: "kernels", path: "/kernels", label: "内核", icon: Cpu, group: "线路", roles: "admin" },
  { key: "users", path: "/users", label: "用户", icon: Users, group: "用户", roles: "admin" },
  { key: "accounts", path: "/accounts", label: "套餐与用码", icon: Users, group: "用户", roles: "admin" },
  { key: "links", path: "/links", label: "我的订阅", icon: Link2, group: "订阅", roles: "admin" },
  { key: "subscriptions", path: "/subscriptions", label: "订阅源", icon: Rss, group: "订阅", roles: "admin" },
  { key: "groups", path: "/groups", label: "订阅组与模板", icon: Layers, group: "订阅", roles: "admin" },
  { key: "proxy-policy", path: "/proxy", label: "订阅策略", icon: Route, group: "订阅", roles: "admin" },
  { key: "pages", path: "/pages", label: "公开页面", icon: Megaphone, group: "订阅", roles: "admin" },
  { key: "observe", path: "/ops/observe", label: "运行观测", icon: Activity, group: "运维", roles: "admin" },
  { key: "audit", path: "/ops/audit", label: "操作审计", icon: ScrollText, group: "运维", roles: "admin" },
  { key: "system", path: "/system", label: "设置", icon: Settings2, group: "系统", roles: "admin" },
  { key: "my-nodes", path: "/my/nodes", label: "我的节点", icon: Server, group: "个人", roles: "user" },
  { key: "my-proxy", path: "/proxy", label: "我的代理", icon: Waypoints, group: "个人", roles: "user" },
  { key: "my-links", path: "/links", label: "我的订阅", icon: Link2, group: "个人", roles: "user" },
  { key: "security", path: "/security", label: "账户安全", icon: ShieldCheck, group: "个人", roles: "user" },
  // 开发环境专用：生产构建里 import.meta.env.DEV 为 false，菜单项和页面一起被剔除
  ...(import.meta.env.DEV
    ? [{ key: "dev-ui", path: "/dev/ui", label: "动效样例", icon: Sparkles, group: "开发" as const, roles: "admin" as const }]
    : []),
];

export function currentNav(pathname: string, admin = true) {
  return navigation.find((item) => visibleFor(item, admin) && (pathname === item.path || pathname.startsWith(item.path + "/")));
}

// 订阅源页的流水线：来源 → 节点 → 处理 → 模板 → 订阅组
export const subscriptionSteps = [
  { kind: "sources", label: "订阅源", caption: "上游订阅与用量" },
  { kind: "nodes", label: "节点", caption: "导入 · 分组 · 去重" },
  { kind: "operators", label: "处理链", caption: "筛选 · 重命名 · 默认" },
  { kind: "templates", label: "输出模板", caption: "策略组与规则" },
  { kind: "profiles", label: "订阅组", caption: "组合 · 预览 · 分享" },
] as const;
export const subscriptionKinds: readonly string[] = subscriptionSteps.map((s) => s.kind);

export const distributionSections = [
  { key: "pages", label: "公开页面" },
  { key: "clients", label: "客户端目录" },
  { key: "announcements", label: "全局公告" },
] as const;

// 系统设置分区（?tab=）
export const systemSections = [
  { key: "me", label: "我的账户" },
  { key: "appearance", label: "外观" },
  { key: "update", label: "版本与更新" },
  { key: "login", label: "登录与注册" },
  { key: "notify", label: "通知" },
  { key: "defense", label: "防御" },
  { key: "ai", label: "AI 与 dsh" },
  { key: "certs", label: "证书" },
  { key: "data", label: "数据" },
] as const;
