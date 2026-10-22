import type { TopologyGraph, TopologyNodeData } from "./topology-graph";
import type { Node } from "@xyflow/react";

// Fold only unreferenced client detail. Explicit client rules and missing
// references always remain visible, with honest counts for the hidden records.
export function topologyView(
  graph: TopologyGraph,
  expanded: boolean,
  search: string,
): TopologyGraph {
  const clients = graph.nodes.filter((n) => n.data.kind === "client");
  const query = search.trim().toLowerCase();
  if (!query && (expanded || clients.length <= 12)) return graph;
  const explicit = new Set(
    graph.edges
      .filter((e) => e.data?.kind === "client-routing")
      .map((e) => e.source),
  );
  const hidden = new Set(
    clients
      .filter((n) =>
        query
          ? !n.data.title.toLowerCase().includes(query)
          : !explicit.has(n.id),
      )
      .map((n) => n.id),
  );
  const nodes = graph.nodes.filter((n) => !hidden.has(n.id));
  const edges = graph.edges.filter(
    (e) => !hidden.has(e.source) && !hidden.has(e.target),
  );
  if (query) return { nodes, edges };
  const byInbound = new Map<string, Set<string>>();
  for (const edge of graph.edges)
    if (edge.data?.kind === "client-inherits" && hidden.has(edge.target)) {
      const ids = byInbound.get(edge.source) ?? new Set<string>();
      ids.add(edge.target);
      byInbound.set(edge.source, ids);
    }
  for (const [source, ids] of byInbound) {
    const id = `client-summary:${source}`;
    nodes.push({
      id,
      type: "topologyNode",
      position: { x: 380, y: 0 },
      data: {
        kind: "client",
        title: `${ids.size} 条授权已折叠`,
        subtitle: "搜索邮箱定位，或展开全部",
        enabled: false,
        meta: [
          { label: "数量", value: String(ids.size) },
          { label: "类型", value: "汇总记录" },
          { label: "上下行", value: "未知" },
        ],
      },
    } as Node<TopologyNodeData>);
    edges.push({
      id: `summary:${source}`,
      source,
      target: id,
      type: "smoothstep",
      animated: false,
      data: { kind: "client-inherits", label: "折叠授权记录", enabled: false },
      style: { stroke: "var(--mg-edge)", strokeDasharray: "4 4" },
    });
  }
  return { nodes, edges };
}
