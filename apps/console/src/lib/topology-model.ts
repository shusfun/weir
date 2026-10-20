// Public, minimal adapter for the retained MiGate graph builder. No credentials.
export type Client = {
  id: number;
  email?: string;
  enabled: boolean;
  traffic_limit?: number;
};
export type Inbound = {
  id: number;
  remark?: string;
  protocol: string;
  port?: number;
  core?: string;
  network?: string;
  security?: string;
  enabled: boolean;
  clients?: Client[];
};
export type Outbound = {
  id: number;
  tag: string;
  protocol: string;
  address?: string;
  port?: number;
  remark?: string;
  enabled: boolean;
};
export type RoutingRule = {
  id: number;
  inbound_id?: number;
  inbound_tag?: string;
  client_id?: number;
  client_email?: string;
  outbound_id?: number;
  outbound_tag?: string;
  enabled: boolean;
};
export type TrafficV2Snapshot = {
  clients: { id: number; cumulative: { up: number; down: number } }[];
};
export function inboundCore(i: Inbound): string {
  return (
    i.core || (["hysteria2", "tuic"].includes(i.protocol) ? "sing-box" : "xray")
  );
}
export function coreLabel(core: string): string {
  return core === "xray" ? "Xray" : core === "sing-box" ? "sing-box" : core;
}
export function outboundSupportedCores(o: Outbound): string[] {
  return ["hysteria2", "tuic", "shadowtls"].includes(o.protocol)
    ? ["sing-box"]
    : ["xray", "sing-box"];
}
export function outboundSupportsCore(o: Outbound, core: string): boolean {
  return outboundSupportedCores(o).includes(core);
}
export function generatedInboundTag(i: Inbound): string {
  return `inbound-${i.id}-${i.protocol.trim().toLowerCase()}`;
}
