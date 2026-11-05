// 新中心接口的类型与通用换算（Gate、节点、用户、链接、链式代理、订阅）。
import type { LampTone } from "@weir/ui";

export interface Gate {
  id: number;
  name: string;
  status: "active" | "revoked";
  online: boolean;
  state: "applied" | "pending" | "failed" | "revoked";
  public_host: string;
  version: string;
  hostname: string;
  arch: string;
  last_error: string;
  last_seen_at?: string;
  kernels_running?: Record<string, string>;
  // Gate 上报的 1Panel 状态；缺失表示还没有上报（未知）。
  panel?: { kind?: string; http01_webroot?: boolean; detected?: boolean; configured?: boolean; version?: string; error?: string } | null;
  // 本机防火墙（ufw / firewalld / iptables / none）；缺失表示这次没读到（未知）。
  firewall?: { tool: string; active: boolean; rules?: string[]; error?: string } | null;
  // 单机安装时建议的公网地址（本机网卡上的公网 IP）；colocated：中心与这台 VPS 在同一台机器上。
  public_host_hint?: string;
  colocated?: boolean;
  config_revision: number;
  applied_revision: number;
  // center_version：中心的版本，Gate 与它不同时会自更新；self_update 是 Gate 随心跳上报的自更新进度或失败原因（成功或同版本时为空）
  center_version?: string;
  self_update?: GateSelfUpdate | null;
  // Gate 最近一次上报的主机观测；字段缺失表示没读到（未知）。
  host?: GateHost | null;
}

export type GateHost = Record<string, number | string | undefined>;

// GateSelfUpdate：state 为 downloading 时 done / total / source 是下载进度与当前来源；replacing 是正在替换程序；
// failed（或只有 error）是失败原因，失败后 10 分钟内 Gate 不自动重试，手动更新不受限。
export interface GateSelfUpdate {
  version: string;
  state?: "downloading" | "replacing" | "failed";
  done?: number;
  total?: number;
  speed?: number;
  source?: string;
  error?: string;
  at: string;
}

// ExitProbe 是入口 VPS 实测出口的结果：proxy 为按协议真连并拿到实际出口 IP，tcp 为只测了端口。
export interface ExitProbe {
  ok: boolean;
  mode: "proxy" | "tcp";
  latency_ms?: number;
  exit_ip?: string;
  note?: string;
  at: string;
}

// PortNeed 是节点要放行的端口与本机防火墙的对照；Reach 是中心从外部连接的结果。
export interface PortNeed {
  port: number;
  proto: "tcp" | "udp";
  firewall: "open" | "blocked" | "unknown";
  why?: string;
}
export interface Reach {
  status: "ok" | "failed" | "unknown";
  why?: string;
  latency_ms?: number;
  at?: string;
}

// PresetField 是方案「高级」里可以改的一个字段（中文名、类型、单位、可选值）。
export interface PresetField {
  key: string;
  label: string;
  kind: "text" | "number" | "select" | "bool";
  unit?: string;
  hint?: string;
  options?: [string, string][];
}

export interface Preset {
  // 带 gate_id 读取时：这台 VPS 现在能不能用及原因
  available?: boolean;
  why?: string;
  id: string;
  name: string;
  summary: string;
  kernel: string;
  protocol: string;
  cert: "none" | "required" | "self_signed_ok";
  chain_exit: boolean;
  fields: PresetField[] | null;
  note?: string;
}

export type ExitKind = "none" | "socks5" | "http" | "https" | "link" | "node" | "source";

export interface ChainSpec {
  kind: ExitKind;
  address?: string;
  port?: number;
  username?: string;
  password?: string;
  // link：一条节点链接（不接受订阅地址）
  link?: string;
  node_id?: number;
  // source：管理员用 source_id（订阅源），用户自建用 profile_id（分给他的订阅组）；mode node / filter
  source_id?: number;
  profile_id?: number;
  mode?: "node" | "filter";
  match?: string;
}

// SourceResolved 是订阅源出口当前用的节点（一组时为全部成员）；resolved 为空表示还没解析成功。
export interface SourceResolved {
  resolved: string[] | null;
  group?: boolean;
  // 匹配到但不能作出口的节点及原因
  skipped?: string[] | null;
  at?: string;
}

// Withheld 是因额度没有发给用户的节点（WireGuard 无法按用户计量）。
export interface Withheld {
  node_id: number;
  name: string;
  reason: string;
}

