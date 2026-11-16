// 原型共用部分：外壳（分组导航，管理员 / 普通用户两套）、子标签、服务状态卡、节点详情弹窗、添加代理弹窗。
// 只用 @weir/ui 现有组件拼；节点用 Chip / ChipLabel（状态灯 + 名称 + 说明），不另写组件。
import { createContext, useContext, useState, type ReactNode } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { Activity, Cable, FileClock, Gauge, Link2, ListTree, Megaphone, Rss, Search, Server, Settings2, ShieldCheck, Ticket as TicketIcon, Users, Cpu, Layers } from "lucide-react";
import {
  Badge, Button, Card, CardContent, Checkbox, ChipLabel, ChoiceCard, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, FlowBoard, GlobeBackground,
  HoldButton, InsetGroup, InsetRow, InsetRows, Kbd, Notice, Readout, Segmented, SwitchField, Tabs, TabsList, TabsTab, Textarea, useToast, type LampTone,
} from "@weir/ui";
import { BrandMark } from "../components/brand-mark";
import { gb, nodes, proxies, sourceLabel, udpText, udpTone, type ProtoNode, type Tone } from "./data";

export const base = "/dev/proto";

// 原型里切换身份只为演示两种账户看到什么；正式产品没有这个切换
export type Role = "admin" | "user";
const RoleContext = createContext<Role>("admin");
export const useRole = () => useContext(RoleContext);

type Item = { path: string; label: string; icon: typeof Server };
const adminNav: [string, Item[]][] = [
  ["", [{ path: base, label: "概览", icon: Gauge }]],
  ["线路", [{ path: base + "/nodes", label: "节点", icon: Server }, { path: base + "/proxies", label: "代理", icon: Cable }, { path: base + "/rules", label: "规则", icon: ListTree }, { path: base + "/kernels", label: "内核", icon: Cpu }]],
  ["用户", [{ path: base + "/users", label: "用户", icon: Users }, { path: base + "/plans", label: "套餐与用码", icon: TicketIcon }]],
  ["订阅", [{ path: base + "/subs", label: "我的订阅", icon: Link2 }, { path: base + "/sources", label: "订阅源", icon: Rss }, { path: base + "/groups", label: "订阅组与模板", icon: Layers }, { path: base + "/pages", label: "公开页面", icon: Megaphone }]],
  ["运维", [{ path: base + "/observe", label: "运行观测", icon: Activity }, { path: base + "/audit", label: "操作审计", icon: FileClock }]],
  ["系统", [{ path: base + "/system", label: "设置", icon: Settings2 }]],
];
const userNav: [string, Item[]][] = [
  ["", [{ path: base + "/my/nodes", label: "我的节点", icon: Server }, { path: base + "/proxies", label: "我的代理", icon: Cable }, { path: base + "/subs", label: "我的订阅", icon: Link2 }, { path: base + "/me", label: "账户安全", icon: ShieldCheck }]],
];

const globeNodes = [
  { id: "g1", lat: 34.05, lon: -118.24, tone: "ok" as const, label: "方微云" },
  { id: "g2", lat: 22.3, lon: 114.17, tone: "ok" as const, label: "方微云-154" },
  { id: "e1", lat: 38.9, lon: -77.03, tone: "info" as const, label: "美东" },
];

