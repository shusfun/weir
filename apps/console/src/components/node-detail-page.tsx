// 节点详情页（设计稿 BNodeDetail）：对象页头 + 右侧「最近实测」卡；标签页：链路三段、凭据、出口、端口与防火墙、记录。
// 实测数据缺失时写未知；链路三段只写真实数据（用户数、入口 VPS、出口），读不到的写未知。
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Badge, Button, Card, ObjectHeader, SwitchField, Tabs, TabsList, TabsPanel, TabsTab } from "@weir/ui";
import { ExitSummary } from "./node-summary";
import { exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { NodeTestControl } from "./node-test";
import { PendingExits } from "./pending-exits";
import { nodeStatus, protocolNames, type Gate, type NodeItem, type NodeTestCheck } from "../lib/core";
import { rateText } from "../lib/linkmap";

const securityNames: Record<string, string> = { reality: "REALITY", tls: "TLS" };

function checkValue(c?: NodeTestCheck) {
  if (!c) return { value: "未知", unit: "" };
  if (c.status === "not_applicable") return { value: "不支持", unit: "" };
  if (c.status !== "passed") return { value: c.status === "failed" ? "失败" : "未知", unit: "" };
  return c.latency_ms != null ? { value: (c.latency_ms / 1000).toFixed(2), unit: "s" } : { value: "通过", unit: "" };
}

// 右侧的实测卡：点阵光环 + 三个读数
function TestCard({ node }: { node: NodeItem }) {
  const t = node.test;
  const tcp = checkValue(t?.tcp);
  const udp = t?.udp?.status === "passed" ? "通过" : t?.udp?.status === "not_applicable" ? "不支持" : t?.udp?.status === "failed" ? "失败" : "未知";
  const dots = Array.from({ length: 5 }, (_, k) => Array.from({ length: 23 + k * 7 }, (_, i) => {
    const a = (i / (23 + k * 7)) * Math.PI * 2;
    return { x: 190 + Math.cos(a) * (19 + k * 23), y: 55 + Math.sin(a) * (9 + k * 9), o: 0.9 - k * 0.15 };
  })).flat();
  return (
    <Card className="px-[21px] py-[21px]">
      <div className="flex items-center justify-between gap-3">
        <b>最近实测</b>
        <span className="font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{t ? `${new Date(t.at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false })} · ${t.location === "gate" ? "VPS" : "中心"} · mihomo` : "还没有实测"}</span>
      </div>
      <svg viewBox="0 0 381 111" className="my-[13px] block h-[111px] w-full" aria-hidden>
        {dots.map((d, i) => <circle key={i} cx={d.x} cy={d.y} r={1.5} opacity={t?.status === "passed" ? d.o : d.o * 0.37} fill={t?.status === "failed" ? "var(--weir-bad)" : "var(--weir-dot-line)"} />)}
      </svg>
      <div className="grid grid-cols-3 gap-3">
        <div><span className="font-[family-name:var(--weir-mono)] text-title2 font-bold">{tcp.value}</span><small className="ml-1 text-fine text-[var(--weir-ink-3)]">{tcp.unit}</small><div className="text-fine text-[var(--weir-ink-3)]">TCP 建连</div></div>
        <div><span className="font-[family-name:var(--weir-mono)] text-title2 font-bold">{node.rate?.minutes ? rateText(node.rate.p90_bps) : "未知"}</span><div className="text-fine text-[var(--weir-ink-3)]">下载</div></div>
        <div><span className="text-headline font-bold">{udp}</span><div className="text-fine text-[var(--weir-ink-3)]">UDP</div></div>
      </div>
    </Card>
  );
}

function Segment({ index, kind, title, sub, tone }: { index: number; kind: string; title: string; sub: string; tone?: "warn" }) {
  return (
    <div className="weir-chain-seg" data-tone={tone}>
      <small>{["①", "②", "③"][index]} {kind}</small>
      <b className="block truncate text-headline">{title}</b>
      <span className="block truncate font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{sub}</span>
    </div>
  );
}

export function NodeDetailPage({ node: n, gate: g, onEdit, onToggle, onDelete, busy }: { node: NodeItem; gate?: Gate; onEdit: () => void; onToggle: (enabled: boolean) => void; onDelete: () => void; busy: boolean }) {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const tab = params.get("tab") || "chain";
  const [label, tone] = nodeStatus(n, g);
  const tcpOnly = n.exit?.udp === "unsupported";
  const tags = [protocolNames[n.protocol] ?? n.protocol, securityNames[n.security], String(n.port)].filter(Boolean);
  const exitName = !n.exit || n.exit.kind === "freedom" ? "直连" : n.exit.summary?.name || n.exit.label || exitKindName[n.exit.kind] || "未知";
  return (
    <div className="stack">
      <ObjectHeader
        back={<Link to="/gates">VPS 与节点</Link>}
        crumb={<><Link className="text-[var(--weir-accent-text)] no-underline" to={`/gates/${n.gate_id}`}>{g?.name ?? "未知 VPS"}</Link><span className="mx-[7px] font-normal text-[var(--weir-ink-3)]">/</span>{n.name}</>}
        actions={<><Button variant="outline" onClick={onEdit}>编辑</Button><Button onClick={() => setParams((p) => { p.set("tab", "chain"); return p; }, { replace: true })}>实测</Button></>}
        tone={tone}
        status={[label === "在线" ? "可用" : label, tcpOnly ? "仅 TCP" : null, g ? `生效配置 r${g.applied_revision}` : null].filter(Boolean).join(" · ")}
        title={n.name}
        meta={<span className="weir-tags">{tags.map((t) => <span key={t}>{t}</span>)}</span>}
        banner={<p className="m-0 max-w-[521px] text-lead text-[var(--weir-ink-2)]">客户端经 {g?.name ?? "未知 VPS"} 进来，{exitName === "直连" ? "直接出去" : `再从${exitName}出去`}。{tcpOnly ? "这个出口传不了 UDP，Weir 会让客户端直接改走 TCP。" : ""}</p>}
        aside={<TestCard node={n} />}
      />
      <Tabs value={tab} onValueChange={(v) => setParams((p) => { p.set("tab", String(v)); return p; }, { replace: true })}>
        <TabsList aria-label="节点详情">
          <TabsTab value="chain">链路</TabsTab>
          <TabsTab value="credentials">凭据 {n.credentials}</TabsTab>
          <TabsTab value="exit">出口</TabsTab>
          <TabsTab value="ports">端口与防火墙</TabsTab>
          <TabsTab value="log">记录</TabsTab>
        </TabsList>
        <TabsPanel value="chain" className="stack">
          <div className="weir-chain">
            <Segment index={0} kind="客户端" title={n.credentials ? `${n.credentials} 份凭据` : "还没有凭据"} sub={n.rate?.minutes ? `共 ${rateText(n.rate.p90_bps)} · 订阅里叫「${n.name}」` : `订阅里叫「${n.name}」`} />
            <span className="weir-chain-dots" aria-hidden><i /><i /><i /></span>
            <Segment index={1} kind="入口 VPS" title={g?.name ?? "未知"} sub={tags.join(" · ")} />
            <span className="weir-chain-dots" aria-hidden><i /><i /><i /></span>
            <Segment index={2} kind="出口" title={exitName} sub={[n.exit?.address, n.exit?.place?.city, tcpOnly ? "仅 TCP" : null].filter(Boolean).join(" · ") || "地址未知"} tone={tcpOnly ? "warn" : undefined} />
          </div>
          <Card className="overflow-hidden p-0">
            <div className="weir-set-row"><div><b>伪装目标</b><div className="text-meta text-[var(--weir-ink-3)]">{String(n.settings?.reality_dest ?? n.settings?.tls_sni ?? "未知")}</div></div><Button size="sm" variant="ghost" onClick={onEdit}>换一个</Button></div>
            <div className="weir-set-row"><div><b>实测</b><div className="text-meta text-[var(--weir-ink-3)]">长期内部凭据，实测时不改配置、不重启内核</div></div><div className="w-[341px] max-w-full"><NodeTestControl node={n} gate={g} /></div></div>
            <div className="weir-set-row"><div><b>UDP 处理</b><div className="text-meta text-[var(--weir-ink-3)]">{tcpOnly ? "服务端直接拒绝，客户端立刻改走 TCP" : n.exit?.udp === "supported" ? "出口支持 UDP" : "出口的 UDP 支持未知"}</div></div>{tcpOnly ? <Badge variant="soft" tone="neutral">仅 TCP</Badge> : null}</div>
            <div className="weir-set-row"><div><b>启用</b><div className="text-meta text-[var(--weir-ink-3)]">停用后这个节点不在任何订阅里，VPS 上也不再监听</div></div><SwitchField label="启用" className="[&_label]:sr-only" checked={n.enabled} disabled={busy} onCheckedChange={onToggle} /></div>
          </Card>
        </TabsPanel>
        <TabsPanel value="credentials" className="stack">
          <p className="m-0 text-body text-[var(--weir-ink-2)]">每条订阅链接一份凭据，共 {n.credentials} 份。凭据原文在 <Link to="/users">用户</Link> 的链接里查看。</p>
        </TabsPanel>
        <TabsPanel value="exit" className="stack">
          <div className="text-body"><ExitSummary exit={n.exit} expanded /></div>
          {n.exit.kind !== "freedom" && <p className="m-0 text-meta text-[var(--weir-ink-3)]">{probeText(n.exit.probe)}</p>}
          <PendingExits all nodeId={n.id} />
          <div><Button variant="outline" onClick={() => navigate(`/exits?gate=${n.gate_id}`)}>去出口与分流</Button></div>
        </TabsPanel>
        <TabsPanel value="ports" className="stack"><NodeAccess ports={n.ports} reach={n.reach} /></TabsPanel>
        <TabsPanel value="log" className="stack">
          <p className="m-0 text-body text-[var(--weir-ink-2)]">这个节点的操作记录在 <Link to={`/ops/audit?q=node:${n.id}`}>操作审计</Link>。</p>
          <div><Button variant="destructive" disabled={busy} onClick={onDelete}>删除节点</Button></div>
        </TabsPanel>
      </Tabs>
    </div>
  );
}
