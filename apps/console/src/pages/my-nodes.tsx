import { BrowserPathTest } from "../components/browser-test";
import { Link } from "react-router-dom";
import { Badge, Button, InsetGroup, InsetRow, InsetRows, Notice, PageHeader, StatusLamp } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { objectStatus, protocolNames } from "../lib/core";
import { useConsoleContext } from "../lib/console-context";
import type { CatalogNode } from "../components/node-catalog";
import { State } from "../components/feedback";
import { date } from "../lib/models";

const origins = { node: "自有 VPS", source: "订阅源", manual: "手动节点", chain: "本人链式" };
export function MyNodesPage() {
  const q = useQuery({ queryKey: ["my-node-catalog"], queryFn: () => api("/me/node-catalog") as Promise<{ items: CatalogNode[] }> });
  useConsoleContext({ page: "我的节点", list: { total: q.data?.items.length ?? 0, items: (q.data?.items ?? []).filter(item => item.kind !== "manual").slice(0, 20).map(item => ({ type: item.kind === "node" ? "node" : item.kind === "chain" ? "chain" : "source", id: item.kind === "source" ? item.source_id : item.id, name: item.name })) } });
  return <div className="stack">
    <PageHeader size="data" title="我的节点" description={q.data ? `${q.data.items.length} 个已授权节点` : "正在读取你的节点授权"} actions={<div className="flex flex-wrap gap-2"><Button variant="outline" render={<Link to="/proxy" />}>我的代理</Button><Button render={<Link to="/links" />}>我的订阅</Button></div>} />
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => void q.refetch()} /> : q.data.items.length === 0 ? <State empty="还没有获授权的节点" detail="请联系管理员分配节点，授权后可在订阅中选用。" /> : <InsetGroup data-tour="my-node-catalog"><InsetRows>
      {q.data.items.map(item => <InsetRow key={item.ref} className="flex items-start gap-[var(--weir-space-3)]">
        <StatusLamp tone={objectStatus(item.state)[1]} label={objectStatus(item.state)[0]} />
        <div className="stack-tight min-w-0"><strong>{item.name || origins[item.kind]}</strong><span><Badge variant="soft" tone="neutral">{origins[item.kind]} · {protocolNames[item.protocol] || item.protocol || "协议未知"}</Badge></span>{item.state.reason && <small>{item.state.reason}</small>}<small className="text-[var(--weir-ink-3)]">{[item.state.observed_at ? `${date(item.state.observed_at)} 更新` : null, item.chain_entry ? "可作为链式入口" : null].filter(Boolean).join(" · ")}</small>{item.kind === "node" && <BrowserPathTest nodeId={item.id} />}</div>
      </InsetRow>)}
    </InsetRows></InsetGroup>}
  </div>;
}
