// 管理员通知：中心按当前状态算出要处理的问题（缺内核、版本没有驱动、有新版本、磁盘不足等），
// 每条带处理按钮，可以忽略；问题解决后自动消失。只给管理员看。
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { useNavigate } from "react-router-dom";
import { Badge, Button, Popover, PopoverContent, PopoverTrigger, ScrollArea } from "@weir/ui";
import { Bell } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { DraftsPending, useDrafts } from "./draft-confirm";

interface Notice {
  id: string;
  level: "warn" | "info";
  title: string;
  detail?: string;
  action?: { kind: "fetch_kernel" | "open" | "update"; label: string; kernel?: string; version?: string; to?: string };
  extra?: { reason?: string };
}

export function NoticesButton() {
  const q = useQuery({ queryKey: ["notices"], queryFn: () => api("/admin/notices") as Promise<{ notices: Notice[]; dismissed: number }>, refetchInterval: 60000 });
  const [open, setOpen] = useState(false);
  const nav = useNavigate();
  const client = useQueryClient();
  const { run, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const list = q.data?.notices ?? [];
  const drafts = useDrafts().data?.length ?? 0;
  const count = list.length + drafts;
  const warn = drafts > 0 || list.some((n) => n.level === "warn");
  const act = (n: Notice) => {
    const a = n.action!;
    if (a.kind === "fetch_kernel")
      return run(n.id, async () => {
        await api(`/admin/kernels/${a.kernel}/fetch`, "POST", { version: a.version });
        await client.invalidateQueries({ queryKey: ["jobs"] });
        setOpen(false);
        nav("/kernels");
      }, `开始下载 ${a.kernel} ${a.version}`);
    setOpen(false);
    nav(a.kind === "update" ? "/system?tab=update" : a.to ?? "/");
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<Button variant="ghost" size="icon-sm" className="relative" aria-label={count ? `通知：${count} 条待处理` : "通知：没有待处理"} />}>
        <Bell />
        {count > 0 && <span className={"absolute -top-0.5 -right-0.5 min-w-4 rounded-full px-1 text-center text-fine font-semibold text-[var(--weir-badge-ink)] " + (warn ? "bg-[var(--weir-badge)]" : "bg-[var(--weir-accent)]")}>{count}</span>}
      </PopoverTrigger>
      <PopoverContent className="w-[min(92vw,380px)] p-0">
        <div className="border-b border-[var(--weir-line)] px-3 py-2 text-body font-semibold">待处理{q.data?.dismissed ? <small className="ml-2 font-normal text-[var(--weir-ink-3)]">另有 {q.data.dismissed} 条已忽略</small> : null}</div>
        <DraftsPending onOpen={() => setOpen(false)} />
        {q.error ? <p className="m-0 p-3 text-body text-[var(--weir-bad-ink)]">通知读取失败，稍后重试。</p> : list.length === 0 ? (drafts > 0 ? null : <p className="m-0 p-3 text-body text-[var(--weir-ink-3)]">{q.isPending ? "正在读取…" : "没有待处理的问题。"}</p>) : (
          <ScrollArea className="max-h-[60vh]">
          <ul className="m-0 list-none p-0">
            {list.map((n) => (
              <li key={n.id} className="stack border-b border-[var(--weir-line)] px-3 py-2 last:border-0">
                <div className="flex items-start gap-2">
                  <Badge variant="soft" tone={n.level === "warn" ? "warn" : "neutral"}>{n.level === "warn" ? "待处理" : "提示"}</Badge>
                  <strong className="text-body">{n.title}</strong>
                </div>
                {n.detail && <small className="text-[var(--weir-ink-2)]">{n.detail}</small>}
                {n.extra?.reason && <small className="text-[var(--weir-ink-3)]">{n.extra.reason}</small>}
                <div className="flex flex-wrap gap-2">
                  {n.action && <Button size="xs" disabled={isBusy()} busy={isBusy(n.id)} busyText="正在开始…" onClick={() => void act(n)}>{n.action.label}</Button>}
                  <Button size="xs" variant="ghost" disabled={isBusy()} busy={isBusy(n.id + ":dismiss")} busyText="正在忽略…" onClick={() => void run(n.id + ":dismiss", () => api(`/admin/notices/${encodeURIComponent(n.id)}/dismiss`, "POST"), "已忽略")}>忽略</Button>
                </div>
              </li>
            ))}
          </ul>
          </ScrollArea>
        )}
      </PopoverContent>
    </Popover>
  );
}
