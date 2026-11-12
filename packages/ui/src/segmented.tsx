// Weir 分段控件（iOS UISegmentedControl）：单选筛选、指标切换。填充底座，当前项是白色滑块（.is-pressed）。
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
  id,
  "aria-label": ariaLabel,
}: {
  value: T;
  onValueChange: (value: T) => void;
  options: readonly (readonly [T, ReactNode])[];
  size?: "sm" | "default";
  // true：始终撑满；"mobile"：仅窄屏撑满（工具栏里的筛选）
  fullWidth?: boolean | "mobile";
  className?: string;
  id?: string;
  "aria-label": string;
}) {
  return (
    <ToggleGroup
      id={id}
      data-slot="segmented"
      aria-label={ariaLabel}
      value={[value]}
      onValueChange={(next) => {
        // 单选：不允许取消当前项
        const picked = next.find((v) => v !== value) ?? value;
        if (picked !== value) onValueChange(picked as T);
      }}
      data-full-width={fullWidth === true ? "always" : fullWidth === "mobile" ? "mobile" : undefined}
      className={cn("weir-segmented", className)}
    >
      {options.map(([key, label]) => (
        <Toggle
          key={key}
          value={key}
          className={(state) =>
            cn(
              "weir-key inline-flex shrink-0 items-center justify-center gap-[var(--weir-gap-7)] px-[var(--weir-space-3)] font-medium whitespace-nowrap text-[var(--weir-ink-2)] hover:text-[var(--weir-ink)] [&_svg]:size-[var(--weir-icon-sm)]",
              // 保留分段控件的紧凑档位，触屏单项的命中区至少 45。
              size === "sm" ? "h-[var(--weir-gap-21)] text-meta touch:h-[var(--weir-tap-touch)] touch:min-w-[var(--weir-tap-touch)]" : "h-[var(--weir-gap-25)] text-meta touch:h-[var(--weir-tap-touch)] touch:min-w-[var(--weir-tap-touch)] touch:text-body",
              state.pressed && "is-pressed font-semibold text-[var(--weir-ink)]",
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
