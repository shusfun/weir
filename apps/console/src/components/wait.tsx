// 等待提示：请求超过 2 秒时，在按钮下方的固定行（弹窗里是页脚按钮左侧那一位）写
// 「正在<做什么>（<对象>）· 已等 N 秒，最多 M 秒」；中心把操作转成后台任务后，改为显示任务推来的当前步骤，
// 「详情」在二级弹窗里看日志、取消。结束后同一位置换成结果或失败原因，页面不跳动。
import { useEffect, useState } from "react";
import { Button, Dialog, DialogContent, DialogDescription, DialogTitle } from "@weir/ui";
import type { Pending } from "../lib/action";
import { pollingNote, useLive } from "../lib/live";
import { JobByID } from "./job";

export function useTick(on: boolean) {
  const [, set] = useState(0);
  useEffect(() => {
    if (!on) return;
    const t = setInterval(() => set((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, [on]);
}

function waited(since: number, max?: number) {
  const n = Math.max(0, Math.round((Date.now() - since) / 1000));
  if (!max) return `已等 ${n} 秒`;
  return n > max ? `已等 ${n} 秒，超过预计的 ${max} 秒` : `已等 ${n} 秒，最多 ${max} 秒`;
}

// stepSince：任务当前步骤开始的时刻，按服务端的相对时间换算到本机（不受两边时钟差影响）
function stepSince(p: Pending) {
  const j = p.job;
  if (!j?.step_at) return p.since;
  return p.since + (Date.parse(j.step_at) - Date.parse(j.started_at));
}

// ActionsWait：按钮行（.actions）末尾的等待位。只占这一行剩下的宽度（basis 0），出现、消失都不把按钮挤到下一行。
export function ActionsWait({ wait }: { wait: Pending | null }) {
  if (!wait) return null;
  return <small className="min-w-0 grow basis-0 text-[var(--weir-ink-3)]"><WaitText wait={wait} /></small>;
}

export function WaitText({ wait }: { wait: Pending | null }) {
  const live = useLive();
  const [open, setOpen] = useState(false);
  useTick(!!wait);
  if (!wait) return null;
  const j = wait.job;
  const step = j?.step || wait.label || j?.title || "正在处理";
  const text = `${step} · ${waited(j?.step ? stepSince(wait) : wait.since, j?.step ? j.step_max : wait.max)}${j && !live ? " · " + pollingNote : ""}`;
  return (
    <span className="inline-flex min-w-0 max-w-full items-center gap-1.5 tabular-nums" aria-live="polite">
      <span className="min-w-0 truncate" title={text}>{text}</span>
      {j && (
        <>
          <Button size="xs" variant="ghost" className="shrink-0" onClick={() => setOpen(true)}>详情</Button>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogContent size="lg">
              <DialogTitle>{j.title}</DialogTitle>
              <DialogDescription>操作在中心后台进行：关掉窗口、断开网络都不会中断它，控制台开着时结束后会提示结果。还没开始写入时可以取消。</DialogDescription>
              <JobByID id={j.id} initial={j} openLogs />
            </DialogContent>
          </Dialog>
        </>
      )}
    </span>
  );
}
