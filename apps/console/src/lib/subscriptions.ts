// 订阅源页的接口类型：订阅源、手动节点、订阅组、规则模板与全局设置。
import type { Operator } from "../components/operator-editor";

export interface UserInfo {
  upload?: number;
  download?: number;
  total?: number;
  expire?: number;
}

export interface Source {
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
}

export const ruleLevels = [["none", "不加规则"], ["lite", "精简"], ["std", "标准"], ["full", "完整"]] as const;
