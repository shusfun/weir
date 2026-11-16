// 原型的模拟数据：只给 /dev/proto 用，不接接口。名称、地址都是示意，不含真实凭据。
// 状态结论按决策 0016 的形状给出：一个结论、一句中文原因、一个下一步动作。
// 分工（决策 0014）：节点归节点（用户的链式就是他多出来的节点）、代理归代理、规则归规则、订阅只管自己的。

export type Tone = "ok" | "warn" | "bad" | "unknown";
export type Verdict = { tone: Tone; text: string; why?: string; fix?: string };
export type UDP = "full" | "tcp" | "unknown";

// 节点统计来自 Gate 每 30 秒上报的凭据计数（用户 × 链接 × 节点 × 链式）；速率取 24 小时 P90，没有样本就是未知
export type NodeStats = { traffic24h: number | null; p90: number | null; activeUsers: number; top: { user: string; gb: number }[] };

export type ProtoNode = {
  id: number;
  name: string;
  protocol: string;
  port?: number;
  network: "tcp" | "udp";
  kernel?: "Xray" | "sing-box";
  region: string;
  source: { kind: "gate"; gateId: number } | { kind: "sub"; name: string } | { kind: "chain"; owner: string; entry: number; proxy: string };
  landing: string | null; // 落地代理名；null = 直接出去
  udp: UDP;
  users: number | "all";
  verdict: Verdict;
  latency?: number | null;
  stats: NodeStats;
};

export type ProtoGate = {
  id: number;
  name: string;
  host: string;
  cpu: number | null;
  steal: number | null;
  firewall: string;
  browserMs: number | null;
  browserUdp: string;
  health: Verdict;
  beat: string;
  // 供应商给的月流量：额度、计费方式、重置日；用量由 Gate 读网卡计数累计，可手动校准
  quota: { totalGB: number | null; usedGB: number | null; mode: string; resetDay: number | null; daysLeft: number | null };
};

export const gates: ProtoGate[] = [
  { id: 1, name: "方微云", host: "38.246.245.38", cpu: 2, steal: 15, firewall: "ufw · Gate 管理", browserMs: 43, browserUdp: "UDP 可达", health: { tone: "warn", text: "2 项要处理", why: "证书 2 天后到期 · 宿主机抢 CPU 15%" }, beat: "34 秒前", quota: { totalGB: 1000, usedGB: 812, mode: "只算出站", resetDay: 26, daysLeft: 12 } },
  { id: 2, name: "方微云-154", host: "154.40.58.28", cpu: 6, steal: 1, firewall: "未启用", browserMs: 61, browserUdp: "QUIC 限速", health: { tone: "ok", text: "9 项通过" }, beat: "12 秒前", quota: { totalGB: null, usedGB: 143, mode: "未设置", resetDay: null, daysLeft: null } },
];

const s = (traffic24h: number | null, p90: number | null, activeUsers: number, top: [string, number][] = []): NodeStats => ({ traffic24h, p90, activeUsers, top: top.map(([user, gb]) => ({ user, gb })) });

