// MiGate 分段拨钮：单选筛选、指标切换。内凹底座内各项平放，当前项点亮为选中色（.is-pressed）。
// 所有“几个按钮里选一个”的场景都用它，不要用 Button + aria-pressed 手拼。
import { ToggleGroup } from "@base-ui/react/toggle-group";
import { Toggle } from "@base-ui/react/toggle";
import { cn } from "cn";
import type { ReactNode } from "react";

function Segmented<T extends string>({
  value,
  onValueChange,
  options,
  size = "default",
  fullWidth = false,
  className,
  "aria-label": ariaLabel,
}: {
  value: T;
  onValueChange: (value: T) => void;
  options: readonly (readonly [T, ReactNode])[];
  size?: "sm" | "default";
  // true：始终撑满；"mobile"：仅窄屏撑满（工具栏里的筛选）
  fullWidth?: boolean | "mobile";
  className?: string;
  "aria-label": string;
}) {
  return (
    <ToggleGroup
      data-slot="segmented"
      aria-label={ariaLabel}
      value={[value]}
      onValueChange={(next) => {
        // 单选：不允许取消当前项
        const picked = next.find((v) => v !== value) ?? value;
        if (picked !== value) onValueChange(picked as T);
      }}
      data-full-width={fullWidth === true ? "always" : fullWidth === "mobile" ? "mobile" : undefined}
      className={cn("mg-segmented", className)}
    >
      {options.map(([key, label]) => (
        <Toggle
          key={key}
          value={key}
          className={(state) =>
            cn(
              "mg-key inline-flex shrink-0 items-center justify-center gap-1.5 rounded-[var(--mg-radius-sm)] px-3 whitespace-nowrap text-[var(--mg-ink-3)] outline-none hover:text-[var(--mg-ink)] [&_svg]:size-3.5",
              size === "sm" ? "h-7 text-xs" : "h-8 text-[12.5px]",
              state.pressed && "is-pressed font-semibold text-[var(--mg-ink)]",
            )
          }
        >
          {label}
        </Toggle>
      ))}
    </ToggleGroup>
  );
}

export { Segmented };
