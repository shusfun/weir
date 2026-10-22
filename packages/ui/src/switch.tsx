// 来源 codedthemes/uiable switch；本地改为拟物拨杆：内凹轨道 + 凸起拨钮，切换时弹簧吸附到端点并轻微回弹。
import { Switch as SwitchPrimitive } from "@base-ui/react/switch";
import { cn } from "cn";

function Switch({
  className,
  size = "default",
  ...props
}: SwitchPrimitive.Root.Props & { size?: "sm" | "default" }) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      className={cn("mg-switch peer group/switch", className)}
      {...props}
    >
      <SwitchPrimitive.Thumb data-slot="switch-thumb" className="mg-switch-thumb" />
    </SwitchPrimitive.Root>
  );
}

export { Switch };
