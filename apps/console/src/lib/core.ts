// 新中心接口的类型与通用换算（Gate、节点、用户、链接、链式代理、订阅）。
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
  panel?: { detected?: boolean; configured?: boolean; version?: string; error?: string } | null;
  // 本机防火墙（ufw / firewalld / iptables / none）；缺失表示这次没读到（未知）。
  firewall?: { tool: string; active: boolean; rules?: string[]; error?: string } | null;
  // 单机安装时建议的公网地址（本机网卡上的公网 IP）；colocated：中心与这台 VPS 在同一台机器上。
  public_host_hint?: string;
  colocated?: boolean;
  config_revision: number;
  applied_revision: number;
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
  id: string;
  name: string;
  summary: string;
  kernel: string;
  protocol: string;
  cert: "none" | "required" | "self_signed_ok";
  chain_exit: boolean;
  fields: PresetField[];
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

// LinkStats 是「订阅管理」里一条链接的拉取统计（最近 30 天）。
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

export interface ProxyView {
  policy: ProxyPolicy;
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
