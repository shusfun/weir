// 链路页的数据：快照（结构）+ SSE（实时速率）。速率放在不触发渲染的仓库里，画布每帧读取并在两次采样之间平滑过渡；
// 面板与列表每秒刷新一次。没有收到的数据显示「未知」，超过 stale_ms 没有新帧时标「数据中断」，不往后推算。
import { useEffect, useRef, useState } from "react";
import type { ExitProbe, PortNeed, Reach } from "./core";

export interface GeoPlace {
  known: boolean;
  ip?: string;
  lat?: number;
  lon?: number;
  city?: string;
  region?: string;
  country?: string;
  asn?: number;
  org?: string;
  why?: string;
}
export interface MapNode {
  id: number;
  name: string;
  protocol: string;
  network: string;
  security: string;
  port: number;
  kernel?: string;
  enabled: boolean;
  ports?: PortNeed[];
  reach?: Reach;
  users?: number;
}
export interface MapGate {
  id: number;
  name: string;
  online: boolean;
  place: GeoPlace;
  host?: Record<string, number | string | undefined>;
  state?: string;
  last_error?: string;
  kernels?: Record<string, string>;
  version?: string;
  last_seen_at?: string;
  entry: boolean;
  nodes: MapNode[];
}
export interface MapExit {
  id: string;
  kind: string;
  label: string;
  sub?: string;
  from_gate: number;
  to_gate?: number;
  owner: "admin" | "self";
  address?: string;
  place: GeoPlace;
  probe?: ExitProbe;
  members?: string[];
  skipped?: number;
  group?: boolean;
}
export interface MapPath {
  id: string;
  user_id: number;
  user: string;
  node_id: number;
  node: string;
  gate_id: number;
  chain_id?: number;
  chain?: string;
  route: "chain" | "nodechain" | "direct";
  exit_id: string;
  today_bytes: number | null;
}
export interface LinkMap {
  admin: boolean;
  version: string;
  geo: { ready: boolean; error?: string };
  gates: MapGate[];
  exits: MapExit[];
  paths: MapPath[];
  rules?: { id: number; gate_id: number; label: string; exit_id: string; node_id?: number; user_id?: number }[];
  users?: { id: number; name: string; paths: string[] }[];
  hidden_rules?: number;
}

export interface Rate {
  up: number;
  down: number;
}
interface LiveEvent {
  gate: number;
  at: number;
  net?: Rate;
  paths: Record<string, Rate>;
  exits: Record<string, Rate>;
  active?: Record<string, number>;
}

// Series 是一个对象最近 5 分钟的速率（每 2 秒一点；null 表示那一帧算不出来）。
export interface Series {
  at: number[];
  v: (number | null)[];
  last?: Rate;
}

const keep = 150;

export class LiveStore {
  paths = new Map<string, Series>();
  exits = new Map<string, Series>();
  nets = new Map<number, Series>();
  active = new Map<string, number>();
  gateAt = new Map<number, number>(); // 每台 Gate 最近一帧的时间
  pulses: { gate: number; at: number }[] = [];
  // connected：收到过 hello 且 35 秒内收到过任何消息（中心每 15 秒推一次 ping；反代可能不把断开传给浏览器）。
  private open = false;
  heardAt = 0;
  get connected() {
    return this.open && Date.now() - this.heardAt < 35_000;
  }
  set connected(v: boolean) {
    this.open = v;
    if (v) this.heardAt = Date.now();
  }
  staleMs = 10_000;
  everyMs = 2_000;

  private push<K>(map: Map<K, Series>, key: K, at: number, r: Rate | undefined) {
    let s = map.get(key);
    if (!s) map.set(key, (s = { at: [], v: [] }));
    s.at.push(at);
    s.v.push(r ? r.up + r.down : null);
    if (r) s.last = r;
    if (s.at.length > keep) {
      s.at.shift();
      s.v.shift();
    }
  }

  // ingest 收一帧；pathIds / exitIds 是这台 Gate 上应该有的对象（没出现的记为 null）。
  ingest(ev: LiveEvent, pathIds: string[], exitIds: string[]) {
    for (const id of pathIds) this.push(this.paths, id, ev.at, ev.paths[id]);
    for (const id of exitIds) this.push(this.exits, id, ev.at, ev.exits[id]);
    if (ev.net !== undefined) this.push(this.nets, ev.gate, ev.at, ev.net);
    for (const [k, v] of Object.entries(ev.active ?? {})) this.active.set(k, v);
    this.gateAt.set(ev.gate, ev.at);
    this.pulses.push({ gate: ev.gate, at: performance.now() });
    if (this.pulses.length > 50) this.pulses.shift();
  }

  // stale：这台 Gate 超过 staleMs 没有新帧（或从没收到过）。
  stale(gate: number, now = Date.now()) {
    const at = this.gateAt.get(gate);
    return at === undefined || now - at > this.staleMs;
  }

