import "@xyflow/react/dist/style.css";
import { useMemo, useState, useEffect } from "react";
import {
  ReactFlow,
  Background,
  MiniMap,
  Handle,
  Position,
  MarkerType,
  useNodesState,
  type Node,
  type NodeProps,
  type ReactFlowInstance,
} from "@xyflow/react";
import { ZoomIn, ZoomOut, Maximize, Network } from "lucide-react";
import {
  Button,
  Badge,
  Card,
  CardContent,
  Label,
  Input,
  Switch,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import type { Resource, Grant } from "../lib/models";
import type { Inbound, Outbound, RoutingRule } from "../lib/topology-model";
import {
  buildTopologyGraph,
  type TopologyNodeData,
} from "../lib/topology-graph";
import { State } from "./feedback";
import { topologyView } from "../lib/topology-view";
import { Metric } from "./metric";

// MiGate 0998448 graph adaptation. UIAble controls, React Flow canvas.
function TopologyNode({ data }: NodeProps<Node<TopologyNodeData>>) {
  return (
    <Card
      className={
        "gate-graph-node" +
        (data.missing || data.invalid ? " graph-invalid" : "")
      }
    >
      <Handle type="target" position={Position.Left} />
      <Handle type="source" position={Position.Right} />
      <CardContent>
        <div className="section-heading">
          <strong title={data.title}>{data.title}</strong>
          <Badge variant="outline">
            {data.missing
              ? "缺失"
              : data.kind === "client"
                ? "授权记录"
                : data.enabled
                  ? "配置启用"
                  : "配置停用"}
          </Badge>
        </div>
        {data.subtitle && <small title={data.subtitle}>{data.subtitle}</small>}
        <dl>
          {data.meta.map((m) => (
            <div key={m.label}>
              <dt>{m.label}</dt>
              <dd title={m.value}>{m.value}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}
const nodeTypes = { topologyNode: TopologyNode };
const kinds: Record<string, string> = {
  all: "全部资源",
  inbound: "入站",
  client: "客户端授权",
  outbound: "出站",
};
export function GateTopology({
  resources,
  grants,
  go,
}: {
  resources: Record<string, Resource[]>;
  grants: Grant[];
  go: (tab: string) => void;
}) {
  const [flow, F] = useState<ReactFlowInstance<Node<TopologyNodeData>> | null>(
      null,
    ),
    [kind, K] = useState("all"),
    [expanded, X] = useState(false),
    [clientSearch, Q] = useState(""),
    [layout, L] = useState("columns"),
    [selected, S] = useState<string | null>(null);
  const graph = useMemo(() => {
    const baseCount =
      (resources["device-inbound"] ?? []).length +
      (resources["device-outbound"] ?? []).length +
      grants.filter((g) => g.status === "applied").length +
      1;
    if (baseCount > 1000)
      return { nodes: [], edges: [], limit: { nodes: baseCount, edges: 0 } };

    const inbounds: Inbound[] = (resources["device-inbound"] ?? []).map(
      (r) => ({
        id: Number(r.data.id),
        remark: String(r.data.remark ?? ""),
        protocol: String(r.data.protocol),
        port: Number(r.data.port),
        core: String(r.data.core ?? ""),
        network: String(r.data.network ?? "tcp"),
        security: String(r.data.security ?? "none"),
        enabled: r.data.enabled !== false,
        clients: grants
          .filter(
            (g) => g.inbound_id === Number(r.data.id) && g.status === "applied",
          )
          .map((g) => ({
            id: g.id,
            email: `${g.user_email || "未知用户"} · 授权 #${g.id}`,
            enabled: g.enabled,
          })),
      }),
    );
    const outbounds: Outbound[] = [
      { id: 0, tag: "direct", protocol: "freedom", enabled: true },
      ...(resources["device-outbound"] ?? []).map((r) => ({
        id: Number(r.data.id),
        tag: String(r.data.tag) + " #" + r.data.id,
        protocol: String(r.data.protocol),
        address: String(r.data.address ?? ""),
        port: Number(r.data.port ?? 0),
        enabled: r.data.enabled !== false,
      })),
    ];
    const rules: RoutingRule[] = (resources["device-routing"] ?? []).map(
      (r) => ({
        id: Number(r.data.id),
        inbound_id: Number(r.data.inbound_id ?? 0),
        client_id: Number(r.data.client_id ?? 0),
        outbound_id: Number(r.data.outbound_id),
        enabled: r.data.enabled !== false,
      }),
    );
    const edgeEstimate =
      rules.reduce(
        (sum, r) => sum + (r.client_id || r.inbound_id ? 1 : inbounds.length),
        0,
      ) +
      inbounds.length +
      grants.length * 2;
    const nodeEstimate =
      inbounds.length +
      outbounds.length +
      inbounds.reduce((sum, i) => sum + (i.clients?.length ?? 0), 0);
    if (nodeEstimate > 1000 || edgeEstimate > 3000)
      return {
        nodes: [],
        edges: [],
        limit: { nodes: nodeEstimate, edges: edgeEstimate },
      };
    return { ...buildTopologyGraph(inbounds, outbounds, rules), limit: null };
  }, [resources, grants]);
  const fullCount = graph.nodes.length,
    tooLarge = !!graph.limit || fullCount > 1000 || graph.edges.length > 3000;
  const shown = useMemo(() => {
    if (tooLarge) return { nodes: [], edges: [] };
    const visible = topologyView(graph, expanded, clientSearch);
    const matches = visible.nodes.filter(
      (n) =>
        kind === "all" ||
        n.data.kind === kind ||
        n.data.kind === `missing-${kind}`,
    );
    const ids = new Set(matches.map((n) => n.id));
    const direct = new Set(ids);
    for (const e of visible.edges)
      if (direct.has(e.source) || direct.has(e.target)) {
        ids.add(e.source);
        ids.add(e.target);
      }
    const columns: Record<string, number> = {
      inbound: 0,
      "missing-inbound": 0,
      client: 1,
      "missing-client": 1,
      outbound: 2,
      "missing-outbound": 2,
    };
    const counts = [0, 0, 0];
    const nodes = visible.nodes
      .filter((n) => ids.has(n.id))
      .map((n) => {
        const col = columns[n.data.kind] ?? 0,
          index = counts[col]++;
        return {
          ...n,
          position:
            layout === "columns"
              ? { x: col * 380, y: index * 205 }
              : { x: index * 315, y: col * 260 },
          ariaLabel: `${kinds[n.data.kind] ?? "缺失资源"}：${n.data.title}`,
          style: { width: 270 },
          draggable: false,
        };
      });
    const edges = visible.edges
      .filter((e) => ids.has(e.source) && ids.has(e.target))
      .map((e) => ({
        ...e,
        animated: false,
        style: { ...e.style, strokeWidth: 2 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: e.data?.enabled ? "#0f766e" : "#94a3b8",
        },
      }));
    return { nodes, edges };
  }, [graph, kind, layout, tooLarge, expanded, clientSearch]);
  const [renderNodes, setRenderNodes, onNodesChange] = useNodesState<
    Node<TopologyNodeData>
  >([]);
  useEffect(() => setRenderNodes(shown.nodes), [shown.nodes, setRenderNodes]);
  // Unchanged refetches must preserve pan and zoom.
  const viewKey = shown.nodes.map((n) => n.id).join(",") + kind + layout;
  useEffect(() => {
    if (flow) {
      const frame = requestAnimationFrame(
        () => void flow.fitView({ padding: 0.18, duration: 0 }),
      );
      return () => cancelAnimationFrame(frame);
    }
  }, [flow, viewKey]);
  const detail = shown.nodes.find((n) => n.id === selected);
  return (
    <div className="stack">
      <div className="section-heading">
        <div>
          <h2>链路画布</h2>
          <p>
            已回执的配置关系，不代表实时命中。默认直连仅表示未命中规则时的回退路径。
          </p>
        </div>
        <Network size={22} />
      </div>
      <div className="metric-grid graph-metrics">
        <Metric
          title="入站"
          value={String(
            graph.nodes.filter((n) => n.data.kind === "inbound").length,
          )}
        />
        <Metric
          title="客户端授权"
          value={String(
            graph.nodes.filter((n) => n.data.kind === "client").length,
          )}
        />
        <Metric
          title="配置出站"
          value={String((resources["device-outbound"] ?? []).length)}
        />
        <Metric
          title="缺失引用"
          value={String(graph.nodes.filter((n) => n.data.missing).length)}
        />
      </div>
      <Card>
        <CardContent className="stack">
          <div className="graph-toolbar">
            <div className="field">
              <Label htmlFor="graph-client-search">搜索客户端授权</Label>
              <Input
                id="graph-client-search"
                placeholder="邮箱或授权名称"
                value={clientSearch}
                onChange={(e) => Q(e.target.value)}
              />
            </div>
            <div className="actions">
              <Switch
                id="graph-expand"
                checked={expanded}
                onCheckedChange={X}
              />
              <Label htmlFor="graph-expand">展开所有客户端</Label>
            </div>
          </div>
          <p className="muted">
            超过 12
            条授权时折叠未被显式路由引用的记录；数量完整保留。搜索邮箱可定位具体授权，展开后可移动与缩放查看。
          </p>
          <div className="graph-toolbar">
            <div className="field">
              <Label>查看资源</Label>
              <Select value={kind} onValueChange={(v) => v && K(v)}>
                <SelectTrigger aria-label="拓扑资源筛选">
                  <SelectValue>{kinds[kind]}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(kinds).map(([v, t]) => (
                    <SelectItem key={v} value={v}>
                      {t}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="field">
              <Label>布局</Label>
              <Select value={layout} onValueChange={(v) => v && L(v)}>
                <SelectTrigger aria-label="拓扑布局">
                  <SelectValue>
                    {layout === "columns" ? "从左向右" : "从上向下"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="columns">从左向右</SelectItem>
                  <SelectItem value="rows">从上向下</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="actions">
              <Button
                variant="outline"
                aria-label="放大拓扑"
                onClick={() => void flow?.zoomIn()}
              >
                <ZoomIn size={17} />
              </Button>
              <Button
                variant="outline"
                aria-label="缩小拓扑"
                onClick={() => void flow?.zoomOut()}
              >
                <ZoomOut size={17} />
              </Button>
              <Button
                variant="outline"
                onClick={() => void flow?.fitView({ padding: 0.18 })}
              >
                <Maximize size={16} />
                适应画布
              </Button>
            </div>
          </div>
          {tooLarge ? (
            <State
              empty={`当前规模估计 ${graph.limit?.nodes ?? fullCount} 个节点 / ${graph.limit?.edges ?? graph.edges.length} 条关系，超过画布展示上限（1000 / 3000）。请从业务列表查看；未截断数据冒充完整拓扑。`}
            />
          ) : !graph.nodes.some((n) => n.data.kind === "inbound") ? (
            <State empty="配置入站后显示关系" />
          ) : (
            <div
              className="gate-graph"
              aria-label="Gate 配置关系画布"
              onKeyDownCapture={(event) => {
                if (event.key !== "Enter" && event.key !== " ") return;
                const element = (
                  event.target as HTMLElement
                ).closest<HTMLElement>(".react-flow__node[data-id]");
                if (element?.dataset.id) {
                  event.preventDefault();
                  S(element.dataset.id);
                }
              }}
            >
              <ReactFlow
                nodes={renderNodes}
                onNodesChange={onNodesChange}
                edges={shown.edges}
                nodeTypes={nodeTypes}
                onInit={F}
                onNodeClick={(_, n) => S(n.id)}
                onPaneClick={() => S(null)}
                nodesConnectable={false}
                nodesDraggable={false}
                edgesReconnectable={false}
                elementsSelectable
                deleteKeyCode={null}
                minZoom={0.08}
                maxZoom={1.7}
                fitView
                onlyRenderVisibleElements
              >
                <Background gap={20} />
                <MiniMap
                  style={{ width: 96, height: 64 }}
                  pannable
                  zoomable
                  nodeColor="#9c92d4"
                  nodeStrokeColor="#6e61ad"
                  maskColor="rgba(226,232,240,0.55)"
                  ariaLabel="拓扑缩略图"
                />
              </ReactFlow>
            </div>
          )}
          <p className="muted">
            实线：规则候选关系；灰虚线：附属 /
            默认直连。停用关系保持可见；上下行缺少采样时显示未知。支持触摸移动和缩略图缩放。
          </p>
          {detail && (
            <div className="notice" role="status">
              <strong>{detail.data.title}</strong>
              <p>{detail.data.subtitle}</p>
              <Button
                variant="outline"
                onClick={() =>
                  go(
                    detail.data.kind.includes("client")
                      ? "grants"
                      : detail.data.kind.includes("inbound")
                        ? "inbounds"
                        : "outbounds",
                  )
                }
              >
                查看对应资源
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
