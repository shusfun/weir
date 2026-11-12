// 页头下的关键指标行：等分 Readout，中间细分隔线，窄屏两列。值缺失由 Readout 显示「未知」。
import type { CSSProperties, ReactNode } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";
import { cn } from "cn";
import { Readout } from "./readout";

type MetricDelta = {
  text: string | null;
  direction: "up" | "down" | null;
  /** 这项指标越大越好还是越小越好；方向与之一致时用正常色，相反用故障色 */
  better: "up" | "down";
};

function MetricStrip({
  items,
  className,
}: {
  items: {
    label: ReactNode;
    value: ReactNode | null | undefined;
    unit?: ReactNode;
    caption?: ReactNode;
    delta?: MetricDelta | null;
  }[];
  className?: string;
}) {
  return (
    <div data-slot="metric-strip" className={cn("weir-metrics", className)} role="group" style={{ "--weir-metrics-cols": String(Math.max(items.length, 1)) } as CSSProperties}>
      {items.map((item, i) => {
        const delta = item.delta;
        const known = delta != null && delta.text != null && delta.direction != null;
        const good = known && delta.direction === delta.better;
        return (
          <div key={i} className="weir-metrics-cell">
            <Readout label={item.label} value={item.value} unit={item.unit} caption={item.caption} />
            {delta !== undefined && (
              <span className={cn("weir-metric-delta", known && (good ? "is-good" : "is-bad"))}>
                {known ? (
                  <>
                    {delta.direction === "up" ? <ArrowUp aria-hidden /> : <ArrowDown aria-hidden />}
                    {delta.text}
                  </>
                ) : (
                  "未知"
                )}
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}

export { MetricStrip };
export type { MetricDelta };