  // smooth 在最近两次采样之间按时间插值（只在两个真实值之间过渡，不往后推）。
  smooth(s: Series | undefined, now = Date.now()): number | null {
    if (!s || s.v.length === 0) return null;
    const n = s.v.length;
    const b = s.v[n - 1];
    if (b === null) return null;
    const a = n > 1 ? s.v[n - 2] : null;
    if (a === null) return b;
    const t = Math.min(1, Math.max(0, (now - s.at[n - 1]) / this.everyMs));
    return a + (b - a) * t;
  }
}

// useLinkMap 取快照并保持 SSE 连接；结构变了重新取快照。tick 每秒加一，驱动面板刷新。
export function useLinkMap(admin: boolean) {
  const base = admin ? "/api/v1/admin/linkmap" : "/api/v1/me/linkmap";
  const [map, setMap] = useState<LinkMap | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [tick, setTick] = useState(0);
  const store = useRef(new LiveStore());
  const mapRef = useRef<LinkMap | null>(null);
  const load = async () => {
    try {
      const res = await fetch(base, { credentials: "same-origin", headers: { Accept: "application/json" } });
      const data = await res.json().catch(() => null);
      if (!res.ok) throw Object.assign(new Error(data?.error?.code === "forbidden" ? "没有权限查看" : `读取链路失败（${res.status}）`), { status: res.status });
      mapRef.current = data as LinkMap;
      setMap(data as LinkMap);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e : new Error(String(e)));
    }
  };
  useEffect(() => {
    void load();
    // 浏览器只在网络断开时自动重连；服务端（或反代）回了非 200 时 EventSource 会永久关闭，这里按退避自己重建。
    let es: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let wait = 2000;
    let stopped = false;
    const connect = () => {
      store.current.heardAt = Date.now();
      es = new EventSource(base + "/live");
      es.addEventListener("hello", (e) => {
        const h = JSON.parse((e as MessageEvent).data);
        store.current.connected = true;
        store.current.staleMs = h.stale_ms ?? 10_000;
        store.current.everyMs = h.every_ms ?? 2_000;
        wait = 2000;
        if (mapRef.current && h.version !== mapRef.current.version) void load();
      });
      es.addEventListener("frame", (e) => {
        const ev = JSON.parse((e as MessageEvent).data) as LiveEvent;
        const m = mapRef.current;
        const paths = m ? m.paths.filter((p) => p.gate_id === ev.gate).map((p) => p.id) : Object.keys(ev.paths);
        const exits = m ? m.exits.filter((x) => x.from_gate === ev.gate).map((x) => x.id) : Object.keys(ev.exits);
        store.current.ingest(ev, paths, exits);
        store.current.heardAt = Date.now();
      });
      es.addEventListener("topology", () => void load());
      es.addEventListener("ping", () => (store.current.heardAt = Date.now()));
      es.onerror = () => {
        store.current.connected = false;
        if (es?.readyState === EventSource.CLOSED) reconnect();
      };
    };
    const reconnect = () => {
      if (stopped || retry) return;
      es?.close();
      retry = setTimeout(() => {
        retry = undefined;
        void load();
        connect();
      }, wait);
      wait = Math.min(wait * 2, 30_000);
    };
    connect();
    // 半开的连接（反代没把断开传给浏览器）不会报错：35 秒没收到任何消息（中心每 15 秒推 ping）就重建。
    const t = setInterval(() => {
      setTick((n) => n + 1);
      if (Date.now() - store.current.heardAt > 35_000) {
        store.current.connected = false;
        reconnect();
      }
    }, 1000);
    return () => {
      stopped = true;
      es?.close();
      clearTimeout(retry);
      clearInterval(t);
    };
  }, [base]);
  return { map, error, store: store.current, tick, reload: load };
}

// rateText：字节 / 秒换成易读的速率；null 是未知。
export function rateText(v: number | null | undefined) {
  if (v === null || v === undefined) return "未知";
  const units = ["B/s", "KB/s", "MB/s", "GB/s"];
  let n = v;
  let i = 0;
  while (n >= 1000 && i < units.length - 1) {
    n /= 1000;
    i++;
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}

const regionNames = typeof Intl !== "undefined" && "DisplayNames" in Intl ? new Intl.DisplayNames(["zh-CN"], { type: "region" }) : null;

// placeText：城市 · 国家（国家按代码显示中文名）；不知道位置时给出原因。
export function placeText(p?: GeoPlace) {
  if (!p || !p.known) return p?.why ? `位置未知（${p.why}）` : "位置未知";
  let country = p.country ?? "";
  try {
    country = (p.country && regionNames?.of(p.country)) || country;
  } catch {
    // 不认识的代码原样显示
  }
  return [p.city, country].filter(Boolean).join(" · ") || "位置未知";
}