export interface NodeItem {
  id: number;
  gate_id: number;
  gate_name?: string;
  host?: string;
  name: string;
  preset: string;
  kernel: string;
  protocol: string;
  port: number;
  network: string;
  security: string;
  enabled: boolean;
  credentials: number;
  // 整个节点实际走的 VPS 出口（只对这个节点、不限域名的分流规则）
  route_exit?: { tag: string; name: string; protocol: string; address: string; port: number };
  readiness?: { state: "ready" | "credentials" | "pending" | "kernel" | "listener" | "unknown"; why: string };
  cert_id?: number | null;
  settings?: Record<string, unknown>;
  chain: { kind: ExitKind; address?: string; port?: number; username?: string; password?: string; link?: string; node_id?: number | null; source?: SourceResolved; probe?: ExitProbe };
  ports?: PortNeed[];
  reach?: Reach;
  // 全部用户直连这个节点时的下载速率
  rate?: RateStat;
}

export interface Cert {
  id: number;
  name: string;
  domain: string;
  source: string;
  gate_id?: number | null;
  not_after?: string;
  fingerprint: string;
  // ready / renewing / pending / failed / expired / unknown
  health?: string;
  last_error?: string;
  renewed_at?: string;
}

// 带时间戳的待生效结果仍需刷新，直到凭据状态明确或实际监听与探测均已就绪。
export function nodeReachSettled(n?: NodeItem): boolean {
  return n?.readiness?.state === "credentials" || (!!n?.reach?.at && !n.reach.at.startsWith("0001") && (!n.readiness || n.readiness.state === "ready"));
}

export interface Entry {
  name: string;
  node_id: number;
  chain_id?: number;
  protocol: string;
  host: string;
  port: number;
  uuid?: string;
  password?: string;
  uri: string;
  error?: string;
}

export interface LinkItem {
  id: number;
  name: string;
  token: string;
  url: string;
  all_nodes: boolean;
  node_ids?: number[] | null;
  chain_ids?: number[] | null;
  enabled: boolean;
  used: number;
  nodes: number;
  last_used_at?: string;
  // 生效时间（都可空）；effective_ends_at 是与用户到期取早者后的实际结束时间
  starts_at?: string | null;
  ends_at?: string | null;
  effective_ends_at?: string | null;
  state: LinkState;
  entries?: Entry[];
}

export type LinkState = "active" | "not_started" | "expired";

// LinkStats 是「订阅链接」里一条链接的拉取统计（最近 30 天）。
export interface LinkStats {
  id: number;
  name: string;
  user_id: number;
  user: string;
  enabled: boolean;
  state: LinkState;
  starts_at?: string | null;
  ends_at?: string | null;
  used: number;
  pulls_24h: number;
  pulls_30d: number;
  refused_30d: number;
  sources_30d: number;
  clients: { name: string; count: number }[];
  last_used_at?: string | null;
  last_pull?: Pull;
}

export interface Pull {
  at: string;
  target: string;
  client: string;
  user_agent: string;
  ip: string;
  status: "ok" | "not_started" | "expired" | "failed";
  place?: string;
  org?: string;
}

// ---- 代理页 ----
export type Health = "ok" | "down" | "unknown";

// RateStat 是节点自己统计的下载速率：最近 24 小时有流量的分钟的第 90 百分位（字节/秒）。minutes 为 0 是没有数据（未知）；
// from：self 是这个用户自己的，all 是全部用户的（他自己没有数据时补上）。
export interface RateStat {
  p90_bps: number;
  minutes: number;
  from: "self" | "all" | "";
}

export interface ProxyNode {
  id: number;
  name: string;
  gate_id?: number;
  gate_name?: string;
  health: Health;
  why?: string;
  rate: RateStat;
}

export interface ProxyType {
  key: string;
  label: string;
  enabled: boolean;
  online: number;
  nodes: ProxyNode[];
  exit?: { kind: Exclude<ExitKind, "none">; address?: string; port?: number; username?: string; password?: string; link?: string; node_id?: number | null; source?: { source_id?: number; profile_id?: number; mode?: "node" | "filter"; match?: string } | null };
  missing_exit?: string[] | null;
}

export interface ProxyPolicy {
  disabled?: string[];
  rules?: Record<string, string>;
  auto: { mode: "latency" | "ranked"; top: number; refresh_minutes: number };
}

export type CustomMode = "off" | "bypass" | "only";
export type CustomRuleKind = "domain" | "domain_suffix" | "domain_keyword" | "ip_cidr";

export interface CustomResult {
  rules: { kind: CustomRuleKind; value: string }[];
  counts: Partial<Record<CustomRuleKind, number>>;
  total: number;
  reduced: number;
  unused: { line: number; text: string; reason: string }[];
  warnings: string[];
}

export interface CustomView {
  mode: CustomMode;
  active_mode: CustomMode;
  active_count: number;
  source: string;
  check?: { status: "passed" | "failed" | "unknown"; detail?: string; at: string } | null;
  result: CustomResult;
  refresh_seconds: number;
}

export interface CustomMatch {
  label: string;
  kind: string;
  value?: string;
  policy: string;
  maybe?: { label: string; kind: string; policy: string }[];
}

