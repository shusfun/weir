// 按功能组织的导航表：侧边栏、顶栏标题、命令面板与旧地址重定向共用这一份配置。
import {
  LayoutDashboard,
  Server,
  Users,
  KeyRound,
  Workflow,
  Megaphone,
  ListChecks,
  ScrollText,
  ShieldAlert,
  Mail,
  Settings2,
  Link2,
  ShieldCheck,
  Gauge,
  ArrowDownToLine,
  ArrowUpFromLine,
  Route,
  Network,
  Cpu,
  FileBadge,
  Sparkles,
  type LucideIcon,
} from "lucide-react";

export interface NavItem {
  key: string;
  path: string;
  label: string;
  icon: LucideIcon;
  group: (typeof navGroups)[number];
  admin: boolean;
}

export const navGroups = ["工作区", "订阅与分发", "运维", "系统", "个人", "开发"] as const;

export const navigation: NavItem[] = [
  { key: "gates", path: "/gates", label: "VPS 与节点", icon: Server, group: "工作区", admin: true },
  { key: "kernels", path: "/kernels", label: "内核", icon: Cpu, group: "工作区", admin: true },
  { key: "users", path: "/users", label: "用户", icon: Users, group: "工作区", admin: true },
  { key: "subscriptions", path: "/subscriptions", label: "订阅管理", icon: Workflow, group: "订阅与分发", admin: true },
  { key: "pages", path: "/pages", label: "公开页面", icon: Megaphone, group: "订阅与分发", admin: true },
  { key: "audit", path: "/ops/audit", label: "操作审计", icon: ScrollText, group: "运维", admin: true },
  { key: "system", path: "/system", label: "系统设置", icon: Settings2, group: "系统", admin: true },
  { key: "subscription", path: "/subscription", label: "我的订阅", icon: Link2, group: "个人", admin: false },
  { key: "security", path: "/security", label: "账户安全", icon: ShieldCheck, group: "个人", admin: false },
  // 开发环境专用：生产构建里 import.meta.env.DEV 为 false，菜单项和页面一起被剔除
  ...(import.meta.env.DEV
    ? [{ key: "dev-ui", path: "/dev/ui", label: "动效样例", icon: Sparkles, group: "开发" as const, admin: true }]
    : []),
];

export function currentNav(pathname: string) {
  return navigation.find((item) => pathname === item.path || pathname.startsWith(item.path + "/"));
}

// 订阅流水线：来源 → 节点 → 处理 → 模板 → 订阅组
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
  { key: "ai", label: "AI 与 dsh" },
  { key: "defense", label: "安全防御" },
  { key: "login", label: "登录与令牌" },
  { key: "notify", label: "通知" },
  { key: "backup", label: "备份迁移" },
  { key: "distribution", label: "分发" },
] as const;

export const deviceTabs = [
  { key: "", label: "概览", icon: Gauge },
  { key: "inbounds", label: "入站", icon: ArrowDownToLine },
  { key: "grants", label: "客户端授权", icon: KeyRound },
  { key: "outbounds", label: "出站", icon: ArrowUpFromLine },
  { key: "routing", label: "路由", icon: Route },
  { key: "topology", label: "拓扑", icon: Network },
  { key: "cores", label: "内核", icon: Cpu },
  { key: "certificates", label: "证书", icon: FileBadge },
  { key: "tasks", label: "任务", icon: ListChecks },
] as const;
export const deviceTabKeys: readonly string[] = deviceTabs.map((t) => t.key).filter(Boolean);