export const nodes: ProtoNode[] = [
  { id: 1, name: "方微云 VLESS + REALITY", protocol: "VLESS + REALITY", port: 43512, network: "tcp", kernel: "Xray", region: "美国", source: { kind: "gate", gateId: 1 }, landing: null, udp: "full", users: "all", verdict: { tone: "ok", text: "可用" }, latency: 42, stats: s(18.4, 12.6, 4, [["shus808", 11.2], ["Tulip", 5.1], ["阿木", 2.1]]) },
  { id: 2, name: "方微云 Trojan + TLS", protocol: "Trojan + TLS", port: 26692, network: "tcp", kernel: "Xray", region: "美国", source: { kind: "gate", gateId: 1 }, landing: null, udp: "full", users: "all", verdict: { tone: "ok", text: "可用" }, latency: 45, stats: s(2.3, 9.8, 2, [["阿木", 1.9], ["shus808", 0.4]]) },
  { id: 3, name: "方微云 VLESS + REALITY", protocol: "VLESS + REALITY", port: 20596, network: "tcp", kernel: "Xray", region: "美国", source: { kind: "gate", gateId: 1 }, landing: "美东", udp: "tcp", users: 2, verdict: { tone: "warn", text: "可用 · 慢", why: "经代理「美东」P90 只有 0.8 MB/s；同台直连是 12.6 MB/s", fix: "换落地" }, latency: 8627, stats: s(0.6, 0.8, 1, [["Tulip", 0.6]]) },
  { id: 8, name: "方微云 Hysteria2", protocol: "Hysteria2", port: 35138, network: "udp", kernel: "sing-box", region: "美国", source: { kind: "gate", gateId: 1 }, landing: "小代", udp: "tcp", users: "all", verdict: { tone: "bad", text: "UDP 35138 未放行", why: "ufw 已启用，只放行了 35138/tcp；Hysteria2 走 UDP，客户端连不进来。", fix: "由 Gate 放行" }, latency: null, stats: s(0, null, 0) },
  { id: 4, name: "方微云-154 VLESS + REALITY", protocol: "VLESS + REALITY", port: 46955, network: "tcp", kernel: "Xray", region: "香港", source: { kind: "gate", gateId: 2 }, landing: "美西", udp: "unknown", users: 3, verdict: { tone: "ok", text: "可用" }, latency: 61, stats: s(4.1, 7.2, 2, [["阿木", 3.0], ["小林", 1.1]]) },
  { id: 5, name: "方微云-154 Hysteria2", protocol: "Hysteria2", port: 34840, network: "udp", kernel: "sing-box", region: "香港", source: { kind: "gate", gateId: 2 }, landing: "美西", udp: "unknown", users: 3, verdict: { tone: "unknown", text: "还没实测", fix: "实测" }, latency: null, stats: s(null, null, 0) },
  { id: 6, name: "方微云-154 VLESS + REALITY", protocol: "VLESS + REALITY", port: 52766, network: "tcp", kernel: "Xray", region: "香港", source: { kind: "gate", gateId: 2 }, landing: "美东", udp: "tcp", users: 3, verdict: { tone: "ok", text: "可用" }, latency: 2326, stats: s(0.2, 2.1, 1, [["小林", 0.2]]) },
  { id: 20, name: "香港 01", protocol: "VLESS + WS", network: "tcp", region: "香港", source: { kind: "sub", name: "示例机场" }, landing: null, udp: "full", users: 2, verdict: { tone: "ok", text: "可用" }, latency: 88, stats: s(1.4, 6.4, 2, [["Tulip", 1.0], ["shus808", 0.4]]) },
  { id: 21, name: "日本 02", protocol: "Hysteria2", network: "udp", region: "日本", source: { kind: "sub", name: "示例机场" }, landing: null, udp: "full", users: 0, verdict: { tone: "warn", text: "跳过证书校验", why: "这个节点要求跳过证书校验，作落地前要确认（决策 0018）。" }, latency: 120, stats: s(0, null, 0) },
  // 用户自己的链式：入口是分到的节点，从他自己的代理出去；对他来说就是多出来的节点
  { id: 101, name: "方微云 VLESS → 家宽", protocol: "VLESS + REALITY", port: 43512, network: "tcp", kernel: "Xray", region: "上海", source: { kind: "chain", owner: "Tulip", entry: 1, proxy: "家宽" }, landing: "家宽", udp: "full", users: 1, verdict: { tone: "ok", text: "可用" }, latency: 31, stats: s(2.2, 4.6, 1, [["Tulip", 2.2]]) },
  { id: 102, name: "香港 01 → 公司代理", protocol: "VLESS + WS", network: "tcp", region: "深圳", source: { kind: "chain", owner: "Tulip", entry: 20, proxy: "公司代理" }, landing: "公司代理", udp: "tcp", users: 1, verdict: { tone: "ok", text: "可用" }, latency: 64, stats: s(0.3, 3.0, 1, [["Tulip", 0.3]]) },
  { id: 103, name: "方微云 VLESS → 朋友的节点", protocol: "VLESS + REALITY", port: 20596, network: "tcp", kernel: "Xray", region: "未知", source: { kind: "chain", owner: "阿木", entry: 3, proxy: "朋友的节点" }, landing: "朋友的节点", udp: "full", users: 1, verdict: { tone: "bad", text: "代理连不上", why: "VPS 连代理握手超时；代理可能已失效。", fix: "重新实测" }, latency: null, stats: s(0, null, 0) },
];

// 代理：系统的（管理员给节点和规则用）与各用户自己的（只给他的链式节点用）都在一处；同一地址合并成一行，写明挂在哪几台 VPS
export type Proxy = { id: string; owner: string | null; name: string; kind: string; address: string; username: string; secret: string; cipher: string; udp: UDP; probe: string; probeTone: Tone; gates: string[]; usedBy: string[]; dupOf?: string };

