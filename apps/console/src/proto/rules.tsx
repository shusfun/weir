// 原型 · 规则：谁走哪个代理。规则按已有数据慢慢调：每条规则带实际流量与速率；
// 重点是「国内网站被误判走代理」——Gate 只统计目标在国内却经代理出去的流量，按域名聚合（不记 URL），确认后写进订阅的直连规则。
import { useState } from "react";
import { Plus } from "lucide-react";
import { Badge, Button, Card, CardContent, CardHeader, Checkbox, LaneMap, MetricStrip, Notice, PageHeader, SectionHeader, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast } from "@weir/ui";
import { misjudged, proxies, rules } from "./data";
import { UdpBadge } from "./parts";

export function ProtoRules() {
  const toast = useToast();
  const [picked, setPicked] = useState(misjudged.filter((m) => m.gb >= 1).map((m) => m.id));
  const [added, setAdded] = useState<string[]>([]);
  const left = misjudged.filter((m) => !added.includes(m.id));
  const saved = left.filter((m) => picked.includes(m.id)).reduce((a, m) => a + m.gb, 0);
  return (
    <div className="stack">
      <PageHeader eyebrow="线路" title="规则" description="谁走哪个代理，按实际数据慢慢调。客户端的分流规则随订阅下发；服务端按节点和网站选代理。"
        actions={<Button onClick={() => toast("添加规则（原型）")}><Plus />添加规则</Button>} />
      <MetricStrip items={[{ label: "国内误判走代理", value: "17.9", unit: "GB", caption: "最近 7 天" }, { label: "涉及域名", value: misjudged.length }, { label: "最费流量的 VPS", value: "方微云", caption: "本月已用 81%" }]} />
      <Card>
        <CardHeader title="疑似误判：国内网站走了代理" description="Gate 只统计目标在国内、却经代理出去的流量，按域名聚合，不记具体网址。加进直连后，客户端下次更新订阅生效。" />
        <CardContent className="stack">
          <Table>
            <TableHeader><TableRow><TableHead className="w-11"><span className="sr-only">选择</span></TableHead><TableHead>域名</TableHead><TableHead>7 天流量</TableHead><TableHead>涉及用户</TableHead><TableHead>主要经过</TableHead><TableHead>目标</TableHead></TableRow></TableHeader>
            <TableBody>
              {left.map((m) => (
                <TableRow key={m.id}>
                  <TableCell><Checkbox aria-label={`选择 ${m.domain}`} checked={picked.includes(m.id)} onCheckedChange={(c) => setPicked(c ? [...picked, m.id] : picked.filter((x) => x !== m.id))} /></TableCell>
                  <TableCell><strong>{m.domain}</strong></TableCell>
                  <TableCell>{m.gb} GB</TableCell>
                  <TableCell>{m.users}</TableCell>
                  <TableCell><small>{m.via}</small></TableCell>
                  <TableCell><Badge variant="soft">{m.ip}</Badge></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {left.length === 0 ? <Notice tone="ok">都加进直连了。7 天后可以在这里看这些域名的代理流量有没有降下来。</Notice> : (
            <div className="actions">
              <small className="text-[var(--weir-ink-2)]">已选 {picked.filter((id) => left.some((m) => m.id === id)).length} 个，预计每周少走代理约 {saved.toFixed(1)} GB</small>
              <span className="ml-auto" />
              <Button variant="ghost" onClick={() => toast("这些域名不再提示（原型）")}>忽略</Button>
              <Button onClick={() => { setAdded([...added, ...picked]); toast("已加进订阅的直连规则（原型）", { tone: "ok", description: "客户端下次更新订阅时生效。" }); }}>加进直连规则</Button>
            </div>
          )}
        </CardContent>
      </Card>
      <SectionHeader title="现在怎么走" description="从上到下匹配；配了代理的节点流量只走这条链。每条规则后面是实际流量和速率。" />
      <LaneMap label="当前流向" lanes={rules.map((r) => ({ id: r.id, from: r.lane, to: r.to }))} />
      <Card className="overflow-hidden p-0">
        <Table>
          <TableHeader><TableRow><TableHead>#</TableHead><TableHead>按</TableHead><TableHead>匹配</TableHead><TableHead>走哪个代理</TableHead><TableHead>7 天流量</TableHead><TableHead>速率 P90</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>
            {rules.map((r, i) => {
              const p = proxies.find((x) => x.name === r.to);
              const slow = r.p90 === "0.8 MB/s";
              return (
                <TableRow key={r.id}>
                  <TableCell>{i + 1}</TableCell>
                  <TableCell><Badge variant="soft">{r.kind}</Badge></TableCell>
                  <TableCell>{r.match}</TableCell>
                  <TableCell className="whitespace-nowrap"><span className="flex flex-nowrap items-center gap-[var(--weir-space-2)]"><strong>{r.to}</strong>{p && <UdpBadge udp={p.udp} />}</span></TableCell>
                  <TableCell className="whitespace-nowrap">{r.traffic}</TableCell>
                  <TableCell className="whitespace-nowrap">{slow ? <Badge variant="soft" tone="warn">{r.p90} · 直连 12.6</Badge> : r.p90}</TableCell>
                  <TableCell><div className="flex flex-nowrap items-center justify-end gap-[var(--weir-space-2)]">{slow && <Button size="sm" onClick={() => toast("改成直接出去（原型）")}>改直连</Button>}<Button size="sm" variant="ghost" onClick={() => toast("编辑规则（原型）")}>编辑</Button></div></TableCell>
                </TableRow>
              );
            })}
            <TableRow><TableCell>·</TableCell><TableCell><Badge variant="soft">其余</Badge></TableCell><TableCell>没匹配到的流量</TableCell><TableCell><strong>直接出去</strong></TableCell><TableCell>—</TableCell><TableCell>—</TableCell><TableCell className="text-right"><small className="text-[var(--weir-ink-3)]">固定</small></TableCell></TableRow>
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
