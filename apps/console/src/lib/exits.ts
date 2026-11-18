import { useQuery } from "./query";
import { api } from "../api";
import type { ExitItem, ExitList, ExitSpec } from "./core";

export const exitKindName: Record<string, string> = {};
export function useExitCatalog(admin: boolean, gateId?: number) {
  return useQuery({ queryKey: ["exits", admin, gateId], queryFn: async () => {
    const data = await api((admin ? "/admin/exits" : "/me/exits") + (gateId ? `?gate_id=${gateId}` : "")) as ExitList;
    for (const kind of data.kinds) exitKindName[kind.kind] = kind.label;
    return data;
  }});
}
// gate_id 查询的是已使用出口；选用时从完整目录按归属和 VPS 专属范围筛选。
export function exitChoices(items: ExitItem[], gateId: number, ownerUserId?: number | null) {
  return items.filter(e => e.enabled
    && (e.gate_id == null || e.gate_id === gateId)
    && (ownerUserId === undefined || e.owner_user_id === ownerUserId));
}
export function exitTitle(e: ExitItem) { return e.summary?.name || e.remark || "地区未知"; }
export function exitInput(e: ExitItem): ExitSpec {
  return { kind:e.kind, remark:e.remark, address:e.address, port:e.port, username:e.username, password:e.password, link:e.link, node_id:e.node_id, identity:e.identity, insecure_identities:e.insecure_identities, ...e.source };
}