export const proxies: Proxy[] = [
  { id: "xd", owner: null, name: "小代", kind: "HTTP", address: "23.188.208.11:58245", username: "xd", secret: "xd-8f2c-pass", cipher: "enc:v1:3b1a…7e20", udp: "tcp", probe: "198.3.18.142 · 1.4 s", probeTone: "ok", gates: ["方微云"], usedBy: ["方微云 Hysteria2"] },
  { id: "md", owner: null, name: "美东", kind: "HTTP", address: "23.188.208.11:58245", username: "xd", secret: "xd-8f2c-pass", cipher: "enc:v1:3b1a…7e20", udp: "tcp", probe: "198.3.18.142 · 8.6 s", probeTone: "warn", gates: ["方微云", "方微云-154"], usedBy: ["方微云 VLESS :20596", "方微云-154 VLESS :52766"], dupOf: "小代" },
  { id: "mx", owner: null, name: "美西", kind: "节点链接", address: "vless · 192.0.2.80:443", username: "", secret: "vless://8a2d…@192.0.2.80:443?security=reality#us-west", cipher: "enc:v1:aa90…11fe", udp: "full", probe: "还没实测", probeTone: "unknown", gates: ["方微云-154"], usedBy: ["方微云-154 上的其余节点"] },
  { id: "jk0", owner: null, name: "家宽 SOCKS5", kind: "SOCKS5", address: "203.0.113.9:1080", username: "home", secret: "home-socks-2026", cipher: "enc:v1:5c77…0d3a", udp: "full", probe: "203.0.113.9 · 31 ms", probeTone: "ok", gates: ["方微云"], usedBy: ["规则：流媒体"] },
  { id: "jk", owner: "Tulip", name: "家宽", kind: "SOCKS5", address: "203.0.113.7:1080", username: "tulip", secret: "p@ss-家宽-2026", cipher: "enc:v1:9f3a…c41e", udp: "full", probe: "203.0.113.7 · 31 ms", probeTone: "ok", gates: ["方微云"], usedBy: ["方微云 VLESS → 家宽"] },
  { id: "gs", owner: "Tulip", name: "公司代理", kind: "HTTP", address: "198.51.100.20:8080", username: "corp", secret: "corp-proxy-key", cipher: "enc:v1:77b0…a2d9", udp: "tcp", probe: "198.51.100.20 · 64 ms", probeTone: "ok", gates: ["示例机场 · 香港 01"], usedBy: ["香港 01 → 公司代理"] },
  { id: "py", owner: "阿木", name: "朋友的节点", kind: "节点链接", address: "vless · 198.51.100.44:443", username: "", secret: "vless://6f1c…@198.51.100.44:443?security=reality#friend", cipher: "enc:v1:1c5e…09bb", udp: "full", probe: "握手超时", probeTone: "bad", gates: ["方微云"], usedBy: ["方微云 VLESS → 朋友的节点"] },
];

// 规则：从上到下匹配；每条带实际流量与速率，按数据调整
export const rules = [
  { id: "r1", kind: "网站", lane: "流媒体 12 个域名", match: "netflix.com、disneyplus.com 等 12 个域名", to: "家宽 SOCKS5", traffic: "41.2 GB / 7 天", p90: "5.1 MB/s" },
  { id: "r2", kind: "节点", lane: "Hysteria2 :35138", match: "方微云 Hysteria2 :35138", to: "小代", traffic: "0 / 7 天", p90: "未知" },
  { id: "r3", kind: "节点", lane: "VLESS :20596 等 2 个", match: "方微云 VLESS :20596、方微云-154 VLESS :52766", to: "美东", traffic: "5.6 GB / 7 天", p90: "0.8 MB/s" },
  { id: "r4", kind: "VPS", lane: "方微云-154 其余", match: "方微云-154 上的其余节点", to: "美西", traffic: "28.7 GB / 7 天", p90: "7.2 MB/s" },
];

// 疑似误判：目标在国内却经代理出去的流量，按域名聚合（只收国内目标，不记 URL）；确认后写进订阅的直连规则
export const misjudged = [
  { id: "m1", domain: "*.bilivideo.com", gb: 9.6, users: 3, via: "方微云 VLESS :43512", ip: "国内 · 电信" },
  { id: "m2", domain: "*.douyinvod.com", gb: 4.1, users: 2, via: "香港 01", ip: "国内 · 联通" },
  { id: "m3", domain: "dl.steamcontent.cn", gb: 3.8, users: 1, via: "方微云 Trojan :26692", ip: "国内 · 移动" },
  { id: "m4", domain: "*.mi-img.com", gb: 0.4, users: 1, via: "方微云-154 VLESS :46955", ip: "国内 · 阿里云" },
];

export const users = [
  { id: 1, name: "shus808", email: "管理员", used: 20.15, quota: null as number | null, nodes: "全部 10" },
  { id: 2, name: "Tulip", email: "tulip@example.com", used: 2.76, quota: 20, nodes: "3 个 + 2 条链式" },
  { id: 3, name: "阿木", email: "amu@example.com", used: 7.0, quota: 50, nodes: "全部 + 1 条链式" },
  { id: 4, name: "小林", email: "lin@example.com", used: 1.3, quota: 30, nodes: "3 个" },
  { id: 5, name: "测试机", email: "probe@example.com", used: 0, quota: 5, nodes: "1 个" },
];

