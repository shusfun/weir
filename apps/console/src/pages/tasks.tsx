import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Notice, SearchInput, PageHeader, Pager,
  Button,
  Card,
  CardContent,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@migate/ui";
import { client } from "../lib/client";
import { date, labels, type Task } from "../lib/models";
import { State, Status } from "../components/feedback";

export function TasksPage({ deviceId }: { deviceId?: number }) {
  const [params] = useSearchParams();
  const [search, S] = useState(params.get("q") ?? "");
  const [status, F] = useState("all");
  const [device, D] = useState("all");
  const [page, P] = useState(1);
  const [detail, T] = useState<Task | null>(null);
  const query = new URLSearchParams({
    page: String(page),
    page_size: "20",
    q: search,
  });
  if (status !== "all") query.set("status", status);
  if (deviceId || device !== "all")
    query.set("device_id", String(deviceId ?? device));
  const tasks = useQuery({
    queryKey: ["task-directory", query.toString()],
    queryFn: () => client.tasks("?" + query),
    refetchInterval: 5000,
  });
  const devices = useQuery({ queryKey: ["devices"], queryFn: client.devices });
  const name = (id: number) =>
    devices.data?.items.find((d) => d.id === id)?.name || "Gate #" + id;
  const current = tasks.data?.items.find((t) => t.id === detail?.id) ?? detail;
  return (
    <div className="stack">
      <PageHeader title="执行记录与回执" description="设备暂离线时任务保持待执行；执行中超出租约会以同一任务 ID 自动重试。失败须修正配置后重新提交。" />
      <Card>
        <CardContent>
          <div className="table-toolbar">
            <SearchInput
              aria-label="搜索执行任务"
              placeholder="搜索操作名称或任务编号…"
              value={search}
              onChange={(e) => {
                S(e.target.value);
                P(1);
              }}
            />
            {!deviceId && (
              <Select
                value={device}
                onValueChange={(v) => {
                  D(String(v));
                  P(1);
                }}
              >
                <SelectTrigger className="min-w-[130px]" aria-label="筛选任务设备">
                  <SelectValue>
                    {device === "all" ? "全部设备" : name(Number(device))}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部设备</SelectItem>
                  {devices.data?.items.map((d) => (
                    <SelectItem key={d.id} value={String(d.id)}>
                      {d.name || "Gate #" + d.id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            <Select
              value={status}
              onValueChange={(v) => {
                F(String(v));
                P(1);
              }}
            >
              <SelectTrigger className="min-w-[130px]" aria-label="筛选任务状态">
                <SelectValue>
                  {status === "all" ? "全部状态" : labels[status]}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {[
                  ["all", "全部状态"],
                  ["pending", "待执行"],
                  ["running", "执行中"],
                  ["applied", "已生效"],
                  ["failed", "失败"],
                  ["expired", "已过期"],
                ].map(([key, title]) => (
                  <SelectItem key={key} value={key}>
                    {title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {tasks.isPending ? (
            <State loading />
          ) : tasks.error ? (
            <State error={tasks.error} onRetry={() => tasks.refetch()} />
          ) : !tasks.data?.items.length ? (
            <State empty="没有符合条件的任务" />
          ) : (
            tasks.data.items.map((t) => (
              <div className="list-row grant-row" key={t.id}>
                <div>
                  <strong>{t.operation}</strong>
                  <small>
                    #{t.id} · {name(t.device_id)} · {date(t.created_at)}
                  </small>
                  <small>
                    资源版本 {t.revision} · 执行 {t.attempts} 次
                  </small>
                </div>
                <div className="actions">
                  <Status value={t.status} />
                  <Button variant="outline" size="sm" onClick={() => T(t)}>
                    查看回执 #{t.id}
                  </Button>
                </div>
              </div>
            ))
          )}
          <Pager variant="inline" page={page} pageSize={20} total={tasks.data?.total} busy={tasks.isPending} onPage={P} />
        </CardContent>
      </Card>
      <Dialog open={!!detail} onOpenChange={(open) => !open && T(null)}>
        <DialogContent>
          <DialogTitle>任务 #{current?.id} 执行回执</DialogTitle>
          <DialogDescription>
            任务与设备审计通过编号关联。这里仅显示回执，不包含下发的凭据和私钥。
          </DialogDescription>
          {current && (
            <div className="stack">
              <Status value={current.status} />
              <div className="detail-grid">
                <div>
                  <span>设备</span>
                  <strong>{name(current.device_id)}</strong>
                </div>
                <div>
                  <span>操作</span>
                  <strong>{current.operation}</strong>
                </div>
                <div>
                  <span>资源版本 / 次数</span>
                  <strong>
                    {current.revision} / {current.attempts}
                  </strong>
                </div>
                <div>
                  <span>有效期至</span>
                  <strong>{date(current.expires_at)}</strong>
                </div>
                <div>
                  <span>执行租约至</span>
                  <strong>{date(current.leased_until)}</strong>
                </div>
              </div>
              <h3>设备返回结果</h3>
              <pre className="task-receipt">
                {JSON.stringify(
                  current.result ?? { message: "尚未收到设备回执" },
                  null,
                  2,
                )}
              </pre>
              {current.status === "failed" && (
                <Notice tone="bad">
                  配置未生效。请按返回原因修正对应资源，再次核对并下发；不能把失败任务当作成功配置。
                </Notice>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
