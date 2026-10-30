// 来源 codedthemes/uiable checkbox；本地改为凹槽方框，勾选后点亮。
// 按下缩到 0.9，松手按 --weir-bounce 回弹；用户亲手勾上时对勾一笔画出，
// 首屏已勾选的直接显示，不让一整列同时描绘。
import { useState } from "react";
import { Checkbox as CheckboxPrimitive } from "@base-ui/react/checkbox";
import { cn } from "cn";
import { CheckMark } from "./check-mark";

function Checkbox({ className, onCheckedChange, ...props }: CheckboxPrimitive.Root.Props) {
  const [moved, setMoved] = useState(false);
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer relative flex size-[18px] shrink-0 items-center justify-center rounded-[5px] border border-[var(--weir-line)] bg-[var(--weir-well)] shadow-[inset_0_1px_2px_var(--weir-inset)] outline-none [transition:background-color_var(--weir-fast),border-color_var(--weir-fast),scale_var(--weir-bounce-dur)_var(--weir-bounce)] after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:ring-3 focus-visible:ring-ring active:scale-90 active:[transition:background-color_var(--weir-fast),border-color_var(--weir-fast),scale_var(--weir-fast)_ease-out] disabled:cursor-not-allowed disabled:opacity-50 disabled:active:scale-100 aria-invalid:border-destructive data-checked:border-[var(--weir-accent-lo)] data-checked:bg-[var(--weir-accent)] data-checked:text-[var(--weir-accent-ink)] data-checked:shadow-[inset_0_1px_0_rgb(255_255_255/0.25)]",
        className,
      )}
      onCheckedChange={(checked, details) => {
        setMoved(true);
        onCheckedChange?.(checked, details);
      }}
      {...props}
    >
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator" className="grid place-content-center text-current [&>svg]:size-3">
        <CheckMark animate={moved} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };
