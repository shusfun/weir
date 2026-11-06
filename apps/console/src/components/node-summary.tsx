import type { DisplaySummary, ExitItem, NodeChoice } from "../lib/core";

export const nodeTitle = (node: NodeChoice) => node.display_name || node.name?.trim() || "地区未知";
export const exitTitle = (exit: Partial<ExitItem>) => exit.summary?.name || exit.remark?.trim() || "地区未知";

const kindText: Record<string, string> = { freedom: "直连", blackhole: "拦截", socks5: "SOCKS5", http: "HTTP", https: "HTTPS", link: "节点链接", node: "节点", source: "订阅节点" };

export function ExitSummary({ exit, summary, expanded = false }: { exit?: Partial<ExitItem>; summary?: DisplaySummary; expanded?: boolean }) {
  const s = summary ?? exit?.summary;
  const kind = s?.kind ?? exit?.kind;
  const name = s?.name || (exit ? exitTitle(exit) : "地区未知");
  if (kind === "freedom") return <span>直连</span>;
  if (kind === "blackhole") return <span>拦截</span>;
  return <span className="min-w-0 break-words">
    <span>{name}</span><span className="text-[var(--weir-ink-3)]"> · {kindText[kind ?? ""] ?? "出口未知"}</span>
    {!expanded && s?.region && s.region !== name && <span className="text-[var(--weir-ink-3)]"> · {s.region}</span>}
    {expanded && <small className="block text-[var(--weir-ink-3)]">
      {s?.region || "地区未知"}{exit?.probe?.ok && exit.probe.mode === "proxy" && exit.probe.exit_ip ? ` · ${exit.probe.exit_ip}` : ""}
      {exit?.probe?.at ? ` · 实测 ${new Date(exit.probe.at).toLocaleString()}` : " · 尚无有效实测"}
    </small>}
  </span>;
}

export function NodeSummary({ node, compact = false }: { node: NodeChoice; compact?: boolean }) {
  const protocol = [node.protocol?.toUpperCase(), node.network && node.network !== "tcp" ? node.network.toUpperCase() : "", node.security === "reality" ? "REALITY" : ""].filter(Boolean).join(" + ");
  return <span className="min-w-0 break-words">
    <span className="font-medium">{nodeTitle(node)}</span>
    <small className={(compact ? " " : "block ") + "text-[var(--weir-ink-3)]"}>
      {node.name && node.name !== nodeTitle(node) ? ` · 节点 ${node.name}` : ""}
      {[protocol, node.gate_name, node.host ? `${node.host.includes(":") ? `[${node.host}]` : node.host}${node.port ? `:${node.port}` : ""}` : node.port ? `:${node.port}` : ""].filter(Boolean).map((v, i) => <span key={i}> · {v}</span>)}
      {node.exit_summary && <span> · {node.exit_summary.kind === "freedom" ? "" : "链式 → "}<ExitSummary summary={node.exit_summary} /></span>}
    </small>
  </span>;
}
