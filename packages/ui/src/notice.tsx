// Weir 提示条：页面 / 弹窗内常驻的说明、结果与错误。左侧状态色刻条，浅色底。
// tone=bad 默认 role="alert"（读屏立即播报）；其余语气不默认加 live region，需要时由调用方传 role="status"。
import { ComponentProps } from "react";
import { cn } from "cn";

const tones = {
  info: "border-[color-mix(in_srgb,var(--mg-info)_30%,var(--mg-line))] bg-[var(--mg-info-soft)] text-[var(--mg-info-ink)] before:bg-[var(--mg-info)]",
  ok: "border-[color-mix(in_srgb,var(--mg-ok)_30%,var(--mg-line))] bg-[var(--mg-ok-soft)] text-[var(--mg-ok-ink)] before:bg-[var(--mg-ok)]",
  warn: "border-[color-mix(in_srgb,var(--mg-warn)_32%,var(--mg-line))] bg-[var(--mg-warn-soft)] text-[var(--mg-warn-ink)] before:bg-[var(--mg-warn)]",
  bad: "border-[color-mix(in_srgb,var(--mg-bad)_32%,var(--mg-line))] bg-[var(--mg-bad-soft)] text-[var(--mg-bad-ink)] before:bg-[var(--mg-bad)]",
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
        "relative m-0 rounded-[var(--mg-radius)] border py-[11px] pr-3.5 pl-4 text-[13px] leading-[1.6] wrap-anywhere",
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
