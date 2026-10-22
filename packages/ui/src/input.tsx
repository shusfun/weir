// 来源 codedthemes/uiable input；本地改为凹槽材质，聚焦时显示强调色焦点环。
import { ComponentProps } from "react";
import { Input as InputPrimitive } from "@base-ui/react/input";
import { cn } from "cn";

export const fieldClass =
  "mg-well block w-full min-w-0 px-3 text-[13px] text-[var(--mg-ink)] placeholder:text-[var(--mg-ink-engraved)] outline-none transition-[border-color,box-shadow] duration-[var(--mg-fast)] focus:border-[var(--mg-accent-text)] focus:shadow-[inset_0_1px_2px_var(--mg-inset),0_0_0_3px_var(--mg-ring)] disabled:cursor-not-allowed disabled:opacity-55 read-only:bg-[var(--mg-well-deep)] aria-invalid:border-destructive";

function Input({ className, type, ...props }: ComponentProps<"input">) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(fieldClass, "h-9.5 py-2", className)}
      {...props}
    />
  );
}

export { Input };
