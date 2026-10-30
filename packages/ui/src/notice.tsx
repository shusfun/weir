// Weir 提示条：页面 / 弹窗内常驻的说明、结果与错误。左侧状态色刻条，浅色底。
// tone=bad 默认 role="alert"（读屏立即播报）；其余语气不默认加 live region，需要时由调用方传 role="status"。
import { ComponentProps } from "react";
import { cn } from "cn";

const tones = {
  info: "border-[color-mix(in_srgb,var(--weir-info)_30%,var(--weir-line))] bg-[var(--weir-info-soft)] text-[var(--weir-info-ink)] before:bg-[var(--weir-info)]",
  ok: "border-[color-mix(in_srgb,var(--weir-ok)_30%,var(--weir-line))] bg-[var(--weir-ok-soft)] text-[var(--weir-ok-ink)] before:bg-[var(--weir-ok)]",
  warn: "border-[color-mix(in_srgb,var(--weir-warn)_32%,var(--weir-line))] bg-[var(--weir-warn-soft)] text-[var(--weir-warn-ink)] before:bg-[var(--weir-warn)]",
  bad: "border-[color-mix(in_srgb,var(--weir-bad)_32%,var(--weir-line))] bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)] before:bg-[var(--weir-bad)]",
} as const;

function Notice({
  tone = "info",
  role,
  className,
  ...props
}: ComponentProps<"div"> & { tone?: keyof typeof tones }) {
  return (
    <div
      data-slot="notice"
      data-tone={tone}
      role={role ?? (tone === "bad" ? "alert" : undefined)}
      className={cn(
        "relative m-0 rounded-[var(--weir-radius)] border py-[11px] pr-3.5 pl-4 text-[13px] leading-[1.6] wrap-anywhere",
        "before:absolute before:top-2 before:bottom-2 before:left-0 before:w-[3px] before:rounded-r-[3px] before:content-['']",
        // 提示条里的段落、链接沿用提示条的文字色
        "[&_p]:m-0 [&_p]:text-inherit [&_a]:underline [&_a]:underline-offset-3",
        tones[tone],
        className,
      )}
      {...props}
    />
  );
}

export { Notice };
