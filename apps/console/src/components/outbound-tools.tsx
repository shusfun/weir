import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, ArrowDown, ListOrdered, Activity } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  Checkbox,
  DialogFooter,
} from "@migate/ui";
import type { Device, Resource, Task } from "../lib/models";
import { date } from "../lib/models";
import { api } from "../api";

type ProbeResult = {
  source: string;
  observed_at: string;
  items: { id: number; status: string; latency_ms?: number }[];
};
const statuses: Record<string, string> = {
  reachable: "TCP 可达",
  unreachable: "TCP 未连接",
  unknown: "未知",
  blocked: "安全限制：不探测私网目标",
  inventory_changed: "库存已变化，请重新探测",
};
export function OutboundTools({
  device,
  rows,
  tasks,
}: {
  device: Device;
  rows: Resource[];
  tasks: Task[];
}) {
  const [order, setOrder] = useState<Resource[] | null>(null),
    [probing, setProbing] = useState(false),
    [ids, setIDs] = useState<number[]>([]),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [probeID, setProbeID] = useState<number | null>(null);
  const cache = useQueryClient(),
    capable = (device.metadata.capabilities as string[] | undefined)?.includes(
      "outbound.tools.v1",
    );
  const manual = rows.filter((r) => r.data.source !== "subscription");
  const pending = tasks.some(
    (t) =>
      t.device_id === device.id &&
      ["pending", "running"].includes(t.status) &&
      /^(outbound|routing)\./.test(t.operation),
  );
  const observations = useQuery({
    queryKey: ["outbound-probe", device.id, probeID],
    enabled: probeID !== null,
    queryFn: () =>
      api(
        `/admin/tasks?device_id=${device.id}&operation=outbound.probe&page_size=20`,
      ) as Promise<{ items: Task[] }>,
    refetchInterval: (q) =>
      q.state.data?.items.some(
        (t) =>
          t.id === probeID &&
          ["applied", "failed", "expired"].includes(t.status),
      )
        ? false
        : 2000,
  });
  const task = observations.data?.items.find((t) => t.id === probeID),
    result =
      task?.status === "applied" ? (task.result as ProbeResult) : undefined;
  const [clock, setClock] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setClock(Date.now()), 30000);
    return () => clearInterval(timer);
  }, []);
  const stale =
    !result?.observed_at || clock - Date.parse(result.observed_at) > 300000;
  const name = (r: Resource) =>
    String(r.data.remark || r.data.tag || "未命名出站");
  async function submit(operation: string, payload: unknown) {
    setBusy(true);
    setError("");
    try {
      const t = await api(`/admin/devices/${device.id}/tasks`, "POST", {
        operation,
        confirm: true,
        payload,
      });
      if (operation === "outbound.probe") {
        setProbeID(t.id);
        setProbing(false);
      } else setOrder(null);
      setNotice(`任务 #${t.id} 已提交，等待设备回执。`);
      await cache.invalidateQueries({ queryKey: ["tasks"] });
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败，请重试");
    } finally {
      setBusy(false);
    }
  }
  function move(i: number, by: number) {
    const next = [...order!];
    [next[i], next[i + by]] = [next[i + by], next[i]];
    setOrder(next);
  }
  return (
    <div className="stack">
      <div className="row-actions">
        <Button
          variant="outline"
          disabled={!capable || pending || manual.length < 2}
          onClick={() => {
            setOrder([...manual]);
            setError("");
          }}
        >
          <ListOrdered size={15} /> 手动出站排序
        </Button>
        <Button
          variant="outline"
          disabled={!capable || pending || !rows.length}
          onClick={() => {
            setIDs(rows.slice(0, 50).map((r) => Number(r.data.id)));
            setError("");
            setProbing(true);
          }}
        >
          <Activity size={15} /> TCP 连通性探测
        </Button>
      </div>
      {!capable && (
        <p className="muted">
          等待支持出站工具的 Agent 心跳；旧设备不会收到新操作。
        </p>
      )}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {probeID && (
        <div className="notice">
          <strong>TCP 探测 #{probeID}</strong>
          <p>
            从 Gate
            连接已生效出口的公网端口。结果不代表协议握手、吞吐或代理可用性。
          </p>
          {observations.error ? (
            <p role="alert">
              读取失败，当前结果未知。
              <Button
                variant="outline"
                onClick={() => void observations.refetch()}
              >
                重新读取
              </Button>
            </p>
          ) : !task ? (
            <p>正在读取任务…</p>
          ) : task.status !== "applied" ? (
            <p>
              {["failed", "expired"].includes(task.status)
                ? "探测未完成，结果未知。"
                : "等待设备执行…"}
            </p>
          ) : (
            <>
              <p>采样 {date(result?.observed_at)} · 超过 5 分钟须重新探测</p>
              {result?.items.map((item) => (
                <p key={item.id}>
                  {rows.find((r) => Number(r.data.id) === item.id)
                    ? name(rows.find((r) => Number(r.data.id) === item.id)!)
                    : "已移除出站"}{" "}
                  ·{" "}
                  {stale
                    ? "未知（采样已过期）"
                    : statuses[item.status] || "未知"}
                  {!stale && item.status === "reachable"
                    ? ` · ${item.latency_ms} ms`
                    : ""}
                </p>
              ))}
            </>
          )}
        </div>
      )}
      <Dialog
        open={!!order || probing}
        onOpenChange={(v) => {
          if (!v && !busy) {
            setOrder(null);
            setProbing(false);
          }
        }}
      >
        <DialogContent className="business-dialog">
          <DialogTitle>
            {order ? "调整手动出站顺序" : "选择 TCP 探测出口"}
          </DialogTitle>
          <DialogDescription>
            {order
              ? "仅调整手动列表顺序，不修改路由优先级或连接凭据。完整快照冲突时拒绝覆盖；设备回执后更新。"
              : "最多 50 个已生效出口；私网和混合解析目标拒绝探测。不会修改配置或重启内核。"}
          </DialogDescription>
          <div className="stack">
            {order
              ? order.map((r, i) => (
                  <div className="selection-row" key={r.id}>
                    <span className="break-all">
                      {i + 1}. {name(r)}
                    </span>
                    <Button
                      variant="outline"
                      size="icon-sm"
                      aria-label={`上移 ${name(r)}`}
                      disabled={busy || i === 0}
                      onClick={() => move(i, -1)}
                    >
                      <ArrowUp size={14} />
                    </Button>
                    <Button
                      variant="outline"
                      size="icon-sm"
                      aria-label={`下移 ${name(r)}`}
                      disabled={busy || i === order.length - 1}
                      onClick={() => move(i, 1)}
                    >
                      <ArrowDown size={14} />
                    </Button>
                  </div>
                ))
              : rows.map((r) => (
                  <label className="pool-node" key={r.id}>
                    <Checkbox
                      aria-label={`探测 ${name(r)}`}
                      checked={ids.includes(Number(r.data.id))}
                      disabled={
                        busy ||
                        (!ids.includes(Number(r.data.id)) && ids.length >= 50)
                      }
                      onCheckedChange={(checked) =>
                        setIDs(
                          checked === true
                            ? [...ids, Number(r.data.id)]
                            : ids.filter((id) => id !== Number(r.data.id)),
                        )
                      }
                    />
                    <span>
                      <strong>{name(r)}</strong>
                      <small>
                        {String(r.data.address)}:{String(r.data.port)}
                      </small>
                    </span>
                  </label>
                ))}
          </div>
          {error && (
            <p role="alert" className="notice">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                setOrder(null);
                setProbing(false);
              }}
            >
              取消
            </Button>
            <Button
              disabled={busy || !capable || (!order && !ids.length)}
              onClick={() =>
                void submit(
                  order ? "outbound.reorder" : "outbound.probe",
                  order
                    ? {
                        items: order.map((r) => ({
                          id: r.data.id,
                          sort: r.data.sort ?? 0,
                        })),
                      }
                    : { ids },
                )
              }
            >
              {busy ? "提交中…" : order ? "确认提交排序任务" : "开始 TCP 探测"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