export function ProtoShell({ children }: { children: ReactNode }) {
  const toast = useToast();
  const navigate = useNavigate();
  const [role, setRole] = useState<Role>("admin");
  const groups = role === "admin" ? adminNav : userNav;
  return (
    <RoleContext.Provider value={role}>
      <div className="shell">
        <GlobeBackground nodes={globeNodes} links={[{ from: "g1", to: "e1" }, { from: "g2", to: "e1" }]} />
        <aside className="rail">
          <NavLink to={base} className="rail-brand weir-press" aria-label="原型首页">
            <BrandMark className="rail-mark" />
            <strong>Weir</strong>
          </NavLink>
          <nav aria-label="原型导航" className="rail-nav">
            {groups.map(([group, items]) => (
              <div className="rail-group" key={group || "home"}>
                {group && <span className="rail-group-label weir-engraved">{group}</span>}
                {items.map(({ path, label, icon: Icon }) => (
                  <NavLink key={path} to={path} end={path === base} className={({ isActive }) => "weir-key weir-key-ghost rail-item" + (isActive ? " active" : "")}>
                    <Icon />
                    <span className="rail-label">{label}</span>
                  </NavLink>
                ))}
              </div>
            ))}
          </nav>
        </aside>
        <div className="shell-body">
          <header className="topbar">
            <button type="button" className="command-trigger weir-key" onClick={() => toast("命令面板（原型未接）", { description: "可搜索对象、跳转，也可以直接粘贴节点或订阅链接。" })}>
              <Search />
              <span>搜索对象、跳转或粘贴链接</span>
              <Kbd>Ctrl K</Kbd>
            </button>
            <div className="topbar-tools">
              <Segmented size="sm" aria-label="原型演示身份" value={role} onValueChange={(r) => { setRole(r); navigate(r === "admin" ? base : base + "/my/nodes"); }} options={[["admin", "管理员"], ["user", "普通用户"]]} />
              <Badge variant="soft" tone="info">原型 · 模拟数据</Badge>
            </div>
          </header>
          <main className="shell-main">
            <div className="stack">
              <div className="page-content">{children}</div>
            </div>
          </main>
        </div>
      </div>
    </RoleContext.Provider>
  );
}

// 页内子标签：和路由绑定，浏览器后退能回来
export function ModuleTabs({ tabs }: { tabs: [string, string][] }) {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const current = tabs.find(([p]) => p === pathname)?.[0] ?? tabs[0][0];
  return (
    <Tabs value={current} onValueChange={(v) => navigate(String(v))}>
      <TabsList>
        {tabs.map(([path, label]) => <TabsTab key={path} value={path}>{label}</TabsTab>)}
      </TabsList>
    </Tabs>
  );
}

// 服务状态：页面依赖的服务一行排开，用只读标签，右侧一个操作
export function ServiceCard({ items, action }: { items: { tone: LampTone; label: string; meta?: string }[]; action?: ReactNode }) {
  return (
    <Card size="sm">
      <CardContent className="actions">
        {items.map((it) => <ChipLabel key={it.label} tone={it.tone} meta={it.meta}>{it.label}</ChipLabel>)}
        {action && <span className="ml-auto">{action}</span>}
      </CardContent>
    </Card>
  );
}

export const badgeTone = (t: Tone) => (t === "unknown" ? "neutral" : t);
export const lampTone = (t: Tone): LampTone => t;

export function nodeMeta(n: ProtoNode) {
  return [n.port ? `:${n.port}` : null, sourceLabel(n), n.kernel].filter(Boolean).join(" · ");
}
export const nodeTitle = (n: ProtoNode) => (n.source.kind === "gate" ? n.protocol : n.name);

export function UdpBadge({ udp }: { udp: ProtoNode["udp"] }) {
  return <Badge variant="status" tone={badgeTone(udpTone[udp])}>{udpText[udp]}</Badge>;
}

