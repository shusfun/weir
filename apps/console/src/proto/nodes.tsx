// 原型 · 节点：管理员的 VPS（文件夹 + 协议紧凑行 + 供应商流量额度）与全部节点（含用户的链式节点）；普通用户的「我的节点」。
import { useState } from "react";
import { Plus, Server } from "lucide-react";
import {
  Badge, Button, Card, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Field, FolderGrid, FolderSlot, GlassFolder,
  InsightBanner, Input, Meter, MetricStrip, Notice, PageHeader, SectionHeader, Segmented, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast,
} from "@weir/ui";
import { gates, gb, mbps, nodes, proxies, sourceLabel, type ProtoGate, type ProtoNode } from "./data";
import { badgeTone, base, lampTone, ModuleTabs, NodeDialog, nodeMeta, nodeTitle, ServiceCard, UdpBadge, useRole } from "./parts";

const tabs: [string, string][] = [[base + "/nodes", "VPS"], [base + "/nodes/all", "全部节点"]];

function KernelService() {
  const toast = useToast();
  return <ServiceCard items={[
    { tone: "ok", label: "Xray 26.3.27", meta: "2 台运行中" },
    { tone: "ok", label: "sing-box 1.12.8", meta: "2 台运行中" },
    { tone: "warn", label: "1 个端口待放行", meta: "UDP 35138" },
  ]} action={<Button size="sm" variant="outline" onClick={() => toast("Gate 正在放行 UDP 35138（原型）", { tone: "ok" })}>全部由 Gate 放行</Button>} />;
}

const quotaText = (g: ProtoGate) => g.quota.totalGB == null ? `本周期 ${gb(g.quota.usedGB)} · 额度未设置` : `${gb(g.quota.usedGB)} / ${gb(g.quota.totalGB)} · ${g.quota.daysLeft} 天后重置`;

