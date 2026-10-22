// 全局任务托盘：顶栏显示待执行 / 执行中的 Gate 任务，读取失败显示“未知”，不显示为 0。
import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { PageState, Button, Popover, PopoverTrigger, PopoverContent, StatusLamp, toneOf } from "@migate/ui";
import { ListChecks } from "lucide-react";
import { client } from "../lib/client";
import { date, labels } from "../lib/models";

export function TaskTray() {
  const [open, setOpen] = useState(false);
  const pending = useQuery({ queryKey: ["tray-tasks", "pending"], queryFn: () => client.tasks("?page=1&page_size=6&status=pending"), refetchInterval: 10000 });
  const running = useQuery({ queryKey: ["tray-tasks", "running"], queryFn: () => client.tasks("?page=1&page_size=6&status=running"), refetchInterval: 10000 });
  const devices = useQuery({ queryKey: ["devices"], queryFn: client.devices, enabled: open });
  const failed = pending.isError || running.isError;
  const loading = pending.isPending || running.isPending;
  const count = failed || loading ? null : (pending.data?.total ?? pending.data?.items.length ?? 0) + (running.data?.total ?? running.data?.items.length ?? 0);
  const items = [...(running.data?.items ?? []), ...(pending.data?.items ?? [])].slice(0, 8);
  const name = (id: number) => devices.data?.items.find((d) => d.id === id)?.name || "Gate #" + id;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button variant="ghost" size="sm" aria-label={count === null ? "进行中的任务：未知" : "进行中的任务：" + count}>
            <ListChecks />
            {count === null ? <span className="tray-count" style={{ background: "var(--mg-off)" }}>?</span> : count > 0 && <span className="tray-count">{count}</span>}
          </Button>
        }
      />
      <PopoverContent>
        <div className="tray-head">
          <strong>进行中的 Gate 任务</strong>
          <StatusLamp tone={failed ? "unknown" : count ? "info" : "ok"} label={failed ? "状态未知" : count ? "有任务执行" : "空闲"} />
        </div>
        {failed ? (
          <PageState size="sm" className="justify-center px-3.5" kind="error" title="任务状态读取失败，当前数量未知。" />
        ) : loading ? (
          <PageState size="sm" className="justify-center px-3.5" kind="loading" title="正在读取…" />
        ) : !items.length ? (
          <PageState size="sm" className="justify-center px-3.5" kind="empty" title="没有待执行或执行中的任务。" />
        ) : (
          <div className="tray-list">
            {items.map((t) => (
              <Link key={t.id} className="tray-item" to={"/devices/" + t.device_id + "/tasks"} onClick={() => setOpen(false)}>
                <StatusLamp tone={toneOf(t.status)} />
                <div>
                  <strong>{t.operation}</strong>
                  <small>#{t.id} · {name(t.device_id)} · {date(t.created_at)}</small>
                </div>
                <small>{labels[t.status] ?? t.status}</small>
              </Link>
            ))}
          </div>
        )}
        <div className="tray-foot">
          <Link to="/ops/tasks" onClick={() => setOpen(false)}>打开任务中心</Link>
        </div>
      </PopoverContent>
    </Popover>
  );
}
