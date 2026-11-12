// Laya 洞察横幅（设计稿 .v-ins）：左边是 Laya 标识块，中间一句结论和一行进度，右边一个主按钮。
// 置信度缺失写「Laya 判断未知」，不当成有把握。
import type { ReactNode } from "react";
import { cn } from "cn";

function InsightBanner({
  title,
  confidence,
  progress,
  trace,
  action,
  more,
  icon,
  laya = true,
  className,
}: {
  title: ReactNode;
  /** Laya 置信度，如「96%」；缺失写未知 */
  confidence?: ReactNode | null;
  /** 进度说明，如「dsh 已起草 · 不会重启内核」 */
  progress?: ReactNode | null;
  trace?: ReactNode;
  action?: ReactNode;
  more?: number | null;
  /** 左侧标识，默认 Laya 的「L」 */
  icon?: ReactNode;
  /** 不是 Laya 的结论（如内核可升级）时传 false，不写「Laya 判断」 */
  laya?: boolean;
  className?: string;
}) {
  const parts: ReactNode[] = laya ? [`Laya 判断 ${confidence == null || confidence === "" ? "未知" : confidence}`] : [];
  if (progress != null && progress !== "") parts.push(progress);
  if (typeof more === "number" && more > 0) parts.push(`还有 ${more} 条`);
  return (
    <div data-slot="insight-banner" className={cn("weir-insight", className)} role="status">
      <span className="weir-insight-mark" aria-hidden>{icon ?? "L"}</span>
      <div className="weir-insight-copy">
        <p className="weir-insight-title">{title}</p>
        <p className="weir-insight-meta">
          {parts.map((p, i) => (
            <span key={i}>{i > 0 && " · "}{p}</span>
          ))}
          {trace && <span> · {trace}</span>}
        </p>
      </div>
      {action && <div className="weir-insight-action">{action}</div>}
    </div>
  );
}

export { InsightBanner };
