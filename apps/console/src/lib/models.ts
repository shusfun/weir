export interface Account {
  id: number;
  email: string;
  role: "admin" | "user";
  enabled: boolean;
  quota: number;
  used: number;
  expires: number;
  email_verified: boolean;
  totp_enrolled: boolean;
  created_at: string;
}
export interface Device {
  id: number;
  name: string;
  status: string;
  last_seen: string;
  fingerprint: string;
  metadata: Record<string, unknown>;
  pair_expires: string;
}
export interface Grant {
  user_email?: string;
  id: number;
  user_id: number;
  device_id: number;
  inbound_id: number;
  enabled: boolean;
  status: string;
  allocated: number;
  used: number;
}
export interface Resource {
  id: number;
  kind: string;
  owner_id: number;
  key: string;
  data: Record<string, unknown>;
  node?: {valid:boolean;protocol?:string;server?:string;port?:number};
}
export interface Task {
  id: number;
  device_id: number;
  operation: string;
  status: string;
  revision: number;
  created_at: string;
  result?: unknown;
  attempts: number;
  expires_at?: string;
  leased_until?: string;
}
export interface Items<T> {
  items: T[];
  total?: number;
  page?: number;
  page_size?: number;
}
export function bytes(n: number | undefined) {
  if (n === undefined || !Number.isFinite(n)) return "未知";
  if (n === 0) return "0 B";
  const i = Math.min(4, Math.floor(Math.log(Math.abs(n)) / Math.log(1024)));
  return (
    (n / 1024 ** i).toLocaleString("zh-CN", { maximumFractionDigits: 2 }) +
    " " +
    ["B", "KiB", "MiB", "GiB", "TiB"][i]
  );
}
export function date(value: string | number | undefined) {
  if (!value || String(value).startsWith("0001")) return "尚无记录";
  const d = new Date(typeof value === "number" ? value * 1000 : value);
  return Number.isNaN(+d)
    ? "未知"
    : d.toLocaleString("zh-CN", { hour12: false });
}
export const labels: Record<string, string> = {
  active:"运行中",inactive:"已停止",unknown:"未知",
  enabled: "已启用",
  disabled: "已停用",
  approved: "在线",
  offline: "离线",
  pending: "待处理",
  recovery: "待恢复",
  revoked: "已撤销",
  applied: "已生效",
  unapplied: "待首次应用",
  running: "执行中",
  failed: "失败",
  expired: "已过期",
  queued: "待执行",
  success: "已完成",
  completed: "已完成",
  sent: "已发送",
};

export interface SourceUsage {
 status: "empty" | "available" | "partial" | "unknown";
 source: "upstream_subscription_userinfo";
 observed_at: string;
 included: number; excluded: number; disabled: number; unknown_sources: number;
 upload: string | null; download: string | null; total: string | null;
 remaining: string | null; expire: string | null;
}

export interface CatalogClient {id:string;name:string;description:string;platforms:string[];url:string;repo:string;icon:string;icon_data?:string;format:string;import:"copy"|"clash";enabled:boolean}
export interface ClientCatalog {clients:CatalogClient[];revision:string}
