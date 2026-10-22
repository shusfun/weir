// 来源 codedthemes/uiable button；本地改为新拟物键帽（.mg-key）：按下外阴影转内阴影并下沉 2px，松手弹簧回弹。
import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";

const buttonVariants = cva(
  "mg-key group/button inline-flex shrink-0 items-center justify-center gap-2 rounded-[var(--mg-radius)] font-medium whitespace-nowrap select-none outline-none disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-[var(--mg-accent)] text-[var(--mg-accent-ink)] hover:bg-[var(--mg-accent-hi)]",
        outline: "text-[var(--mg-ink)]",
        // 选中态：保持按下
        secondary: "is-pressed text-[var(--mg-accent-soft-ink)]",
        ghost: "mg-key-ghost text-[var(--mg-ink-2)] hover:text-[var(--mg-ink)]",
        destructive: "text-[var(--mg-bad-ink)]",
        link: "!bg-transparent px-0 text-[var(--mg-accent-text)] underline-offset-4 before:hidden after:hidden hover:underline active:!transform-none",
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

function Button({
  className,
  variant = "default",
  size = "default",
  nativeButton,
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      data-variant={variant}
      className={cn(buttonVariants({ variant, size, className }))}
      // 以链接等非 <button> 元素渲染时（render={<Link/>}）告知 Base UI，避免丢失按钮语义的警告
      nativeButton={nativeButton ?? !props.render}
      {...props}
    />
  );
}

export { Button, buttonVariants };
