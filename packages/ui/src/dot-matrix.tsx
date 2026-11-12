// 点阵：DotMatrix 用固定格数表示负载或额度，缺数据整块显示「未知」，不涂成 0。
// DotCrowd 是集合页群像：一个成员一个点，悬停说出是谁，点击交给调用方去定位列表行。
import type { CSSProperties, ReactNode } from "react";
import { cn } from "cn";

type DotTone = "accent" | "ok" | "warn" | "bad" | "off" | "unknown";

function DotMatrix({
  label,
  value,
  max,
  columns = 21,
  rows = 3,
  tone = "accent",
  className,
}: {
  label: string;
  value: number | null | undefined;
  max: number | null | undefined;
  columns?: number;
  rows?: number;
  tone?: DotTone;
  className?: string;
}) {
  const total = columns * rows;
  const known = typeof value === "number" && typeof max === "number" && Number.isFinite(value) && Number.isFinite(max) && max > 0;
  const on = known ? Math.max(0, Math.min(total, Math.round((value / max) * total))) : 0;
  return (
    <div data-slot="dot-matrix" className={cn("weir-dot-block", className)}>
      <div className="weir-dot-label">
        <span>{label}</span>
        <span className="weir-digits">{known ? `${value} / ${max}` : "未知"}</span>
      </div>
      <div
        className="weir-dots"
        style={{ "--weir-dot-cols": String(columns) } as CSSProperties}
        role="img"
        aria-label={known ? `${label} ${value} / ${max}` : `${label}：未知`}
      >
        {Array.from({ length: total }, (_, i) => (
          <span key={i} data-on={known && i < on ? tone : undefined} data-unknown={!known || undefined} />
        ))}
      </div>
    </div>
  );
}

type CrowdTone = "ok" | "warn" | "bad" | "info" | "off" | "unknown";

function DotCrowd({
  members,
  onSelect,
  label = "群像",
  className,
}: {
  members: {
    id: string;
    name: string;
    /** 0–1。缺失时点用中等大小，提示里写未知，不当成 0 */
    weight?: number | null;
    tone?: CrowdTone | null;
  }[];
  onSelect?: (id: string) => void;
  label?: string;
  className?: string;
}) {
  return (
    <div data-slot="dot-crowd" className={cn("weir-crowd", className)} role="group" aria-label={label}>
      {members.map((m) => {
        const tone = m.tone ?? "unknown";
        const weight = typeof m.weight === "number" && Number.isFinite(m.weight) ? m.weight : null;
        // 点的直径 7–27（奇数），按占比平方放大，量越大越显眼；缺数据用中等的 13
        const px = weight == null ? 13 : (Math.round(7 + Math.min(1, weight) ** 2 * 20) | 1);
        const hint = `${m.name}${weight == null ? " · 流量未知" : ""}`;
        return (
          <button
            key={m.id}
            type="button"
            className="weir-hit weir-crowd-dot"
            data-tone={tone}
            style={{ width: px, height: px }}
            title={hint}
            aria-label={hint}
            onClick={() => onSelect?.(m.id)}
          />
        );
      })}
    </div>
  );
}

export { DotMatrix, DotCrowd };
export type { DotTone, CrowdTone };
