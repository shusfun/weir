import { Badge, Notice } from "@weir/ui";
import { bytes } from "../../lib/models";
import { fromUnits, type Unit } from "../../lib/core";
import type { Grant } from "./types";

export const emptyGrant = (): Grant => ({ quota: 0, unlimited: false, days: 0, all_nodes: false, node_ids: [], profile_ids: [] });

export function grantInput(grant: Grant, quota: string, unit: Unit): Grant {
  return { ...grant, quota: grant.unlimited ? 0 : fromUnits(quota || "0", unit), node_ids: grant.all_nodes ? [] : [...new Set(grant.node_ids ?? [])], profile_ids: [...new Set(grant.profile_ids ?? [])] };
}

export function grantParts(g: Grant): string[] {
  const parts: string[] = [];
  if (g.unlimited) parts.push("不限流量");
  else if (g.quota > 0) parts.push(`增加 ${bytes(g.quota)} 流量`);
  if (g.all_nodes) parts.push("全部节点");
  else if (g.node_ids?.length) parts.push(`${g.node_ids.length} 个节点`);
  if (g.profile_ids?.length) parts.push(`${g.profile_ids.length} 个订阅组`);
  if (g.days > 0) parts.push(`有效期增加 ${g.days} 天`);
  return parts;
}

export function GrantSummary({ grant, preview = false }: { grant: Grant; preview?: boolean }) {
  const parts = grantParts(grant);
  if (preview) return <Notice tone={parts.length ? "info" : "warn"}>{parts.length ? `本次发放：${parts.join("；")}。已有授权保留，长期账户保持长期。` : "未配置额外权益；此套餐不会增加流量、授权或有效期。"}</Notice>;
  return <span className="account-grant-summary">{parts.length ? parts.join(" · ") : "不发放额外权益"}</span>;
}

export function GrantSyncStatus({ value }: { value?: string }) {
  const label = value === "ok" ? "同步完成" : value === "pending" ? "已入账 · 同步待完成" : "已入账 · 同步未知";
  return <Badge variant="status" tone={value === "pending" ? "warn" : "neutral"}>{label}</Badge>;
}
