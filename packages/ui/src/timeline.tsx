// 竖线串起的事件。节点色：重启与失败用故障色，Laya 用强调色，热更新用正常色，缺记录用虚线并写「未知」。
import type { ReactNode } from "react";
import { cn } from "cn";

type TimelineTone = "restart" | "laya" | "hot" | "fail" | "neutral" | "unknown";

function Timeline({ children, className, label = "时间线" }: { children: ReactNode; className?: string; label?: string }) {
  return (
    <ol data-slot="timeline" className={cn("weir-timeline", className)} aria-label={label}>
      {children}
    </ol>
  );
}

function TimelineItem({
  time,
  tone = "neutral",
  title,
  description,
  tags,
  trace,
}: {
  time: ReactNode | null | undefined;
  tone?: TimelineTone;
  title: ReactNode;
  description?: ReactNode;
  tags?: ReactNode;
  trace?: ReactNode;
}) {
  const missing = time == null || time === "";
  return (
    <li data-slot="timeline-item" data-tone={missing && tone === "neutral" ? "unknown" : tone} className="weir-tl-item">
      <div className="weir-tl-time">{missing ? "未知" : time}</div>
      <div className="weir-tl-title">{title}</div>
      {description != null && description !== "" && <div className="weir-tl-note">{description}</div>}
      {(tags || trace) && (
        <div className="weir-tl-meta">
          {tags}
          {trace}
        </div>
      )}
    </li>
  );
}

export { Timeline, TimelineItem };
export type { TimelineTone };
