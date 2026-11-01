// 来源 codedthemes/uiable button；本地改为 iOS 风格：实心蓝键、灰色填充键、透明键，按下缩到 0.97 并叠填充色（.weir-key），松手弹簧回弹。
import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import { LoaderCircle } from "lucide-react";
import { cn } from "cn";
import { Ripple } from "./ripple";

// 尺寸：桌面紧凑（默认 32 高、sm 28），触屏（touch:）一律做到 44；xs / icon-xs 视觉保持小，触屏靠 .weir-hit 把命中区扩到 44。
const buttonVariants = cva(
  "weir-key group/button inline-flex shrink-0 items-center justify-center gap-2 rounded-[var(--weir-radius)] font-medium whitespace-nowrap select-none disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "weir-key-solid bg-[var(--weir-accent)] text-[var(--weir-accent-ink)] hover:bg-[var(--weir-accent-hi)]",
        // 灰色填充键（iOS gray button）：页面里大多数次要操作
        outline: "text-[var(--weir-ink)]",
        // 选中态：分段控件同款白色滑块
        secondary: "is-pressed text-[var(--weir-ink)]",
        ghost: "weir-key-ghost text-[var(--weir-ink-2)] hover:text-[var(--weir-ink)]",
        destructive: "text-[var(--weir-bad-ink)]",
        // 文字链接：按下变淡（iOS 的 plain 按钮），不缩放、不叠填充
        link: "!bg-transparent px-0 text-[var(--weir-accent-text)] underline-offset-4 after:hidden hover:underline active:!scale-none active:opacity-60",
      },
      size: {
        default: "min-h-8 px-3 py-1 text-body touch:min-h-11 touch:min-w-11 touch:px-4",
        xs: "weir-hit min-h-7 gap-1 px-2 text-meta touch:min-h-8 [&_svg:not([class*='size-'])]:size-3.5",
        sm: "min-h-7 px-3 py-1 text-body touch:min-h-11 touch:min-w-11 [&_svg:not([class*='size-'])]:size-4",
        lg: "min-h-10 px-5 py-2 text-headline font-semibold touch:min-h-12 touch:min-w-12",
        icon: "size-8 p-0 touch:size-11",
        "icon-xs": "weir-hit size-7 p-0 touch:size-8 [&_svg:not([class*='size-'])]:size-3.5",
        "icon-sm": "size-7 p-0 touch:size-11",
        "icon-lg": "size-10 p-0 touch:size-11",
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
      {/* 水波默认关闭：按压反馈是缩放 + 填充，只有个别大面积主操作键另开 */}
      {ripple && <Ripple />}
    </ButtonPrimitive>
  );
}

export { Button, buttonVariants };
