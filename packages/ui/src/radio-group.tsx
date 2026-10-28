// 单选组：基于 @base-ui/react radio-group，替代原生 <input type="radio">。圆形凹槽，选中后点亮中心圆点。
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
        "peer relative flex size-[18px] shrink-0 items-center justify-center rounded-full border border-[var(--mg-line)] bg-[var(--mg-well)] shadow-[inset_0_1px_2px_var(--mg-inset)] outline-none [transition:background-color_var(--mg-fast),border-color_var(--mg-fast),scale_var(--mg-bounce-dur)_var(--mg-bounce)] after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:ring-3 focus-visible:ring-ring active:scale-90 data-disabled:cursor-not-allowed data-disabled:opacity-50 data-disabled:active:scale-100 aria-invalid:border-destructive data-checked:border-[var(--mg-accent-lo)] data-checked:bg-[var(--mg-accent)] data-checked:shadow-[inset_0_1px_0_rgb(255_255_255/0.25)]",
        className,
      )}
      {...props}
    >
      <RadioPrimitive.Indicator
        data-slot="radio-indicator"
        className="size-[7px] rounded-full bg-[var(--mg-accent-ink)] transition-[scale] duration-[var(--mg-fast)] data-[starting-style]:scale-0 data-[ending-style]:scale-0"
      />
    </RadioPrimitive.Root>
  );
}

export { RadioGroup, Radio };
