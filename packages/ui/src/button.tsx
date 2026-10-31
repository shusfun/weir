// 来源 codedthemes/uiable button；本地改为新拟物键帽（.weir-key）：按下外阴影转内阴影并下沉 2px，松手弹簧回弹。
import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import { LoaderCircle } from "lucide-react";
import { cn } from "cn";
import { Ripple } from "./ripple";

const buttonVariants = cva(
  "weir-key group/button inline-flex shrink-0 items-center justify-center gap-2 rounded-[var(--weir-radius)] font-medium whitespace-nowrap select-none disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-[var(--weir-accent)] text-[var(--weir-accent-ink)] hover:bg-[var(--weir-accent-hi)]",
        outline: "text-[var(--weir-ink)]",
        // 选中态：保持按下
        secondary: "is-pressed text-[var(--weir-accent-soft-ink)]",
        ghost: "weir-key-ghost text-[var(--weir-ink-2)] hover:text-[var(--weir-ink)]",
        destructive: "text-[var(--weir-bad-ink)]",
        link: "!bg-transparent px-0 text-[var(--weir-accent-text)] underline-offset-4 before:hidden after:hidden hover:underline active:!translate-none active:!scale-none",
      },
      size: {
        default: "min-h-9 px-3.5 py-1.5 text-[13px]",
        xs: "min-h-6 gap-1 px-2 text-xs [&_svg:not([class*='size-'])]:size-3",
        sm: "min-h-8 px-3 py-1 text-[12.5px] [&_svg:not([class*='size-'])]:size-3.5",
        lg: "min-h-11 px-5 py-2 text-sm",
        icon: "size-9 p-0",
        "icon-xs": "size-6 p-0 [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-8 p-0",
        "icon-lg": "size-10 p-0",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

// busy：请求进行中，按钮禁用并把文字换成 busyText（默认「正在处理…」）。两段文字叠在同一格里、
// 宽度取较长的那段，切换时按钮和旁边的按钮都不移动。只有图标的按钮（size 为 icon*）没有文字可换：
// 图标原位换成转圈，busyText 作为读屏文字。
function Button({
  className,
  variant = "default",
  size = "default",
  nativeButton,
  ripple = false,
  busy,
  busyText = "正在处理…",
  disabled,
  children,
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants> & { ripple?: boolean; busy?: boolean; busyText?: string }) {
  const iconOnly = typeof size === "string" && size.startsWith("icon");
  const label =
    busy === undefined ? children : iconOnly ? (
      <span className="grid place-items-center [&>*]:col-start-1 [&>*]:row-start-1">
        <span className={cn("inline-flex", busy && "invisible")} aria-hidden={busy || undefined}>{children}</span>
        {busy && <LoaderCircle className="animate-spin motion-reduce:animate-none" aria-label={busyText} />}
      </span>
    ) : (
      <span className="grid items-center justify-items-center [&>*]:col-start-1 [&>*]:row-start-1">
        <span className={cn("inline-flex items-center gap-[inherit]", busy && "invisible")} aria-hidden={busy || undefined}>{children}</span>
        <span className={cn(!busy && "invisible")} aria-hidden={!busy || undefined}>{busyText}</span>
      </span>
    );
  return (
    <ButtonPrimitive
      data-slot="button"
      data-variant={variant}
      className={cn(buttonVariants({ variant, size, className }))}
      // 以链接等非 <button> 元素渲染时（render={<Link/>}）告知 Base UI，避免丢失按钮语义的警告
      nativeButton={nativeButton ?? !props.render}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      {...props}
    >
      {label}
      {/* 水波默认关闭：小键帽上按压内凹已是反馈，大面积主操作键再开 */}
      {ripple && <Ripple />}
    </ButtonPrimitive>
  );
}

export { Button, buttonVariants };
