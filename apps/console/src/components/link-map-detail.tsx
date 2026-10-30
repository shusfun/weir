// 链路图右侧（窄屏在下方）的详情：选中 VPS、出口或路径时显示它的全部数据，速率每秒刷新、带最近 5 分钟的小曲线。
// 没有选中时显示总览。观测不到的值写「未知」，不写 0。
import { Badge } from "@weir/ui";
import { bytes, date } from "../lib/models";
import { placeText, rateText, type LinkMap, type LiveStore, type MapExit, type MapGate, type MapPath, type Series } from "../lib/linkmap";
import { exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { ObjectLink } from "./object-link";

// Spark 是最近 5 分钟的速率曲线；算不出来的帧断开，不连成 0。
export function Spark({ s }: { s?: Series }) {
  const v = s?.v ?? [];
  const max = Math.max(1, ...v.map((x) => x ?? 0));
  const w = 160;
  const h = 28;
  const segs: string[] = [];
  let cur = "";
  v.forEach((x, i) => {
    if (x === null) {
      if (cur) segs.push(cur);
      cur = "";
      return;
    }
    const px = v.length > 1 ? (i / (v.length - 1)) * w : w;
    cur += `${cur ? "L" : "M"}${px.toFixed(1)},${(h - (x / max) * (h - 2) - 1).toFixed(1)}`;
  });
  if (cur) segs.push(cur);
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-hidden className="block text-[var(--weir-accent)]">
      {segs.map((d) => <path key={d} d={d} fill="none" stroke="currentColor" strokeWidth={1.5} />)}
    </svg>
  );
}

const unknown = "未知";
const pct = (v: unknown) => (typeof v === "number" ? v.toFixed(1) + "%" : unknown);
const size = (v: unknown) => (typeof v === "number" ? bytes(v) : unknown);

function Row({ k, children }: { k: string; children: React.ReactNode }) {
  return (
    <>
      <dt className="text-[var(--weir-ink-3)]">{k}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  );
}

