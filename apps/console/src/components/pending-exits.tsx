import { ActionGroup, Badge, Button, InsetGroup, InsetRow, InsetRows, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useRowAction } from "../lib/action";
import { proxyURL } from "../lib/proxy-targets";
import { State } from "./feedback";
import { useConsoleContext } from "../lib/console-context";
import { useEffect } from "react";
import { guideRequest } from "../lib/guide-events";

export interface PendingExit {
  id: string;
  revision: number;
  kind: string;
  status: string;
  reason: string;
  trace_id: string;
  gate_id: number;
  node_id: number;
  exit_id: number;
}

const labels: Record<string, string> = { waiting: "等待验证", failed: "验证失败", stale: "配置已变化", applied: "已应用", discarded: "已放弃" };
const kinds: Record<string, string> = { "node.create": "新建节点", "node.update": "节点出口", "chain.create": "新建链式代理", "chain.update": "链式代理出口", "exit.save": "出口", "type.exit": "类型出口" };

export function PendingExits({ base = "/me", candidate, all = false, gateId, nodeId }: { base?: string; candidate?: string; all?: boolean; gateId?: number; nodeId?: number }) {
  const url = proxyURL(base, "/pending-exits");
  const list = useQuery({ queryKey: ["pending-exits", base, all], queryFn: async () => (await api(url + (all ? (url.includes("?") ? "&" : "?") + "all=true" : ""))).items as PendingExit[], refetchInterval: 5000 });
  const action = useRowAction<string>({ refetch: () => list.refetch(), invalidate: [["nodes"], ["chains"], ["exits"], ["proxy"], ["gates"]] });
  const confirm = useConfirm();
  const items = (list.data ?? []).filter((p) => (candidate ? p.id === candidate : !["applied", "discarded"].includes(p.status)) && (!gateId || p.gate_id === gateId) && (!nodeId || p.node_id === nodeId));
  useEffect(() => {
    for (const p of list.data ?? []) {
      if (p.kind === "node.create" && p.status !== "waiting") guideRequest(p.trace_id, "/admin/nodes", "POST", p.status === "applied" ? 200 : 409);
    }
  }, [list.data]);
  useConsoleContext({ notices: items.map((p) => `${kinds[p.kind] ?? "出口"}：${labels[p.status] ?? "未知"}，${p.reason}`) });
  if (list.data && !items.length) return null;
  return <InsetGroup title="出口验证任务" description="验证通过后自动应用。等待期间保留原出口，新资源不会进入订阅。">
    {(list.isPending || list.error) && <State loading={list.isPending} error={list.error} onRetry={() => list.refetch()} />}
    <InsetRows>{items.map((p) => <InsetRow key={p.id} className="flex flex-wrap items-center justify-between gap-3">
      <div className="min-w-0 flex-1 basis-[221px] space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <strong>{kinds[p.kind] ?? "出口"}</strong>
          <Badge variant="soft" tone={p.status === "failed" ? "bad" : p.status === "stale" ? "warn" : "info"}>{labels[p.status] ?? "未知"}</Badge>
          <code className="font-mono text-fine text-[var(--weir-ink-3)]" title={`任务 ${p.id}`}>#{p.id.slice(0, 8)}</code>
        </div>
        <div className="text-meta text-[var(--weir-ink-2)] break-words">{p.reason}</div>
      </div>
      {!["applied", "discarded"].includes(p.status) && <ActionGroup wrap={false}>
        <Button size="sm" variant="outline" disabled={action.isBusy() || p.status === "stale" || p.status === "waiting"} busy={action.isBusy(p.id)} busyText="正在重试" onClick={() => action.run(p.id, () => api(proxyURL(base, `/pending-exits/${p.id}/retry`), "POST", { revision: p.revision }))}>重试</Button>
        <Button size="sm" variant="outline" disabled={action.isBusy()} onClick={async () => {
          if (await confirm({ title: "放弃候选出口？", description: "当前生效配置保持不变。", confirmText: "放弃" })) await action.run(p.id, () => api(proxyURL(base, `/pending-exits/${p.id}`), "DELETE", { revision: p.revision }));
        }}>放弃</Button>
      </ActionGroup>}
    </InsetRow>)}</InsetRows>
  </InsetGroup>;
}
