// VPS 详情里的只读关系图：用户 → 入口节点 → 路由 → 出口。点击跳到对应资源，不在图上编辑。
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { Background, Position, ReactFlow, type Edge, type Node } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { api } from "../api";
import { State } from "./feedback";

interface TopoNode {
  id: string;
  kind: "user" | "node" | "route" | "exit";
  label: string;
  sub?: string;
  link?: string;
}

const columns = { user: 0, node: 1, route: 2, exit: 3 } as const;
const colWidth = 180;
const nodeWidth = 140;
const headings = ["用户", "入口节点", "路由", "出口"];
const tone: Record<TopoNode["kind"], string> = {
  user: "var(--weir-info)",
  node: "var(--weir-accent)",
  route: "var(--weir-warn)",
  exit: "var(--weir-ok)",
};

export function GateTopology({ gateId, openRouting }: { gateId: number; openRouting?: () => void }) {
  const navigate = useNavigate();
  const q = useQuery({ queryKey: ["topology", gateId], queryFn: () => api(`/admin/gates/${gateId}/topology`) as Promise<{ nodes: TopoNode[]; edges: { from: string; to: string }[] }> });
  const graph = useMemo(() => {
    const rows = [0, 0, 0, 0];
    const nodes: Node[] = (q.data?.nodes ?? []).map((n) => {
      const col = columns[n.kind];
      const y = rows[col]++ * 64 + 32;
      return {
        id: n.id,
        // 从左到右：用户 → 入口节点 → 路由 → 出口
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
        position: { x: col * colWidth, y },
        data: { label: <span className="block text-left text-xs leading-tight"><strong className="block truncate">{n.label}</strong>{n.sub && <small className="block truncate text-[var(--weir-ink-3)]">{n.sub}</small>}</span>, link: n.link },
        draggable: false,
        connectable: false,
        style: { width: nodeWidth, padding: "6px 8px", borderRadius: 8, borderColor: tone[n.kind], background: "var(--weir-panel)", color: "var(--weir-ink)", cursor: n.link ? "pointer" : "default" },
      };
    });
    // 列标题不是图上的对象：隐藏连接点（见 .topo-heading 样式）。
    const labels: Node[] = headings.map((h, i) => ({ id: "label-" + i, type: "output", position: { x: i * colWidth, y: 0 }, data: { label: h }, draggable: false, selectable: false, connectable: false,
      targetPosition: Position.Left, className: "topo-heading", style: { width: nodeWidth, border: "none", background: "transparent", boxShadow: "none", fontSize: 11, color: "var(--weir-ink-3)", padding: 0 } }));
    const edges: Edge[] = (q.data?.edges ?? []).map((e, i) => ({ id: "e" + i, source: e.from, target: e.to }));
    return { nodes: [...labels, ...nodes], edges, height: Math.max(...rows, 1) * 64 + 56 };
  }, [q.data]);
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />;
  if ((q.data?.nodes ?? []).length === 0) return <small className="text-[var(--weir-ink-3)]">这台 VPS 还没有节点。</small>;
  return (
    <div className="topology w-full rounded-[var(--weir-radius)] border border-[var(--weir-line)]" style={{ height: Math.min(Math.max(graph.height, 180), 420) }}>
      <div style={{ height: "100%" }}>
        <ReactFlow
          nodes={graph.nodes}
          edges={graph.edges}
          fitView
          fitViewOptions={{ padding: 0.08 }}
          minZoom={0.3}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable
          zoomOnScroll={false}
          panOnScroll
          proOptions={{ hideAttribution: true }}
          onNodeClick={(_, n) => {
            const link = (n.data as { link?: string }).link;
            if (link === "routing") openRouting?.();
            else if (link) navigate(link);
          }}
          aria-label="节点关系图"
        >
          <Background gap={16} size={1} />
        </ReactFlow>
      </div>
    </div>
  );
}
