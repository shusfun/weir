// VPS 页的节点表（设计稿 AVps 底部）：卡片里的表格，按「需要注意 / 正常」分组，
// 协议写成小标签，UDP 写实测结果，状态正常时只是一句灰字，有事才用状态标签。
import { Fragment, useState } from "react";
import { Badge, Card, Segmented, SectionHeader, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { nodeStatus, protocolNames, type Gate, type NodeItem } from "../lib/core";
import { rateText } from "../lib/linkmap";
import { nodeChainRoute } from "./node-summary";

type Filter = "all" | "chained" | "attention" | "tcp";

const securityNames: Record<string, string> = { reality: "REALITY", tls: "TLS" };

function udpOf(n: NodeItem): { text: string; pill?: boolean } {
  if (n.exit?.udp === "unsupported") return { text: "出口不支持", pill: true };
  const check = n.test?.udp?.status;
  if (check === "passed") return { text: "通过" };
  if (check === "failed") return { text: "未通过", pill: true };
  return { text: "未知" };
}

export function GateNodeTable({ gates, nodes, openNode }: { gates: Gate[]; nodes: NodeItem[]; openNode: (n: NodeItem) => void }) {
  const [filter, setFilter] = useState<Filter>("all");
  const rows = nodes.map((n) => {
    const g = gates.find((item) => item.id === n.gate_id);
    const [label, tone] = nodeStatus(n, g);
    const udp = udpOf(n);
    const chain = nodeChainRoute(n, nodes, gates);
    return { n, g, label, tone, udp, chain, attention: tone === "bad" || tone === "warn", tcpOnly: udp.text === "出口不支持" || udp.text === "不支持" };
  });
  const chained = rows.filter((r) => !!r.chain);
  const attention = rows.filter((r) => r.attention);
  const tcp = rows.filter((r) => r.tcpOnly);
  const shown = filter === "chained" ? chained : filter === "attention" ? attention : filter === "tcp" ? tcp : rows;
  const groups = [
    { key: "attention", title: "需要注意", list: shown.filter((r) => r.attention) },
    { key: "ok", title: "其他节点", list: shown.filter((r) => !r.attention) },
  ].filter((g) => g.list.length > 0);
  return (
    <>
      <SectionHeader flush
        className="mt-[var(--weir-gap-33)]"
        title="节点"
        actions={<Segmented aria-label="筛选节点" value={filter} onValueChange={setFilter} options={[["all", `全部 ${rows.length}`], ["chained", `链式 ${chained.length}`], ["attention", `需要注意 ${attention.length}`], ["tcp", `仅 TCP ${tcp.length}`]]} />}
      />
      <Card className="overflow-hidden p-0">
        <Table>
          <TableHeader>
            <TableRow><TableHead>节点</TableHead><TableHead>协议</TableHead><TableHead>出口走向</TableHead><TableHead>速率</TableHead><TableHead>UDP</TableHead><TableHead>状态</TableHead></TableRow>
          </TableHeader>
          <TableBody>
            {groups.length === 0 && <TableRow><TableCell colSpan={6} className="text-[var(--weir-ink-3)]">没有符合条件的节点</TableCell></TableRow>}
            {groups.map((group) => (
              <Fragment key={group.key}>
                <TableRow data-group-row="">
                  <TableCell colSpan={6}>{group.title} · {group.list.length}</TableCell>
                </TableRow>
                {group.list.map(({ n, g, label, tone, udp, chain }) => (
                  <TableRow key={n.id} onClick={() => openNode(n)}>
                    <TableCell>
                      <b>{n.name}</b>
                      <small className="block font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{g?.name ?? "未知"} · {n.port ? `${n.port}${n.network === "udp" ? "/udp" : ""}` : "未知"}</small>
                    </TableCell>
                    <TableCell>
                      <span className="inline-flex flex-wrap gap-[var(--weir-gap-5)]">
                        <Badge variant="tag">{protocolNames[n.protocol] ?? n.protocol}</Badge>
                        {securityNames[n.security] && <Badge variant="tag">{securityNames[n.security]}</Badge>}
                      </span>
                    </TableCell>
                    <TableCell>{chain ? <Badge variant="soft" tone="info">{chain.tag} ⇢ {chain.target}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">直连</span>}</TableCell>
                    <TableCell className="font-[family-name:var(--weir-mono)]">{n.rate?.minutes ? rateText(n.rate.p90_bps) : <span className="text-[var(--weir-ink-3)]">未知</span>}</TableCell>
                    <TableCell>{udp.pill ? <Badge variant="soft" tone="neutral" className="border-dashed bg-transparent">{udp.text}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">{udp.text}</span>}</TableCell>
                    <TableCell>
                      {tone === "bad" || tone === "warn" ? <Badge variant="soft" tone={tone}>{label}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">{label === "在线" ? "可用" : label}</span>}
                    </TableCell>
                  </TableRow>
                ))}
              </Fragment>
            ))}
          </TableBody>
        </Table>
      </Card>
    </>
  );
}
