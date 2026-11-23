// 按功能组织的导航表：侧边栏、顶栏标题、命令面板与旧地址重定向共用这一份配置。
import {
  Gauge,
  Globe,
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
  Ticket,
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
  { key: "linkmap", path: "/linkmap", label: "链路", icon: Globe, group: "概览", roles: "admin" },
  { key: "gates", path: "/gates", label: "节点", icon: Server, group: "线路", roles: "admin" },
  { key: "proxies", path: "/proxies", label: "代理", icon: Waypoints, group: "线路", roles: "admin" },
  { key: "rules", path: "/rules", label: "规则", icon: Route, group: "线路", roles: "admin" },
  { key: "kernels", path: "/kernels", label: "内核", icon: Cpu, group: "线路", roles: "admin" },
  { key: "users", path: "/users", label: "用户", icon: Users, group: "用户", roles: "admin" },
  { key: "codes", path: "/codes", label: "兑换码", icon: Ticket, group: "用户", roles: "admin" },
  { key: "links", path: "/links", label: "我的订阅", icon: Link2, group: "订阅", roles: "admin" },
  { key: "proxy-policy", path: "/proxy", label: "客户端分流", icon: Route, group: "订阅", roles: "admin" },
  { key: "subscriptions", path: "/subscriptions", label: "外部订阅源", icon: Rss, group: "订阅", roles: "admin" },
  { key: "groups", path: "/groups", label: "外部订阅组", icon: Layers, group: "订阅", roles: "admin" },
  { key: "pages", path: "/pages", label: "公开页面", icon: Megaphone, group: "订阅", roles: "admin" },
  { key: "observe", path: "/ops/observe", label: "运行观测", icon: Activity, group: "运维", roles: "admin" },
  { key: "audit", path: "/ops/audit", label: "操作审计", icon: ScrollText, group: "运维", roles: "admin" },
  { key: "system", path: "/system", label: "设置", icon: Settings2, group: "系统", roles: "admin" },
  { key: "my-nodes", path: "/my/nodes", label: "我的节点", icon: Server, group: "个人", roles: "user" },
  { key: "my-proxy", path: "/proxy", label: "我的代理", icon: Waypoints, group: "个人", roles: "user" },
  { key: "my-links", path: "/links", label: "我的订阅", icon: Link2, group: "个人", roles: "user" },
  { key: "my-codes", path: "/my/codes", label: "发放兑换码", icon: Ticket, group: "个人", roles: "user" },
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

// 系统设置分区（?tab=）与设置弹窗目录（供设置页与快速跳转共用，不引入具体表单代码分块）
// 设置按用户要办的事分组；key 是地址栏 ?tab= 的稳定值，旧的 appearance / notify / backup 由设置页映射到新分组。
export const systemSections = [
  { key: "me", label: "我的账号" },
  { key: "login", label: "注册与账号" },
  { key: "defense", label: "安全" },
  { key: "certs", label: "节点与下载" },
  { key: "ai", label: "AI" },
  { key: "data", label: "备份与数据" },
  { key: "update", label: "版本与通知" },
] as const;

export interface SystemSettingItem {
  key: string;
  section: (typeof systemSections)[number]["key"];
  title: string;
  description: string;
  keywords: string;
}

export const systemSettingItems: readonly SystemSettingItem[] = [
  { key: "account", section: "me", title: "账户安全", description: "密码、二次验证、通行密钥与已登录设备", keywords: "passkey totp mfa 密码 会话 二步验证" },
  { key: "appearance", section: "me", title: "外观与动效", description: "主题与省电档", keywords: "暗色 明亮 省电 地球 theme" },
  { key: "update", section: "update", title: "版本与更新", description: "中心版本与升级", keywords: "upgrade 升级 自更新 版本" },
  { key: "github", section: "certs", title: "下载来源", description: "内核与更新包的 GitHub 来源与下载策略", keywords: "github 镜像 加速 proxy" },
  { key: "logo", section: "login", title: "登录页标识", description: "站点名称与图标", keywords: "logo 品牌 图标 名称" },
  { key: "security", section: "defense", title: "登录方式", description: "二次验证、通行密钥与注册开关", keywords: "注册 验证 totp 登录" },
  { key: "accounts", section: "login", title: "注册与账号策略", description: "邀请码、兑换码、邮箱后缀与发信限额", keywords: "邀请码 兑换码 邮箱 后缀 altcha 发码" },
  { key: "turnstile", section: "defense", title: "人机验证", description: "Cloudflare Turnstile 配置", keywords: "turnstile captcha 验证码 cloudflare" },
  { key: "smtp", section: "login", title: "邮件服务", description: "登录与通知邮件发送（SMTP）", keywords: "smtp 邮箱 发信 邮件 mail" },
  { key: "notify", section: "update", title: "Telegram 通知", description: "Telegram 告警与运行通知", keywords: "telegram tg bot 通知 告警" },
  { key: "protection", section: "defense", title: "自动安全处置护栏", description: "临时收紧上限、保护网段与域名", keywords: "防御 封禁 护栏 限流 安全" },
  { key: "dsh", section: "ai", title: "dsh 服务与版本", description: "AI 助手地址、反代连通检查与版本管理", keywords: "dsh ai deepseek 助手 反代 版本" },
  { key: "laya", section: "ai", title: "Laya 推理与部署", description: "中心或 VPS 部署 Laya、一键实测启用与运维草稿记录", keywords: "laya ollaya 模型 推理 判断 运维" },
  { key: "ai-profile", section: "ai", title: "AI 偏好与观察点", description: "汇总时区、Laya 兼容范围与各观察点置信度阈值", keywords: "ai profile 观察点 置信度 时区 skill 偏好" },
  { key: "certs", section: "certs", title: "节点证书", description: "ACME 证书来源、申请与自动续签", keywords: "cert ssl tls acme 证书 域名 https 1panel" },
  { key: "retention", section: "data", title: "数据保留", description: "观测与审计历史记录的保留周期", keywords: "清理 天数 日志 保留" },
  { key: "backup", section: "data", title: "备份与恢复", description: "本地导出、导入预览与 WebDAV 自动备份", keywords: "backup webdav 备份 恢复 导出 导入" },
  { key: "api", section: "data", title: "API 令牌", description: "创建与撤销外部接口访问凭据", keywords: "api token 令牌 密钥 接口" },
];
