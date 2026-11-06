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
export function exitTitle(e: ExitItem) { return e.remark || e.label || e.tag; }
export function exitInput(e: ExitItem): ExitSpec {
  return { kind:e.kind, remark:e.remark, address:e.address, port:e.port, username:e.username, password:e.password, link:e.link, node_id:e.node_id, ...e.source };
}