export interface ProxyView {
  policy: ProxyPolicy;
  custom: CustomView;
  types: ProxyType[];
  categories: { key: string; label: string; hint?: string; default: string; target: string }[];
  ranking: { node_id: number; name: string; type: string; health: Health; why?: string; rate: RateStat; rank: number; auto: boolean; exploring: boolean }[];
  allow_chain: boolean;
  min_refresh: number;
}

export interface ChainItem {
  id: number;
  name: string;
  entry_node_id: number;
  entry_name?: string;
  exit_kind: Exclude<ExitKind, "none">;
  exit_address?: string;
  exit_port?: number;
  exit_username?: string;
  exit_password?: string;
  exit_node_id?: number | null;
  exit_link?: string;
  probe?: ExitProbe;
  exit_source?: { source_id?: number; profile_id?: number; mode?: "node" | "filter"; match?: string } | null;
  source?: SourceResolved;
  enabled: boolean;
}

export interface UserItem {
  id: number;
  email: string;
  name: string;
  role: "admin" | "user";
  enabled: boolean;
  quota: number;
  used: number;
  expires_at?: string | null;
  monthly_reset: boolean;
  all_nodes: boolean;
  node_ids?: number[] | null;
  allow_chain: boolean;
  links: number;
  chains: number;
  password?: string;
  link_items?: LinkItem[];
  chain_items?: ChainItem[];
}

export const units = { MiB: 1 << 20, GiB: 1 << 30, TiB: 2 ** 40 } as const;
export type Unit = keyof typeof units;

// toUnits 把字节换成最合适的单位，供编辑表单回填。
export function toUnits(n: number): [string, Unit] {
  if (n > 0 && n % units.TiB === 0) return [String(n / units.TiB), "TiB"];
  if (n > 0 && n % units.GiB === 0) return [String(n / units.GiB), "GiB"];
  if (n > 0) return [String(+(n / units.MiB).toFixed(2)), "MiB"];
  return ["0", "GiB"];
}

export function fromUnits(value: string, unit: Unit) {
  const v = Number(value);
  return Number.isFinite(v) && v > 0 ? Math.round(v * units[unit]) : 0;
}

// localInput / fromLocalInput 在 datetime-local 与 ISO 时间之间换算（按浏览器时区）。
export function localInput(iso?: string | null) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(+d)) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromLocalInput(v: string) {
  return v ? new Date(v).toISOString() : null;
}

export const protocolNames: Record<string, string> = {
  vless: "VLESS",
  vmess: "VMess",
  trojan: "Trojan",
  shadowsocks: "Shadowsocks 2022",
  hysteria2: "Hysteria2",
  tuic: "TUIC",
};

export function nodeStatus(n: NodeItem, g?: Gate): [string, LampTone, string] {
  if (!n.enabled) return ["已停用", "off", "节点已停用，不在订阅里"];
  if (!g || g.status === "revoked") return ["不可用", "bad", "VPS 已撤销"];
  if (!g.online) return ["离线", "bad", "VPS 离线"];
  if (g.state === "failed") return ["不可用", "bad", "VPS 配置应用失败：" + g.last_error];
  if (g.state === "pending") return ["待生效", "warn", "配置正在下发到 VPS"];
  if (n.readiness && n.readiness.state !== "ready") {
    const label = ({ credentials: "待分配", pending: "待生效", kernel: "内核未运行", listener: "未监听", unknown: "未知" } as Record<string, string>)[n.readiness.state] ?? "未知";
    return [label, n.readiness.state === "credentials" || n.readiness.state === "pending" ? "warn" : "unknown", n.readiness.why];
  }
  if ((n.ports ?? []).some((p) => p.firewall === "blocked")) return ["未放行", "bad", (n.ports ?? []).find((p) => p.firewall === "blocked")?.why ?? "本机防火墙没有放行端口"];
  if (n.reach?.status === "failed") return ["连不上", "bad", (n.reach.why ?? "中心从外部连不上") + "。这是中心的连接结果，节点是否真的不可用以客户端实测为准"];
  if (n.reach?.status === "ok") return ["端口可达", "ok", n.reach.latency_ms ? `中心 TCP 连接用了 ${n.reach.latency_ms} ms；协议连接以客户端实测为准` : "中心 TCP 端口可达；协议连接以客户端实测为准"];
  return ["未知", "unknown", n.reach?.why ?? "还没有外部连接结果"];
}

// relativeTime：「30 秒前」「5 分钟前」；没有时间时为「未知时间」，不当成刚刚。
export function relativeTime(value?: string | null) {
  const t = value ? new Date(value).getTime() : NaN;
  if (!Number.isFinite(t) || t <= 0) return "未知时间";
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return `${s} 秒前`;
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  return `${Math.floor(s / 86400)} 天前`;
}
