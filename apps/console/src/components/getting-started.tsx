// GettingStarted：从零到客户端用上的路径。完成与否按现有数据判断，不另存状态；全部完成后不再显示。
import type { ReactNode } from "react";
import { useQuery } from "../lib/query";
import { Button, Card, CardContent, StatusLamp } from "@weir/ui";
import { Link } from "react-router-dom";
import { api } from "../api";
import type { Gate, LinkStats, NodeItem } from "../lib/core";
import { StartGuideButton } from "./guide";

export function GettingStarted({ gates, nodes, addGate, addNode }: { gates: Gate[]; nodes: NodeItem[]; addGate: () => void; addNode: () => void }) {
  const stats = useQuery({ queryKey: ["link-stats", true, 0], queryFn: async () => (await api("/admin/link-stats")).items as LinkStats[] });
  const links = stats.data ?? [];
  const steps: { label: string; detail: string; done: boolean; action?: ReactNode }[] = [
    { label: "添加 VPS", detail: "填 SSH 登录信息一次装好，或复制安装命令。", done: gates.some((g) => g.status === "active"), action: <Button size="sm" onClick={addGate}>添加 VPS</Button> },
    { label: "建一个节点", detail: "选协议就行，保存前由 VPS 实测。", done: nodes.length > 0, action: <Button size="sm" onClick={addNode}>新建节点</Button> },
    { label: "拿到订阅链接", detail: "在订阅链接页新建；每个用户可以有多条。", done: links.length > 0, action: <Button size="sm" render={<Link to="/links" />}>打开订阅链接</Button> },
    { label: "客户端取得配置", detail: "复制订阅地址导入并更新；连接是否正常需在客户端验证。", done: links.some((l) => l.pulls_30d > 0), action: <Button size="sm" variant="outline" render={<Link to="/links" />}>查看订阅地址</Button> },
  ];
  // 统计还没读到时不显示：不能把「未知」当成没做。
  if (stats.isPending || stats.error || steps.every((s) => s.done)) return null;
  const current = steps.findIndex((s) => !s.done);
  return (
    <Card aria-label="开始使用">
      <CardContent className="stack">
        <strong className="text-body">开始使用 · {steps.filter((s) => s.done).length} / {steps.length}</strong>
        <StartGuideButton />
        <ol className="m-0 grid list-none items-start gap-3 p-0 md:grid-cols-4">
          {steps.map((s, i) => (
            <li key={s.label} className="stack content-start justify-start" aria-current={i === current ? "step" : undefined}>
              <span className="flex items-center gap-2 text-body">
                <StatusLamp tone={s.done ? "ok" : i === current ? "info" : "off"} />
                <strong className={s.done || i === current ? "" : "text-[var(--weir-ink-3)]"}>{s.label}</strong>
              </span>
              <small className="text-[var(--weir-ink-3)]">{s.done ? "已完成" : s.detail}</small>
              {i === current && <div>{s.action}</div>}
            </li>
          ))}
        </ol>
      </CardContent>
    </Card>
  );
}
