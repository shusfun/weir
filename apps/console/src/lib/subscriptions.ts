// 订阅源页的接口类型：订阅源、手动节点、订阅组、规则模板与全局设置。
import type { Operator } from "../components/operator-editor";
import type { ObjectState } from "./core";

export interface UserInfo {
  upload?: number;
  download?: number;
  total?: number;
  expire?: number;
}

export interface Source {
	object_state?: ObjectState;
  id: number;
  name: string;
  url: string;
  enabled: boolean;
  user_agent: string;
  filter_include: string;
  filter_exclude: string;
  operators: Operator[] | null;
  plus_as_space: boolean;
  node_cache: boolean;
  exclude_traffic: boolean;
  website: string;
  remark: string;
  node_count: number;
  user_info: UserInfo | null;
  title: string;
  last_error: string;
  fetched_at?: string | null;
  // 拉取用哪台 VPS（0 为自动）与自动刷新间隔（0 跟随全局）；effective 是实际生效的间隔，0 表示不自动刷新
  gate_id: number;
  gate: { id: number; name: string } | null;
  refresh_hours: number;
  effective_refresh_hours: number;
  next_refresh_at: string | null;
  last_attempt_at: string | null;
  last_location: string;
  last_location_label: string;
  last_route_reason: string;
  health: SourceHealth | null;
  change: { added: number; removed: number; renamed: number; samples?: string[] } | null;
  // 保存后后台正在拉取
  refreshing?: boolean;
}

// 订阅源检测摘要：unchecked 是本轮没测的（中心直连、负载让路、超出上限），不算不可用
export interface SourceHealth {
  total: number; alive: number; failed: number; unchecked: number;
  regions: Record<string, number>; region_alive: Record<string, number>; multipliers: Record<string, number>;
  checked_at?: string; location: string; reason?: string;
}

export interface InspectedNode { identity: string; name: string; region: string; multiplier?: number; status: "alive" | "failed" | "unchecked"; latency_ms?: number; why?: string }

export interface SourceInspection {
  ok: boolean; error?: string; inspection_id?: string; location: string; location_label?: string; route_reason?: string;
  user_info?: UserInfo | null; title?: string; summary?: SourceHealth; nodes?: InspectedNode[]; gate_id?: number;
}

export interface ManualNode {
  id: number;
  name: string;
  url: string;
  group: string;
  enabled: boolean;
  filter_include: string;
  filter_exclude: string;
  operators: Operator[] | null;
  plus_as_space: boolean;
}

export interface RegionOverride {
  pattern: string;
  region: string;
  flags?: string;
}

export interface RenderingSettings {
  enable_udp?: boolean;
  enable_tfo?: boolean;
  skip_cert_verify?: boolean;
  dns_mode?: string;
  custom_dns?: string;
  region_overrides?: RegionOverride[] | null;
}

export interface ProfileSettings extends RenderingSettings {
	engine?: string;
  template?: string;
  rule_level?: string;
  flag_emoji?: boolean;
  prepend_manual?: boolean;
  manual_prefix?: string;
  prepend_sources?: boolean;
  filter_include?: string;
  filter_exclude?: string;
  traffic_nodes?: boolean;
  pick?: ProfilePick;
}

// 订阅组的挑选条件：不保存固定节点清单，每次按最新节点重新挑选。各项为空表示不限制。
export interface ProfilePick {
  regions?: string[];
  max_multiplier?: number | null;
  include_unlabeled_multiplier?: boolean;
  keywords?: string[];
  only_alive?: boolean;
}

export type NodeHealthStatus = "alive" | "failed" | "unchecked";

// preview-nodes 的返回：只用各源已有缓存组合，不触发拉取。
export interface ProfileNodePreview {
  nodes: { name: string; region: string; multiplier: number | null; kind: "source" | "node" | "profile"; source_id?: number; source_name?: string; status: NodeHealthStatus; latency_ms: number | null }[];
  merged: number;
  steps: { step: string; before: number; removed: number }[];
  available: { regions: { region: string; count: number }[]; multipliers: number[]; unlabeled_multiplier: number };
  sources: { source_id: number; name: string; nodes: number; from_cache: boolean; error?: string }[];
}

export interface Profile {
  id: number;
  name: string;
  enabled: boolean;
  all_sources: boolean;
  source_ids: number[] | null;
  node_ids: number[] | null;
  members: { kind: "source" | "node" | "profile"; id: number }[] | null;
  operators: Operator[] | null;
  settings: ProfileSettings | null;
  expires_at?: string | null;
  downloads: number;
  user_ids: number[];
  shared: boolean;
  share_expires_at?: string | null;
}

export interface BuiltinTemplate {
  id: string;
  name: string;
  description: string;
  format: string;
}

export interface CustomTemplate {
  id: number;
  name: string;
  format: "ini" | "yaml";
  content: string;
}

export interface SubscriptionSettings {
	engine?: string;
  flag_emoji: boolean;
  prepend_manual: boolean;
  manual_prefix: string;
  prepend_sources: boolean;
  prepend_group_name: boolean;
  default_operators: Operator[] | null;
  rule_level: string;
  template: string;
  traffic_nodes: boolean;
  cache_minutes: number;
  skip_cert_verify: boolean;
  enable_udp: boolean;
  enable_tfo: boolean;
  dns_mode: string;
  custom_dns: string;
  region_overrides: RegionOverride[] | null;
  disguise: boolean;
  filter_include: string;
  filter_exclude: string;
  update_interval: number;
  source_refresh_hours: number;
  // 订阅源没有单独指定时首选的拉取 VPS；0 表示自动
  default_gate_id: number;
}

export const ruleLevels = [["none", "不加规则"], ["lite", "精简"], ["std", "标准"], ["full", "完整"]] as const;