function Live({ store, s, gate }: { store: LiveStore; s?: Series; gate: number }) {
  const stale = store.stale(gate);
  const at = store.gateAt.get(gate);
  const v = store.smooth(s);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <strong className="tabular-nums">{stale ? unknown : rateText(v)}</strong>
      {stale && <small className="text-[var(--weir-warn-ink)]">{at ? `数据中断 ${Math.round((Date.now() - at) / 1000)} 秒` : store.connected ? "等待 VPS 上报" : "实时连接中断，正在重连"}</small>}
      <Spark s={s} />
    </div>
  );
}
function GateDetail({ m, g, store }: { m: LinkMap; g: MapGate; store: LiveStore }) {
  const h = g.host ?? {};
  const paths = m.paths.filter((p) => p.gate_id === g.id);
  const users = new Set(paths.map((p) => p.user_id));
  const active = paths.filter((p) => (store.smooth(store.paths.get(p.id)) ?? 0) > 0);
  return (
    <div className="stack gap-2">
      <h3 className="flex items-center gap-2 font-semibold">
        {m.admin ? <ObjectLink to={{ type: "gate", id: g.id, name: g.name }} /> : g.name}
        <Badge variant="soft" tone={g.online ? "ok" : "neutral"}>{g.online ? "在线" : "离线"}</Badge>
      </h3>
      <dl className="grid grid-cols-[5.5rem_1fr] gap-x-2 gap-y-1 text-[12.5px]">
        <Row k="位置">{placeText(g.place)}</Row>
        {g.place.org && <Row k="运营商">{g.place.org}{g.place.asn ? ` · AS${g.place.asn}` : ""}</Row>}
        {g.place.ip && <Row k="地址">{g.place.ip}</Row>}
        {m.admin && <Row k="网卡">{store.nets.has(g.id) ? <Live store={store} s={store.nets.get(g.id)} gate={g.id} /> : unknown}</Row>}
        {m.admin && <Row k="CPU / 负载">{pct(h.cpu_percent)} · {typeof h.load1 === "number" ? h.load1.toFixed(2) : unknown}</Row>}
        {m.admin && <Row k="内存">{size(h.mem_used)} / {size(h.mem_total)}</Row>}
        {m.admin && <Row k="磁盘">{size(h.disk_used)} / {size(h.disk_total)}</Row>}
        {m.admin && <Row k="配置">{g.state === "applied" ? "已生效" : g.state === "pending" ? "待生效" : g.state === "failed" ? `失败：${g.last_error || unknown}` : g.state ?? unknown}</Row>}
        {m.admin && <Row k="内核">{Object.entries(g.kernels ?? {}).map(([k, v]) => `${k} ${v}`).join("、") || "无"}</Row>}
        {m.admin && <Row k="最近心跳">{date(g.last_seen_at)}</Row>}
        <Row k="正在用的人">{store.stale(g.id) ? unknown : `${new Set(active.map((p) => p.user_id)).size} / ${users.size}`}</Row>
      </dl>
      {g.nodes.length > 0 && (
        <ul className="stack gap-2">
          {g.nodes.map((n) => {
            const np = paths.filter((p) => p.node_id === n.id);
            const r = np.reduce<number | null>((sum, p) => {
              const x = store.smooth(store.paths.get(p.id));
              return x === null ? sum : (sum ?? 0) + x;
            }, null);
            return (
              <li key={n.id} className="stack gap-1 rounded-[var(--weir-radius-sm)] border border-[var(--weir-line)] p-2">
                <span className="flex flex-wrap items-center justify-between gap-2">
                  <strong className="text-[12.5px]">{m.admin ? <ObjectLink to={{ type: "node", id: n.id, name: n.name }} /> : n.name}</strong>
                  <small className="tabular-nums">{store.stale(g.id) ? unknown : rateText(r)}</small>
                </span>
                <small className="text-[var(--weir-ink-3)]">{n.protocol} · {n.network}{n.security !== "none" ? " · " + n.security : ""} · 端口 {n.port}{n.kernel ? ` · ${n.kernel}` : ""}{n.users !== undefined ? ` · ${n.users} 个用户` : ""}{!n.enabled ? " · 已停用" : ""}</small>
                {m.admin && <NodeAccess ports={n.ports} reach={n.reach} compact />}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
function ExitDetail({ m, x, store }: { m: LinkMap; x: MapExit; store: LiveStore }) {
  const from = m.gates.find((g) => g.id === x.from_gate);
  const paths = m.paths.filter((p) => p.exit_id === x.id);
  const active = store.active.get(x.id);
  return (
    <div className="stack gap-2">
      <h3 className="font-semibold">{x.label}</h3>
      <dl className="grid grid-cols-[5.5rem_1fr] gap-x-2 gap-y-1 text-[12.5px]">
        <Row k="类型">{exitKindName[x.kind] ?? x.kind}{x.owner === "self" ? "（自建）" : m.admin ? "" : "（管理员配置）"}</Row>
        <Row k="入口">{from?.name ?? unknown}</Row>
        {x.sub && <Row k="说明">{x.sub}</Row>}
        {x.address && <Row k="地址">{x.address}</Row>}
        <Row k="位置">{placeText(x.place)}</Row>
        {x.place.org && <Row k="运营商">{x.place.org}{x.place.asn ? ` · AS${x.place.asn}` : ""}</Row>}
        {x.kind !== "direct" && x.kind !== "block" && <Row k="实测">{probeText(x.probe)}{x.probe?.at ? ` · ${date(x.probe.at)}` : ""}</Row>}
        <Row k="实时">{<Live store={store} s={store.exits.get(x.id)} gate={x.from_gate} />}</Row>
        <Row k="经过的路径">{paths.length}</Row>
      </dl>
      {x.members && x.members.length > 0 && (
        <div className="stack gap-1">
          <small className="text-[var(--weir-ink-3)]">{x.group ? "一组，由内核按延迟自动选择与切换" : "订阅节点"}{x.skipped ? `；另有 ${x.skipped} 个匹配的节点不能作出口` : ""}</small>
          <ul className="flex flex-wrap gap-1">
            {x.members.map((name, i) => (
              <li key={name + i}><Badge variant="soft" tone={x.group && active === i ? "ok" : "neutral"}>{name}{x.group && active === i ? " · 当前有流量" : ""}</Badge></li>
            ))}
          </ul>
          {x.group && m.admin && <small className="text-[var(--weir-ink-3)]">{active === undefined ? "当前成员：未知（还没有实时数据）" : active < 0 ? "这一刻没有流量" : ""}</small>}
        </div>
      )}
    </div>
  );
}

function PathDetail({ m, p, store }: { m: LinkMap; p: MapPath; store: LiveStore }) {
  const x = m.exits.find((e) => e.id === p.exit_id);
  const route = p.route === "chain" ? `自建链式代理「${p.chain}」` : p.route === "nodechain" ? "节点的链式代理" : "直接出去";
  return (
    <div className="stack gap-2">
      <h3 className="font-semibold">{m.admin ? <ObjectLink to={{ type: "user", id: p.user_id, name: p.user }} /> : p.user} → {p.node}</h3>
      <dl className="grid grid-cols-[5.5rem_1fr] gap-x-2 gap-y-1 text-[12.5px]">
        <Row k="入口">{m.gates.find((g) => g.id === p.gate_id)?.name ?? unknown} · {p.node}</Row>
        <Row k="路由">{route}</Row>
        <Row k="出口">{x?.label ?? unknown}</Row>
        <Row k="实时">{<Live store={store} s={store.paths.get(p.id)} gate={p.gate_id} />}</Row>
        <Row k="今天">{p.today_bytes === null ? unknown : bytes(p.today_bytes)}</Row>
      </dl>
      {!m.admin && (m.hidden_rules ?? 0) > 0 && <small className="text-[var(--weir-ink-3)]">入口上有 {m.hidden_rules} 条管理员分流规则，部分网站可能从别的出口出去。</small>}
    </div>
  );
}

export function LinkMapDetail({ m, store, selected }: { m: LinkMap; store: LiveStore; selected: string | null }) {
  const [kind, id] = selected ? [selected.slice(0, 2), selected.slice(2)] : ["", ""];
  if (kind === "g:") {
    const g = m.gates.find((y) => String(y.id) === id);
    if (g) return <GateDetail m={m} g={g} store={store} />;
  }
  if (kind === "x:") {
    const x = m.exits.find((y) => y.id === id);
    if (x) return <ExitDetail m={m} x={x} store={store} />;
  }
  if (kind === "p:") {
    const p = m.paths.find((y) => y.id === selected);
    if (p) return <PathDetail m={m} p={p} store={store} />;
  }
  const total = m.paths.reduce<number | null>((sum, p) => {
    const v = store.stale(p.gate_id) ? null : store.smooth(store.paths.get(p.id));
    return v === null ? sum : (sum ?? 0) + v;
  }, null);
  return (
    <div className="stack gap-2 text-[12.5px]">
      <h3 className="font-semibold">总览</h3>
      <dl className="grid grid-cols-[5.5rem_1fr] gap-x-2 gap-y-1">
        <Row k="VPS">{m.gates.length}（在线 {m.gates.filter((g) => g.online).length}）</Row>
        <Row k="出口">{m.exits.filter((x) => x.kind !== "direct").length}</Row>
        <Row k="路径">{m.paths.length}</Row>
        <Row k="实时合计">{rateText(total)}</Row>
      </dl>
      <small className="text-[var(--weir-ink-3)]">点地图上的点或线、或下面列表里的一行查看详情。</small>
    </div>
  );
}
