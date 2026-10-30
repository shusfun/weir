// 来源 codedthemes/uiable badge；本地改为压印铭牌样式。
import { mergeProps } from "@base-ui/react/merge-props";
import { useRender } from "@base-ui/react/use-render";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";

const badgeVariants = cva(
  "group/badge inline-flex w-fit shrink-0 items-center justify-center gap-1.5 overflow-hidden rounded-[var(--weir-radius-sm)] border px-2 py-0.5 text-[11px] leading-[1.45] font-medium whitespace-nowrap [&>svg]:pointer-events-none [&>svg]:size-3!",
  {
    variants: {
      variant: {
        default: "border-[var(--weir-accent-lo)] bg-[var(--weir-accent)] text-[var(--weir-accent-ink)]",
        secondary:
          "border-[color-mix(in_srgb,var(--weir-accent)_25%,var(--weir-line))] bg-[var(--weir-accent-soft)] text-[var(--weir-accent-soft-ink)]",
        destructive:
          "border-[color-mix(in_srgb,var(--weir-bad)_35%,var(--weir-line))] bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)]",
        outline:
          "border-[var(--weir-line-soft)] bg-transparent text-[var(--weir-ink-2)]",
        ghost: "border-transparent text-[var(--weir-ink-3)]",
        link: "border-transparent text-[var(--weir-accent-text)] underline-offset-4 hover:underline",
        // 状态色浅底标签：配合 tone 使用（事件级别、处理状态、已设置 / 未绑定等）
        soft: "",
        // 状态文字：透明底 + 状态色文字，通常前置 StatusLamp（任务 / 设备 / 授权状态）
        status: "gap-1.5 border-transparent bg-transparent py-0.5 pr-2 pl-1 text-[11.5px]",
      },
      tone: {
        neutral: "",
        ok: "",
        warn: "",
        bad: "",
        info: "",
      },
    },
    compoundVariants: [
      { variant: "soft", tone: "neutral", className: "border-[var(--weir-line)] bg-[var(--weir-well)] text-[var(--weir-ink-2)]" },
      { variant: "soft", tone: "ok", className: "border-[color-mix(in_srgb,var(--weir-ok)_30%,var(--weir-line))] bg-[var(--weir-ok-soft)] text-[var(--weir-ok-ink)]" },
      { variant: "soft", tone: "warn", className: "border-[color-mix(in_srgb,var(--weir-warn)_30%,var(--weir-line))] bg-[var(--weir-warn-soft)] text-[var(--weir-warn-ink)]" },
      { variant: "soft", tone: "bad", className: "border-[color-mix(in_srgb,var(--weir-bad)_30%,var(--weir-line))] bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)]" },
      { variant: "soft", tone: "info", className: "border-[color-mix(in_srgb,var(--weir-info)_30%,var(--weir-line))] bg-[var(--weir-info-soft)] text-[var(--weir-info-ink)]" },
      { variant: "status", tone: "neutral", className: "text-[var(--weir-ink-2)]" },
      { variant: "status", tone: "ok", className: "text-[var(--weir-ok-ink)]" },
      { variant: "status", tone: "warn", className: "text-[var(--weir-warn-ink)]" },
      { variant: "status", tone: "bad", className: "text-[var(--weir-bad-ink)]" },
      { variant: "status", tone: "info", className: "text-[var(--weir-info-ink)]" },
    ],
    defaultVariants: { variant: "default", tone: "neutral" },
  },
);

function Badge({
  className,
  variant = "default",
  tone = "neutral",
  render,
  ...props
}: useRender.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return useRender({
    defaultTagName: "span",
    props: mergeProps<"span">({ className: cn(badgeVariants({ variant, tone }), className) }, props),
    render,
    state: { slot: "badge", variant },
  });
}

export { Badge, badgeVariants };