// 节点详情（弹窗）：状态与处理、链路、两段测试、用量统计、可直接改的设置
export function NodeBody({ n }: { n: ProtoNode }) {
  const admin = useRole() === "admin";
  const toast = useToast();
  const [enabled, setEnabled] = useState(true);
  const [fixed, setFixed] = useState(false);
  const v = fixed ? { tone: "ok" as Tone, text: "已处理，正在重新检查" } : n.verdict;
  const landing = proxies.find((p) => p.name === n.landing);
  return (
    <div className="stack">
      {v.tone === "ok" ? (
        <Notice tone="ok">{v.text}{n.latency ? ` · ${n.latency} ms` : ""}</Notice>
      ) : (
        <Notice tone={v.tone === "bad" ? "bad" : v.tone === "warn" ? "warn" : "info"}>
          <div className="stack-tight">
            <strong>{v.text}</strong>
            {v.why && <span>{v.why}</span>}
            {v.fix && <div className="actions"><Button size="sm" onClick={() => { setFixed(true); toast(`${v.fix}（原型）`, { tone: "ok" }); }}>{v.fix}</Button></div>}
          </div>
        </Notice>
      )}
      <FlowBoard label="链路" steps={[
        { label: "客户端", value: n.users === "all" ? "全部用户" : `${n.users} 个用户`, status: "complete" },
        { label: "入口", value: n.source.kind === "chain" ? nodeTitle(nodes.find((x) => x.id === (n.source as { entry: number }).entry)!) : sourceLabel(n), status: n.verdict.tone === "bad" && !fixed ? "fail" : "complete" },
        { label: "出去", value: n.landing ?? "直接出去", status: n.udp === "tcp" ? "current" : "complete" },
      ]} />
      <InsetGroup title="用量" description="Gate 每 30 秒上报；速率取 24 小时 P90">
        <InsetRows>
          <InsetRow className="grid grid-cols-3 gap-[var(--weir-space-3)]">
            <Readout label="24 小时流量" value={n.stats.traffic24h} unit="GB" />
            <Readout label="速率 P90" value={n.stats.p90} unit="MB/s" />
            <Readout label="在用用户" value={n.stats.activeUsers} />
          </InsetRow>
          {admin && n.stats.top.map((t) => <InsetRow key={t.user} className="actions justify-between"><span>{t.user}</span><small className="text-[var(--weir-ink-2)]">{gb(t.gb)} · 24 小时</small></InsetRow>)}
        </InsetRows>
      </InsetGroup>
      <InsetGroup title="两段测试" actions={<Button size="sm" variant="ghost" onClick={() => toast("两段都在重测（原型）")}>再测一次</Button>}>
        <InsetRows>
          <InsetRow><Readout label="你 → VPS · 浏览器" value={n.source.kind === "gate" ? 43 : null} unit="ms" caption={n.source.kind === "gate" ? "UDP 可达 · QUIC 正常" : "只有自建 VPS 能测"} /></InsetRow>
          <InsetRow><Readout label="VPS → 出去 · 中心 mihomo" value={n.latency ?? null} unit="ms" caption={n.landing ? `经代理「${n.landing}」` : "直接出去"} /></InsetRow>
        </InsetRows>
      </InsetGroup>
      <InsetGroup title="设置">
        <InsetRows>
          <InsetRow><SwitchField placement="end" label="启用" description="停用后不进任何订阅，VPS 上也不监听" checked={enabled} onCheckedChange={(c) => { setEnabled(c); toast(c ? "已启用（原型）" : "已停用（原型）"); }} /></InsetRow>
          <InsetRow className="actions justify-between"><Readout label="出去" value={landing ? landing.name : "直接出去"} caption={landing ? `${landing.kind} · ${landing.address}` : "VPS 自己的 IP"} /><Button size="sm" variant="outline" onClick={() => toast("从代理里选（原型）")}>换代理</Button></InsetRow>
          <InsetRow className="actions justify-between"><Readout label="UDP" value={udpText[n.udp]} caption={n.udp === "tcp" ? "整条链仅 TCP：订阅里写 udp=false，服务端兜底拒绝" : n.udp === "full" ? "整条链能带 UDP" : "还没实测"} /><UdpBadge udp={n.udp} /></InsetRow>
        </InsetRows>
      </InsetGroup>
      <div className="actions">
        <Button variant="outline" onClick={() => toast("已复制节点链接（原型）")}>复制链接</Button>
        <Button variant="outline" onClick={() => toast("实测已开始（原型）")}>实测</Button>
        <span className="ml-auto" />
        <HoldButton variant="destructive" onHold={() => toast("节点已删除（原型）", { tone: "warn" })}>按住删除</HoldButton>
      </div>
    </div>
  );
}

