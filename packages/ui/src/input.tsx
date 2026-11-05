// 来源 codedthemes/uiable input；本地改为白底细边框（表单参照 Element Plus），聚焦时主色 2px 描边。
// 高度桌面 32、触屏 44；字号用 control 角色，触屏 16（iOS 聚焦字号小于 16 的输入框会放大整页）。
import { ComponentProps } from "react";
import { Input as InputPrimitive } from "@base-ui/react/input";
import { cn } from "cn";

// 鼠标点进输入框也画焦点描边（:focus 而不只 :focus-visible），与 iOS / macOS 输入框一致
export const fieldClass =
  "weir-input block w-full min-w-0 px-3 text-control text-[var(--weir-ink)] placeholder:text-[var(--weir-ink-3)] transition-colors focus:outline-2 focus:outline-offset-0 focus:outline-[var(--weir-focus)] disabled:cursor-not-allowed disabled:bg-[var(--weir-fill-4)] disabled:opacity-55 read-only:bg-[var(--weir-fill-4)] aria-invalid:border-destructive";

function Input({ className, type, ...props }: ComponentProps<"input">) {
  return <InputPrimitive type={type} data-slot="input" className={cn(fieldClass, "h-8 py-1 touch:h-11", className)} {...props} />;
}

export { Input };
