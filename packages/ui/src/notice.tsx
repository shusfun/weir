// Weir 提示条：页面 / 弹窗内常驻的说明、结果与错误。状态色浅底、无边框，左侧一条状态色细条；info 只是说明，用中性浅灰底，不抢内容。
// tone=bad 默认 role="alert"（读屏立即播报）；其余语气不默认加 live region，需要时由调用方传 role="status"。
import { ComponentProps } from "react";
import { cn } from "cn";

const tones = {
  info: "bg-[var(--weir-fill-4)] text-[var(--weir-ink-2)] before:bg-[var(--weir-info)]",
  ok: "bg-[var(--weir-ok-soft)] text-[var(--weir-ok-ink)] before:bg-[var(--weir-ok)]",
  warn: "bg-[var(--weir-warn-soft)] text-[var(--weir-warn-ink)] before:bg-[var(--weir-warn)]",
  bad: "bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)] before:bg-[var(--weir-bad)]",
  // 洞察：强调色浅底，不是告警。完整横幅用 InsightBanner。
  insight: "bg-[var(--weir-accent-soft)] text-[var(--weir-accent-soft-ink)] before:bg-[var(--weir-accent)]",
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
        "relative m-0 rounded-[var(--weir-radius)] py-3 pr-4 pl-4 text-body wrap-anywhere",
        "before:absolute before:top-2 before:bottom-2 before:left-0 before:w-[3px] before:rounded-r-full before:content-['']",
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