// 用户的操作记录（来自操作审计，按用户筛出）与节点用量排行
export const userOps = [
  { at: "16:02", what: "拉取订阅「手机」", where: "iPhone · 上海电信" },
  { at: "15:40", what: "新建代理「公司代理」，VPS 实测通过", where: "控制台" },
  { at: "15:38", what: "用代理新建节点「香港 01 → 公司代理」", where: "控制台" },
  { at: "昨天 22:10", what: "订阅「电脑 · 只走家宽」改为只含 1 个节点", where: "控制台" },
];

export const sources = [
  { id: 1, name: "示例机场", url: "https://sub.example.com/••••••", total: 38, picked: 2, left: "120 GB · 2027-01-03", at: "14:00 · 1.2 s", verdict: { tone: "ok", text: "正常" } as Verdict },
  { id: 2, name: "备用机场", url: "https://backup.example.org/••••••", total: 12, picked: 0, left: "未知", at: "14:00 · 0.8 s", verdict: { tone: "warn", text: "上游返回 0 个节点", why: "只有一条「套餐已到期」提示，保留旧节点" } as Verdict },
];

export const sourceNodes = [
  { id: "a1", name: "香港 01", proto: "VLESS · WS", ms: "88 ms", tone: "ok" as Tone, picked: true },
  { id: "a2", name: "香港 02", proto: "Trojan", ms: "93 ms", tone: "ok" as Tone, picked: false },
  { id: "a3", name: "香港 03", proto: "SS", ms: "未测", tone: "unknown" as Tone, picked: false },
  { id: "a4", name: "日本 02", proto: "Hysteria2", ms: "120 ms", tone: "ok" as Tone, picked: true },
  { id: "a5", name: "日本 03", proto: "TUIC", ms: "超时", tone: "bad" as Tone, picked: false },
  { id: "a6", name: "美国 01", proto: "VLESS · REALITY", ms: "160 ms", tone: "ok" as Tone, picked: false },
];

// 订阅：每个人只管自己的；从「我的节点」（分到的 + 自己的链式节点）里挑
export const myLinks = [
  { id: "a", owner: "Tulip", name: "手机", nodes: [1, 20, 101], window: "一直可用", last: "15:52", pulls: 55, on: true },
  { id: "b", owner: "Tulip", name: "电脑 · 只走家宽", nodes: [101], window: "到 2026-12-31", last: "16:02", pulls: 12, on: true },
  { id: "c", owner: "shus808", name: "默认", nodes: [1, 2, 3, 8, 4, 5, 6], window: "一直可用", last: "15:52", pulls: 55, on: true },
];

export const todos = [
  { id: "t1", tone: "bad" as Tone, title: "方微云 Hysteria2", tag: "UDP 35138 未放行", why: "ufw 已启用，只有 35138/tcp。Gate 可以直接放行，放行后自动重新检查。", action: "由 Gate 放行", node: 8 },
  { id: "t5", tone: "warn" as Tone, title: "方微云", tag: "本月流量已用 81%", why: "812 / 1000 GB（只算出站），12 天后重置；其中 17.9 GB 是国内网站被误判走了代理。", action: "看误判", href: "/rules" },
  { id: "t2", tone: "warn" as Tone, title: "方微云 VLESS :20596", tag: "经美东太慢", why: "经代理「美东」P90 只有 0.8 MB/s，同台直连 12.6 MB/s。", action: "换落地", node: 3 },
  { id: "t3", tone: "warn" as Tone, title: "方微云", tag: "证书 2 天后到期", why: "自动续签上次失败：80 端口被其他程序占用。", action: "重试续签" },
];

export const udpText: Record<UDP, string> = { full: "完整", tcp: "仅 TCP", unknown: "未知" };
export const udpTone: Record<UDP, Tone> = { full: "ok", tcp: "warn", unknown: "unknown" };

export function sourceLabel(n: ProtoNode) {
  if (n.source.kind === "gate") return gates.find((g) => g.id === (n.source as { gateId: number }).gateId)?.name ?? "未知 VPS";
  if (n.source.kind === "sub") return n.source.name;
  return `${n.source.owner} 的链式`;
}

export const gb = (v: number | null) => (v == null ? "未知" : v >= 100 ? `${Math.round(v)} GB` : `${v} GB`);
export const mbps = (v: number | null) => (v == null ? "未知" : `${v} MB/s`);
