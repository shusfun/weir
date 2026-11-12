// VPS 体检列表（设计稿 BVpsDetail 右侧）：卡片里一行一项，名称加一行说明；
// 正常只写一句灰字，有问题用状态标签，读不到的用虚线「未知」标签，不把缺失写成正常。
import type { ReactNode } from "react";
import { Badge } from "./badge";
import { Button } from "./button";

type HealthStatus = "ok" | "warn" | "bad" | "info" | "neutral" | "unknown";

function HealthList({
  items,
}: {
  items: {
    id: string;
    name: ReactNode;
    /** 名称下的一行说明（读数、来源） */
    detail?: ReactNode | null;
    status?: HealthStatus | null;
    label?: ReactNode | null;
    onHandle?: () => void;
    action?: ReactNode;
  }[];
}) {
  return (
    <ul className="weir-health" aria-label="体检">
      {items.map((item) => {
        const status = item.status ?? "unknown";
        const text = item.label == null || item.label === "" ? "未知" : item.label;
        return (
          <li key={item.id} className="weir-set-row">
            <div className="min-w-0">
              <b className="block">{item.name}</b>
              {item.detail != null && item.detail !== "" && <div className="text-meta text-[var(--weir-ink-3)]">{item.detail}</div>}
            </div>
            <span className="weir-health-side">
              {status === "bad" || status === "warn" ? (
                <Badge variant="soft" tone={status}>{text}</Badge>
              ) : status === "unknown" ? (
                <Badge variant="soft" tone="neutral" className="border-dashed bg-transparent">{text}</Badge>
              ) : (
                <span className="text-meta text-[var(--weir-ink-3)]">{text}</span>
              )}
              {item.action ?? (item.onHandle && (
                <Button type="button" size="sm" variant="outline" onClick={item.onHandle}>去处理</Button>
              ))}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

export { HealthList };
export type { HealthStatus };