export function NodeDialog({ node, close }: { node: ProtoNode | null; close: () => void }) {
  return (
    <Dialog open={!!node} onOpenChange={(o) => !o && close()}>
      {node && (
        <DialogContent size="lg">
          <DialogHeader>
            <DialogTitle>{node.name}</DialogTitle>
            <DialogDescription>{`${sourceLabel(node)} · ${node.protocol}${node.port ? ` · ${node.port}/${node.network}` : ""} · 订阅里叫「${node.name}」`}</DialogDescription>
          </DialogHeader>
          <NodeBody n={node} />
        </DialogContent>
      )}
    </Dialog>
  );
}

// 添加代理：粘贴识别 → VPS 实测（出口 IP、UDP 能力）→ 加密保存。管理员加的是系统代理，可直接选给哪些节点用
export function AddProxy({ open, close, admin }: { open: boolean; close: () => void; admin: boolean }) {
  const toast = useToast();
  const [kind, setKind] = useState("link");
  const [text, setText] = useState("socks5://user:••••••@203.0.113.66:1080#家宽 2");
  const [picked, setPicked] = useState<number[]>([3]);
  const candidates = nodes.filter((n) => n.source.kind === "gate");
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>添加代理</DialogTitle>
          <DialogDescription>粘贴一条代理或节点链接；只允许公网地址。保存前由 VPS 实际连一次，保存后凭据加密存储。</DialogDescription>
        </DialogHeader>
        <div className="stack">
          {admin && <div className="grid grid-cols-2 gap-[var(--weir-space-3)] sm:grid-cols-4">
            {[["link", "粘贴链接", "代理或节点"], ["sub", "订阅源节点", "一个或一组"], ["gate", "另一台 VPS", "Gate 之间转发"], ["block", "拦截", "丢弃流量"]].map(([k, t, d]) => (
              <ChoiceCard key={k} selected={kind === k} render={<button type="button" />} onClick={() => setKind(k)}><strong>{t}</strong><small className="text-[var(--weir-ink-3)]">{d}</small></ChoiceCard>
            ))}
          </div>}
          <Textarea mono rows={2} value={text} onChange={(e) => setText(e.target.value)} aria-label="链接" />
          <div className="grid grid-cols-3 gap-[var(--weir-space-3)]">
            <Readout framed label="识别为" value="SOCKS5 · 上海" />
            <Readout framed label="UDP" value="完整" />
            <Readout framed label="VPS 实测" value={33} unit="ms" caption="出口 IP 203.0.113.66" />
          </div>
          {admin ? (
            <InsetGroup title="给哪些节点用（可以之后再选）" description="写进规则：这些节点的流量从这个代理出去">
              <InsetRows>
                {candidates.map((n) => (
                  <InsetRow key={n.id} className="actions justify-between">
                    <span className="actions">
                      <Checkbox aria-label={`给 ${n.name} 用`} checked={picked.includes(n.id)} onCheckedChange={(c) => setPicked(c ? [...picked, n.id] : picked.filter((x) => x !== n.id))} />
                      <ChipLabel tone={lampTone(n.verdict.tone)} meta={nodeMeta(n)}>{n.protocol}</ChipLabel>
                    </span>
                    <small className="text-[var(--weir-ink-3)]">现在：{n.landing ?? "直接出去"}</small>
                  </InsetRow>
                ))}
              </InsetRows>
            </InsetGroup>
          ) : <Notice tone="info">保存后到「我的节点」用它新建节点：选一个入口节点，从这个代理出去。</Notice>}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={close}>取消</Button>
          <Button onClick={() => { toast(admin && picked.length ? `已保存，并给 ${picked.length} 个节点用（原型）` : "已实测并加密保存（原型）", { tone: "ok" }); close(); }}>实测并保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

