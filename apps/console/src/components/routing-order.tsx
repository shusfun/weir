import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, ArrowDown, ListOrdered } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@migate/ui";
import type { Resource, Task } from "../lib/models";
import { client } from "../lib/client";
import { api } from "../api";

export function RoutingOrder({
  deviceId,
  rows,
  tasks,
  outbounds,
}: {
  deviceId: number;
  rows: Resource[];
  tasks: Task[];
  outbounds: Resource[];
}) {
  const [order, setOrder] = useState<Resource[] | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: client.devices,
    refetchInterval: 15000,
  });
  const cache = useQueryClient();
  const metadata = devices.data?.items.find((d) => d.id === deviceId)?.metadata;
  const capable =
    Array.isArray(metadata?.capabilities) &&
    metadata.capabilities.includes("routing.order.v1");
  const pending = tasks.some(
    (t) =>
      t.device_id === deviceId &&
      t.operation.startsWith("routing.") &&
      ["pending", "running"].includes(t.status),
  );
  const label = (r: Resource) =>
    String(r.data.domain || r.data.ip || r.data.protocol || "所有流量") +
    " → " +
    (outbounds.find((o) => o.data.id === r.data.outbound_id)?.data.tag ??
      "目标出站未知") +
    " · 规则 #" +
    r.data.id;
  function move(index: number, by: number) {
    const next = [...order!];
    [next[index], next[index + by]] = [next[index + by], next[index]];
    setOrder(next);
  }
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const result = await api(`/admin/devices/${deviceId}/tasks`, "POST", {
        operation: "routing.reorder",
        confirm: true,
        payload: {
          rules: order!.map((r) => ({ id: r.data.id, sort: r.data.sort ?? 0 })),
        },
      });
      setNotice(
        `排序任务 #${result.id} 已提交。列表保留已生效顺序，收到设备成功回执后才更新。`,
      );
      setOrder(null);
      await cache.invalidateQueries({ queryKey: ["tasks"] });
    } catch (e) {
      setError(e instanceof Error ? e.message : "提交失败");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="stack">
      <div className="section-heading">
        <div>
          <h3>匹配优先级</h3>
          <p>从上到下匹配，先命中的规则生效；一次任务更新全部规则顺序。</p>
        </div>
        <Button
          variant="outline"
          disabled={!capable || pending || rows.length < 2}
          onClick={() => {
            setOrder([...rows]);
            setError("");
            setNotice("");
          }}
        >
          <ListOrdered size={15} />
          调整顺序
        </Button>
      </div>
      {!capable && (
        <p className="muted">
          {devices.isError
            ? "暂时无法读取设备能力，请刷新后重试。"
            : "等待支持路由排序的 Agent 心跳，旧 Agent 不会收到排序指令。"}
        </p>
      )}
      {pending && <p role="status">路由任务仍在执行，完成后可继续调整顺序。</p>}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      <Dialog
        open={order !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setOrder(null);
        }}
      >
        <DialogContent className="business-dialog">
          <DialogTitle>调整路由匹配顺序</DialogTitle>
          <DialogDescription>
            上移提高优先级。仅更改顺序，不修改规则匹配条件与出站；列表变化时拒绝覆盖，需重新打开核对。
          </DialogDescription>
          <div className="stack">
            {order?.map((row, i) => (
              <div className="selection-row" key={row.id}>
                <span className="break-all">
                  {i + 1}. {label(row)}
                </span>
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label={"上移 " + label(row)}
                  disabled={busy || i === 0}
                  onClick={() => move(i, -1)}
                >
                  <ArrowUp size={14} />
                </Button>
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label={"下移 " + label(row)}
                  disabled={busy || i === order.length - 1}
                  onClick={() => move(i, 1)}
                >
                  <ArrowDown size={14} />
                </Button>
              </div>
            ))}
          </div>
          {error && (
            <p role="alert" className="notice error-message">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => setOrder(null)}
            >
              取消
            </Button>
            <Button disabled={busy || !capable || pending} onClick={submit}>
              {busy ? "提交中…" : "确认提交排序任务"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