// 供应商给的月流量：额度、计费方式、重置日、用量校准
function QuotaDialog({ gate, close }: { gate: ProtoGate | null; close: () => void }) {
  const toast = useToast();
  const [mode, setMode] = useState("out");
  return (
    <Dialog open={!!gate} onOpenChange={(o) => !o && close()}>
      {gate && (
        <DialogContent>
          <DialogHeader><DialogTitle>{gate.name} 的流量额度</DialogTitle><DialogDescription>照供应商面板填写；Gate 按网卡计数累计本周期用量，快用完时提醒。</DialogDescription></DialogHeader>
          <div className="stack">
            <Field label="每月额度（GB）" name="total"><Input defaultValue={gate.quota.totalGB ?? ""} placeholder="例如 1000" /></Field>
            <Field label="计费方式" name="mode"><Segmented aria-label="计费方式" value={mode} onValueChange={setMode} options={[["both", "双向合计"], ["out", "只算出站"], ["max", "取较大一方"]]} /></Field>
            <Field label="每月重置日" name="reset" hint="1–31；当月没有这一天时取月末"><Input defaultValue={gate.quota.resetDay ?? ""} placeholder="例如 26" /></Field>
            <Field label="本周期已用（GB）" name="used" hint="和供应商面板对不上时填它的数字，从这里接着累计"><Input defaultValue={gate.quota.usedGB ?? ""} /></Field>
          </div>
          <DialogFooter><Button variant="ghost" onClick={close}>取消</Button><Button onClick={() => { toast("额度已保存（原型）", { tone: "ok" }); close(); }}>保存</Button></DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}

// 一台 VPS 里的协议：信息优先的紧凑行，带 24 小时流量与速率；详情进弹窗
function NodeRows({ list, showSource = false }: { list: ProtoNode[]; showSource?: boolean }) {
  const admin = useRole() === "admin";
  const toast = useToast();
  const [open, setOpen] = useState<ProtoNode | null>(null);
  return (
    <>
      <Table>
        <TableHeader><TableRow><TableHead>节点</TableHead><TableHead>出去</TableHead><TableHead>UDP</TableHead><TableHead>24 小时</TableHead><TableHead>速率 P90</TableHead><TableHead>{admin ? "用得最多" : "我用了"}</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
        <TableBody>
          {list.map((n) => (
            <TableRow key={n.id}>
              <TableCell className="whitespace-nowrap"><Chip tone={lampTone(n.verdict.tone)} meta={showSource ? nodeMeta(n) : `${n.port}/${n.network} · ${n.kernel ?? ""}`} onClick={() => setOpen(n)}>{nodeTitle(n)}</Chip></TableCell>
              <TableCell className="whitespace-nowrap">{n.landing ? <Badge variant="outline">{n.landing}</Badge> : <small>直接出去</small>}</TableCell>
              <TableCell className="whitespace-nowrap"><UdpBadge udp={n.udp} /></TableCell>
              <TableCell className="whitespace-nowrap">{gb(n.stats.traffic24h)}</TableCell>
              <TableCell className="whitespace-nowrap">{mbps(n.stats.p90)}</TableCell>
              <TableCell className="whitespace-nowrap"><small>{admin ? (n.stats.top[0] ? `${n.stats.top[0].user} · ${gb(n.stats.top[0].gb)}${n.stats.activeUsers > 1 ? ` 等 ${n.stats.activeUsers} 人` : ""}` : "—") : gb(n.stats.top.find((t) => t.user === "Tulip")?.gb ?? 0)}</small></TableCell>
              <TableCell><div className="stack-tight"><Badge variant="soft" tone={badgeTone(n.verdict.tone)}>{n.verdict.text}</Badge>{n.verdict.why && <small className="text-[var(--weir-ink-3)]">{n.verdict.why}</small>}</div></TableCell>
              <TableCell><div className="flex flex-nowrap items-center justify-end gap-[var(--weir-space-2)]">
                {n.verdict.fix ? <Button size="sm" onClick={() => toast(`${n.verdict.fix}（原型）`, { tone: "ok" })}>{n.verdict.fix}</Button> : <Button size="sm" variant="ghost" onClick={() => toast("实测已开始（原型）")}>实测</Button>}
                <Button size="sm" variant="ghost" onClick={() => setOpen(n)}>详情</Button>
              </div></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <NodeDialog node={open} close={() => setOpen(null)} />
    </>
  );
}

export function ProtoVps() {
  const toast = useToast();
  const [expanded, setExpanded] = useState<string | null>("1");
  const [quota, setQuota] = useState<ProtoGate | null>(null);
  return (
    <div className="stack">
      <PageHeader size="hero" eyebrow="节点" title={<>2 台 VPS 在线<br />3 件事要处理</>} description="方微云 Hysteria2 的 UDP 35138 没放行；方微云本月流量已用 81%；经「美东」的节点太慢。"
        actions={<><Button variant="outline" onClick={() => toast("打开新建节点（原型）")}><Plus />新建节点</Button><Button onClick={() => toast("打开添加 VPS（原型）")}><Server />添加 VPS</Button></>} />
      <ModuleTabs tabs={tabs} />
      <KernelService />
      <MetricStrip items={[{ label: "可用节点", value: "8 / 12" }, { label: "24 小时流量", value: "29.2", unit: "GB" }, { label: "你 → VPS", value: 43, unit: "ms", caption: "浏览器实测 · 方微云" }, { label: "国内误判走代理", value: "17.9", unit: "GB", caption: "7 天 · 可省" }]} />
      <FolderGrid expandedId={expanded} onExpandedChange={setExpanded} items={gates.map((g, i) => {
        const mine = nodes.filter((n) => n.source.kind === "gate" && n.source.gateId === g.id);
        return {
          id: String(g.id), label: g.name,
          folder: (open: boolean) => <GlassFolder expanded={open} controls={`proto-folder-${g.id}`} hue={(i % 2) as 0 | 1} name={g.name} address={g.host}
            status={g.health.tone === "warn" ? { tone: "warn", text: g.health.text } : { tone: "ok", text: "在线" }} cpu={g.cpu}
            caption={`${quotaText(g)} · ${mine.length} 个节点`} metric={g.browserMs} metricUnit="ms" metricLabel="你 → VPS"
            onClick={() => setExpanded(open ? null : String(g.id))}
            files={mine.slice(0, 3).map((n) => ({ id: String(n.id), title: n.protocol.split(" ")[0], meta: `${n.port}${n.network === "udp" ? "/udp" : ""}` }))} />,
          content: (
            <div className="stack">
              <SectionHeader title={`${g.name} · ${mine.length} 个协议`} description={`防火墙：${g.firewall} · 体检：${g.health.text}${g.health.why ? `（${g.health.why}）` : ""}`}
                actions={<><Button size="sm" variant="outline" onClick={() => toast("内核重启需要按住确认（原型）")}>重启内核</Button><Button size="sm" onClick={() => toast("新建节点，已预选这台 VPS（原型）")}><Plus />加协议</Button></>} />
              <Card size="sm">
                <div className="actions justify-between p-[var(--weir-space-4)]">
                  <span className="stack-tight" style={{ minWidth: 0, flex: 1 }}>
                    <strong>供应商流量 · {g.quota.mode}</strong>
                    {g.quota.totalGB == null ? <small className="text-[var(--weir-ink-3)]">还没填额度和重置日，只能看本周期用了 {gb(g.quota.usedGB)}</small> : <Meter value={g.quota.usedGB ?? 0} max={g.quota.totalGB} label={quotaText(g)} />}
                  </span>
                  <Button size="sm" variant="outline" onClick={() => setQuota(g)}>{g.quota.totalGB == null ? "设置额度" : "修改"}</Button>
                </div>
              </Card>
              <NodeRows list={mine} />
            </div>
          ),
        };
      })}>
        <FolderSlot title="添加 VPS" caption="用 SSH 一次装好" onClick={() => toast("打开添加 VPS（原型）")} />
      </FolderGrid>
      <InsightBanner laya={false} title="刚配对的 VPS · 203.0.113.24：要建哪些协议？" progress="Xray 与 sing-box 已就绪，ufw 已启用，建节点时 Gate 自动放行端口"
        action={<Button onClick={() => toast("建 VLESS + REALITY 与 Hysteria2（原型）", { tone: "ok" })}>建推荐的 2 个</Button>} />
      <QuotaDialog gate={quota} close={() => setQuota(null)} />
    </div>
  );
}

const groupOf = (n: ProtoNode) => (n.source.kind === "gate" ? `自建 · ${sourceLabel(n)}` : n.source.kind === "sub" ? `订阅源 · ${sourceLabel(n)}` : `用户链式 · ${n.source.owner}`);

export function ProtoAllNodes() {
  const toast = useToast();
  const [source, setSource] = useState("all");
  const shown = nodes.filter((n) => source === "all" || n.source.kind === source);
  const groups = [...new Set(shown.map(groupOf))];
  return (
    <div className="stack">
      <PageHeader eyebrow="节点" title="全部节点" description="中心拥有的所有节点：自建 VPS 上的、订阅源选用的、用户用自己的代理建的链式节点。"
        actions={<><Button variant="outline" onClick={() => toast("从订阅源挑节点（原型）")}>从订阅源选</Button><Button onClick={() => toast("新建节点（原型）")}><Plus />新建节点</Button></>} />
      <ModuleTabs tabs={tabs} />
      <div className="actions">
        <Button size="sm" variant="outline" onClick={() => toast("选中后批量分给用户（原型）")}>分给用户</Button>
        <span className="ml-auto" />
        <Segmented aria-label="来源" value={source} onValueChange={setSource} options={[["all", `全部 ${nodes.length}`], ["gate", "自建 7"], ["sub", "订阅源 2"], ["chain", "用户链式 3"]]} />
      </div>
      {groups.map((g) => (
        <Card key={g} className="overflow-hidden p-0">
          <div className="px-[var(--weir-space-4)] pt-[var(--weir-space-3)]"><small className="text-[var(--weir-ink-3)]">{g}</small></div>
          <NodeRows list={shown.filter((n) => groupOf(n) === g)} showSource />
        </Card>
      ))}
    </div>
  );
}

// 普通用户：我的节点 = 管理员分给我的 + 我用自己的代理建的链式节点（就是多出来的节点，不多占端口，入口换一份凭据）
function ChainNodeDialog({ open, close }: { open: boolean; close: () => void }) {
  const toast = useToast();
  const [entry, setEntry] = useState(1);
  const [proxy, setProxy] = useState("jk");
  const assigned = nodes.filter((n) => [1, 2, 3, 20].includes(n.id));
  const mine = proxies.filter((p) => p.owner === "Tulip");
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogHeader><DialogTitle>用代理新建节点</DialogTitle><DialogDescription>从入口节点进，再从你的代理出去。不多占端口：入口节点给你换一份凭据，流量只在入口计量一次。</DialogDescription></DialogHeader>
        <div className="stack">
          <Field label="入口节点" name="entry"><div className="actions">{assigned.map((n) => <Chip key={n.id} tone={lampTone(n.verdict.tone)} meta={nodeMeta(n)} selected={entry === n.id} onClick={() => setEntry(n.id)}>{nodeTitle(n)}</Chip>)}</div></Field>
          <Field label="从哪个代理出去" name="proxy" hint="代理在「我的代理」里添加和实测"><div className="actions">{mine.map((p) => <Chip key={p.id} tone={lampTone(p.probeTone)} meta={`${p.kind} · ${p.address}`} selected={proxy === p.id} onClick={() => setProxy(p.id)}>{p.name}</Chip>)}</div></Field>
          <Field label="名称" name="name" hint="订阅里显示的名字"><Input defaultValue={`${nodeTitle(assigned.find((n) => n.id === entry)!)} → ${mine.find((p) => p.id === proxy)?.name ?? ""}`} /></Field>
        </div>
        <DialogFooter><Button variant="ghost" onClick={close}>取消</Button><Button onClick={() => { toast("VPS 实测通过，节点已建好（原型）", { tone: "ok" }); close(); }}>实测并新建</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ProtoMyNodes() {
  const [chain, setChain] = useState(false);
  const assigned = nodes.filter((n) => [1, 2, 3, 20].includes(n.id));
  const mine = nodes.filter((n) => n.source.kind === "chain" && n.source.owner === "Tulip");
  return (
    <div className="stack">
      <PageHeader title="我的节点" description="管理员分给你的节点，加上你用自己的代理建的节点；在「我的订阅」里挑着用。" actions={<Button onClick={() => setChain(true)}><Plus />用代理新建节点</Button>} />
      <Card className="overflow-hidden p-0">
        <div className="px-[var(--weir-space-4)] pt-[var(--weir-space-3)]"><small className="text-[var(--weir-ink-3)]">分给我的 · {assigned.length}</small></div>
        <NodeRows list={assigned} showSource />
      </Card>
      <Card className="overflow-hidden p-0">
        <div className="px-[var(--weir-space-4)] pt-[var(--weir-space-3)]"><small className="text-[var(--weir-ink-3)]">我的链式节点 · {mine.length}</small></div>
        <NodeRows list={mine} showSource />
      </Card>
      <ChainNodeDialog open={chain} close={() => setChain(false)} />
    </div>
  );
}

export function ProtoKernels() {
  return (
    <div className="stack">
      <PageHeader eyebrow="线路" title="内核" description="沿用现有内核页（版本矩阵、驱动、订阅检查）。" />
      <KernelService />
      <Notice tone="info">原型不重画这一页。</Notice>
    </div>
  );
}

