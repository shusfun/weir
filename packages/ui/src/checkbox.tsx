// 来源 codedthemes/uiable checkbox；本地改为 iOS / macOS 勾选框：细边框方框，勾选后填 systemBlue。
// 按下缩到 0.9，松手按 --weir-spring 回弹；触屏下视觉放大到 22px，.weir-hit 把命中区扩到 44。
// 用户亲手勾上时对勾一笔画出，首屏已勾选的直接显示，不让一整列同时描绘。
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
        "peer weir-hit relative flex size-[18px] shrink-0 items-center justify-center rounded-[var(--weir-radius-xs)] border-[1.5px] border-[var(--weir-control-border)] bg-[var(--weir-panel)] [transition:background-color_var(--weir-fast)_var(--weir-ease),border-color_var(--weir-fast)_var(--weir-ease),scale_var(--weir-spring-dur)_var(--weir-spring)] active:scale-90 active:[transition-duration:var(--weir-press)] disabled:cursor-not-allowed disabled:opacity-50 disabled:active:scale-100 aria-invalid:border-destructive data-checked:border-transparent data-checked:bg-[var(--weir-accent)] data-checked:text-[var(--weir-accent-ink)] touch:size-[22px]",
        className,
      )}
      onCheckedChange={(checked, details) => {
        setMoved(true);
        onCheckedChange?.(checked, details);
      }}
      {...props}
    >
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator" className="grid place-content-center text-current [&>svg]:size-3 touch:[&>svg]:size-3.5">
        <CheckMark animate={moved} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };
