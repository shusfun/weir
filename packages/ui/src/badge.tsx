// 来源 codedthemes/uiable badge；按设计稿 .v-pill：25 高、圆角 9 的小标签，状态色文字加 11% 浅底，中性标签是凹面底加细描边。
import { mergeProps } from "@base-ui/react/merge-props";
import { useRender } from "@base-ui/react/use-render";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";

const badgeVariants = cva(
  "group/badge inline-flex h-[25px] w-fit shrink-0 items-center justify-center gap-[5px] overflow-hidden has-[a]:overflow-visible rounded-[var(--weir-radius-9)] border border-transparent px-[11px] text-fine font-semibold whitespace-nowrap [&>svg]:pointer-events-none [&>svg]:size-3!",
  {
    variants: {
      variant: {
        default: "bg-[var(--weir-accent)] text-[var(--weir-accent-ink)]",
        secondary: "bg-[var(--weir-accent-soft)] text-[var(--weir-accent-soft-ink)]",
        destructive: "bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)]",
        outline: "border-[var(--weir-line)] bg-transparent text-[var(--weir-ink-2)]",
        ghost: "text-[var(--weir-ink-3)]",
        link: "text-[var(--weir-accent-text)] underline-offset-4 hover:underline",
        // 状态色浅底标签：配合 tone 使用（事件级别、处理状态、已设置 / 未绑定等）
        soft: "",
        // 状态文字：透明底 + 状态色文字，通常前置 StatusLamp（任务 / 设备 / 授权状态）
        status: "gap-1.5 bg-transparent pr-2 pl-1",
        // 小标签（稿子 .v-tag）：协议、安全层这类属性，比状态标签更轻
        tag: "h-auto rounded-[var(--weir-radius-7)] border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-[7px] py-px font-normal text-[var(--weir-ink-2)]",
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
      { variant: "soft", tone: "neutral", className: "border-[var(--weir-line-soft)] bg-[var(--weir-well)] text-[var(--weir-ink-2)]" },
      { variant: "soft", tone: "ok", className: "bg-[var(--weir-ok-soft)] text-[var(--weir-ok-ink)]" },
      { variant: "soft", tone: "warn", className: "bg-[var(--weir-warn-soft)] text-[var(--weir-warn-ink)]" },
      { variant: "soft", tone: "bad", className: "bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)]" },
      { variant: "soft", tone: "info", className: "bg-[var(--weir-info-soft)] text-[var(--weir-info-ink)]" },
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
