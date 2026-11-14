// VPS 体检：读不到的项显示未知，不把缺失写成正常。正常项右边写「正常」，说明里写读数。
import { HealthList } from "@weir/ui";
import type { GateHealth } from "../lib/core";

type Item = { id: string; name: string; status: "ok" | "warn" | "bad" | "info" | "unknown"; label: string; detail?: string };

function item(id: string, name: string, present: boolean, status: Item["status"], label: string, detail?: string): Item {
  return present ? { id, name, status, label, detail } : { id, name, status: "unknown", label: "未知", detail: "这次没有读到" };
}

export function healthItems(health?: GateHealth | null): Item[] {
  const h = health ?? {};
  const pct = (v?: number) => v == null ? "" : `${Math.round(v * 100)}%`;
  return [
    h.dns === "answers_differ" ? { id: "dns", name: "系统 DNS", status: "unknown", label: "未知", detail: "不同解析器返回不同公网地址，可能是 CDN 调度；仅凭地址差异不能判定污染。" } : item("dns", "系统 DNS", h.dns === "clean" || h.dns === "polluted", h.dns === "polluted" ? "bad" : "ok", h.dns === "polluted" ? "被污染" : "正常", h.dns === "polluted" ? "查常用站点拿到假地址，经这台直连的网站可能打不开" : "解析结果与公共 DNS 一致"),
    item("congestion", "拥塞控制", typeof h.congestion === "string" && h.congestion !== "", "ok", "正常", h.congestion),
    item("steal", "宿主机 CPU 抢占", typeof h.cpu_steal === "number", h.cpu_steal != null && h.cpu_steal >= 10 ? "warn" : "ok", h.cpu_steal != null && h.cpu_steal >= 10 ? "被抢占" : "正常", h.cpu_steal == null ? "" : `steal ${h.cpu_steal.toFixed(1)}%`),
    item("cert", "证书", typeof h.cert_days === "number", h.cert_days != null && h.cert_days <= 3 ? "bad" : h.cert_days != null && h.cert_days <= 14 ? "warn" : "ok", h.cert_days != null && h.cert_days < 0 ? "已过期" : h.cert_days != null && h.cert_days <= 3 ? "即将过期" : "正常", h.cert_days == null ? "" : h.cert_days < 0 ? `已过期 ${-h.cert_days} 天` : `${h.cert_days} 天后到期`),
    item("udp", "UDP 缓冲区", typeof h.udp_buffer === "number", "ok", "正常", h.udp_buffer == null ? "" : `接收缓冲 ${h.udp_buffer}`),
    item("mtu", "MTU", typeof h.mtu === "number", h.mtu != null && h.mtu < 1280 ? "warn" : "ok", h.mtu != null && h.mtu < 1280 ? "偏小" : "正常", h.mtu == null ? "" : String(h.mtu)),
    item("conntrack", "连接跟踪余量", typeof h.conntrack_free === "number", h.conntrack_free != null && h.conntrack_free < 0.05 ? "warn" : "ok", h.conntrack_free != null && h.conntrack_free < 0.05 ? "快用完" : "正常", pct(h.conntrack_free)),
    item("files", "文件句柄余量", typeof h.file_handles_free === "number", h.file_handles_free != null && h.file_handles_free < 0.05 ? "warn" : "ok", h.file_handles_free != null && h.file_handles_free < 0.05 ? "快用完" : "正常", pct(h.file_handles_free)),
    item("ipv6", "IPv6", typeof h.ipv6 === "boolean", h.ipv6 ? "ok" : "unknown", h.ipv6 ? "正常" : "不可用", h.ipv6 ? "有公网地址" : "没有公网地址"),
  ];
}

export function GateHealthList({ health }: { health?: GateHealth | null }) {
  return <HealthList items={healthItems(health)} />;
}

export function healthSummary(health?: GateHealth | null): string {
  const items = healthItems(health);
  const bad = items.filter((i) => i.status === "bad" || i.status === "warn");
  const unknown = items.filter((i) => i.status === "unknown" && i.label === "未知");
  if (!health || unknown.length === items.length) return "体检未知";
  if (bad.length === 0 && unknown.length > 0) return "体检：" + unknown.map((i) => `${i.name}未知`).join("，");
  if (bad.length === 0) return "体检没有需要处理的项";
  const text = "体检：" + bad.map((i) => `${i.name} ${i.label}`).join("，");
  return unknown.length ? text + "；" + unknown.map((i) => `${i.name}未知`).join("，") : text;
}
