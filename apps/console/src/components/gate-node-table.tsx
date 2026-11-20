// VPS 页的节点表：按「需要注意 / 正常」分组。每行先回答「有没有人在用」（近 7 天用量、用户数、最后上报），
// 协议并进名称下方的小字；UDP 只在出口不支持或实测失败时出现；状态正常时只是一句灰字，有事才用状态标签。
import { Fragment, useState } from "react";
import { Badge, Card, Segmented, SectionHeader, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { nodeStatus, protocolNames, type Gate, type NodeItem } from "../lib/core";
import { bytes, date } from "../lib/models";
import { nodeChainRoute } from "./node-summary";

type Filter = "all" | "chained" | "attention" | "tcp";

const securityNames: Record<string, string> = { reality: "REALITY", tls: "TLS" };

function udpOf(n: NodeItem): { text: string; pill?: boolean } {
  if (n.exit?.udp === "unsupported") return { text: "出口不支持", pill: true };
  const check = n.test?.udp?.status;
  if (check === "passed") return { text: "通过" };
  if (check === "failed") return { text: "未通过", pill: true };
  return { text: "" };
}

function UsageCell({ n }: { n: NodeItem }) {
  if (n.usage_error) return <span className="text-meta text-[var(--weir-warn-ink)]">{n.usage_error}</span>;
  if (!n.usage?.known) return <span className="text-meta text-[var(--weir-ink-3)]">尚无用量上报</span>;
  return <><span className="font-[family-name:var(--weir-mono)]">{bytes(Number(n.usage.bytes))}</span>
    <small className="block text-fine text-[var(--weir-ink-3)]">{n.usage.users} 人在用{n.usage.observed_at ? ` · ${date(n.usage.observed_at)}` : ""}</small></>;
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
            <TableRow><TableHead>节点</TableHead><TableHead>出口走向</TableHead><TableHead>近 7 天用量</TableHead><TableHead>状态</TableHead></TableRow>
          </TableHeader>
          <TableBody>
            {groups.length === 0 && <TableRow><TableCell colSpan={4} className="text-[var(--weir-ink-3)]">没有符合条件的节点</TableCell></TableRow>}
            {groups.map((group) => (
              <Fragment key={group.key}>
                <TableRow data-group-row="">
                  <TableCell colSpan={4}>{group.title} · {group.list.length}</TableCell>
                </TableRow>
                {group.list.map(({ n, label, tone, udp, chain }) => (
                  <TableRow key={n.id} onClick={() => openNode(n)}>
                    <TableCell>
                      <b>{n.name}</b>
                      <small className="block text-fine text-[var(--weir-ink-3)]">{[protocolNames[n.protocol] ?? n.protocol, securityNames[n.security], n.port ? `${n.port}${n.network === "udp" ? "/udp" : ""}` : null].filter(Boolean).join(" · ")}</small>
                    </TableCell>
                    <TableCell>{chain ? <Badge variant="soft" tone="info">经 {chain.target}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">直连</span>}</TableCell>
                    <TableCell><UsageCell n={n} /></TableCell>
                    <TableCell>
                      <span className="inline-flex flex-wrap items-center gap-[var(--weir-gap-5)]">
                        {tone === "bad" || tone === "warn" ? <Badge variant="soft" tone={tone}>{label}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">{label === "在线" ? "可用" : label}</span>}
                        {udp.pill && <Badge variant="soft" tone="neutral" className="border-dashed bg-transparent">UDP {udp.text}</Badge>}
                      </span>
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
