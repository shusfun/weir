// 单选组：基于 @base-ui/react radio-group，替代原生 <input type="radio">。细边框圆圈，选中后填主色、中心白点按弹簧弹出；
// 触屏下视觉放大到 22px，.weir-hit 把命中区扩到 44。
// 常与 ChoiceCard render={<label />} 组合：<label> 包住 <Radio value=…/> 和说明文字，整张卡片可点。
import { Radio as RadioPrimitive } from "@base-ui/react/radio";
import { RadioGroup as RadioGroupPrimitive } from "@base-ui/react/radio-group";
import { cn } from "cn";

function RadioGroup({ className, ...props }: RadioGroupPrimitive.Props) {
  return <RadioGroupPrimitive data-slot="radio-group" className={cn("min-w-0", className)} {...props} />;
}

function Radio({ className, ...props }: RadioPrimitive.Root.Props) {
  return (
    <RadioPrimitive.Root
      data-slot="radio"
      className={cn(
        "peer weir-hit relative flex size-[18px] shrink-0 items-center justify-center rounded-full border-[1.5px] border-[var(--weir-control-border)] bg-[var(--weir-panel)] [transition:background-color_var(--weir-fast)_var(--weir-ease),border-color_var(--weir-fast)_var(--weir-ease),scale_var(--weir-spring-dur)_var(--weir-spring)] active:scale-90 active:[transition-duration:var(--weir-press)] data-disabled:cursor-not-allowed data-disabled:opacity-50 data-disabled:active:scale-100 aria-invalid:border-destructive data-checked:border-transparent data-checked:bg-[var(--weir-accent)] touch:size-[22px]",
        className,
      )}
      {...props}
    >
      <RadioPrimitive.Indicator
        data-slot="radio-indicator"
        className="size-[7px] rounded-full bg-[var(--weir-accent-ink)] transition-[scale] duration-[var(--weir-spring-dur)] ease-[var(--weir-bounce)] data-[starting-style]:scale-0 data-[ending-style]:scale-0 touch:size-2"
      />
    </RadioPrimitive.Root>
  );
}

export { RadioGroup, Radio };
