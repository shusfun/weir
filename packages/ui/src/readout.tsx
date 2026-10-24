// Weir 读数窗与表头。value 为 null / undefined 时显示“未知”，Meter 显示斜纹，不落到 0。
import { ComponentProps, ReactNode } from "react";
import { cn } from "cn";
import { Counter, isCountable } from "./counter";

function Readout({
  label,
  value,
  unit,
  caption,
  variant = "plain",
  framed = false,
  className,
  children,
  ...props
}: Omit<ComponentProps<"div">, "children"> & {
  framed?: boolean;
  label: ReactNode;
  value: ReactNode | null | undefined;
  unit?: ReactNode;
  caption?: ReactNode;
  variant?: "plain" | "lcd";
  children?: ReactNode;
}) {
  const unknown = value === null || value === undefined || value === "未知";
  return (
    <div
      data-slot="readout"
      data-variant={variant}
      data-unknown={unknown}
      className={cn("mg-readout", className)}
      {...props}
    >
      <span className="mg-engraved">{label}</span>
      <span className="mg-readout-value">
        {unknown ? "未知" : isCountable(value) ? <Counter value={value as string | number} framed={framed} /> : value}
        {!unknown && unit && <span className="mg-readout-unit">{unit}</span>}
      </span>
      {caption && <span className="mg-readout-caption">{caption}</span>}
      {children}
    </div>
  );
}

function Meter({
  value,
  max,
  label,
  className,
  ...props
}: ComponentProps<"div"> & { value: number | null | undefined; max: number | null | undefined; label: string }) {
  const known = typeof value === "number" && typeof max === "number" && Number.isFinite(value) && Number.isFinite(max) && max > 0;
  const ratio = known ? Math.min(1, Math.max(0, value / max)) : 0;
  const tone = !known ? undefined : ratio >= 0.95 ? "bad" : ratio >= 0.8 ? "warn" : undefined;
  return (
    <div
      data-slot="meter"
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={known ? max : undefined}
      aria-valuenow={known ? value : undefined}
      aria-valuetext={known ? Math.round(ratio * 100) + "%" : "未知"}
      data-unknown={!known}
      data-tone={tone}
      className={cn("mg-meter", className)}
      {...props}
    >
      {known && <span style={{ width: ratio * 100 + "%" }} />}
    </div>
  );
}

export { Readout, Meter };
