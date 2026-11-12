// 行内走势：纯 SVG，无坐标轴。缺失的点断开，不连成 0。整段缺失显示「未知」。
// 入场描线只在完整档；均衡与省电直接显示终态。
import type { ComponentProps } from "react";
import { cn } from "cn";
import { useMotionTier } from "./motion-tier";

type SparkTone = "neutral" | "ok" | "warn" | "bad" | "info";

function finite(values: readonly (number | null | undefined)[] | null | undefined) {
  if (!values) return [];
  return values.filter((v): v is number => typeof v === "number" && Number.isFinite(v));
}

function Sparkline({
  values,
  tone = "neutral",
  label,
  className,
  ...props
}: Omit<ComponentProps<"svg">, "values"> & {
  values: readonly (number | null | undefined)[] | null | undefined;
  tone?: SparkTone;
  label: string;
}) {
  const tier = useMotionTier();
  const known = finite(values);
  if (!values || known.length === 0) {
    return (
      <span data-slot="sparkline" data-unknown="true" className={cn("weir-spark-unknown", className)} role="img" aria-label={`${label}：未知`}>
        未知
      </span>
    );
  }
  const min = Math.min(...known);
  const max = Math.max(...known);
  const span = max - min || 1;
  const last = Math.max(values.length - 1, 1);
  let pen = false;
  let d = "";
  values.forEach((v, i) => {
    if (typeof v !== "number" || !Number.isFinite(v)) {
      pen = false;
      return;
    }
    const x = (i / last) * 97;
    const y = 3 + (1 - (v - min) / span) * 23;
    d += `${pen ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`;
    pen = true;
  });
  return (
    <svg
      data-slot="sparkline"
      data-tone={tone}
      data-draw={tier === "full" || undefined}
      className={cn("weir-spark", className)}
      viewBox="0 0 97 29"
      role="img"
      aria-label={label}
      {...props}
    >
      <path d={d} pathLength={1} />
    </svg>
  );
}

export { Sparkline };
export type { SparkTone };
