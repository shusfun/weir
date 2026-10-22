// 来源 codedthemes/uiable checkbox；本地改为凹槽方框，勾选后点亮。
import { Checkbox as CheckboxPrimitive } from "@base-ui/react/checkbox";
import { cn } from "cn";
import { CheckIcon } from "lucide-react";

function Checkbox({ className, ...props }: CheckboxPrimitive.Root.Props) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer relative flex size-[18px] shrink-0 items-center justify-center rounded-[5px] border border-[var(--mg-line)] bg-[var(--mg-well)] shadow-[inset_0_1px_2px_var(--mg-inset)] outline-none transition-[background-color,border-color] duration-[var(--mg-fast)] after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:ring-3 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive data-checked:border-[var(--mg-accent-lo)] data-checked:bg-[var(--mg-accent)] data-checked:text-[var(--mg-accent-ink)] data-checked:shadow-[inset_0_1px_0_rgb(255_255_255/0.25)]",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current [&>svg]:size-3 [&>svg]:stroke-3"
      >
        <CheckIcon />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };
