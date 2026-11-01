// 来源 codedthemes/uiable switch；本地改为 iOS UISwitch：灰色轨道、打开为绿色，白色拨钮按弹簧落位，按住时拨钮横向拉长。
// 触屏下 .weir-hit 把命中区扩到 44×44。样式见 styles/controls.css 的 .weir-switch。
import { Switch as SwitchPrimitive } from "@base-ui/react/switch";
import { cn } from "cn";

function Switch({ className, size = "default", ...props }: SwitchPrimitive.Root.Props & { size?: "sm" | "default" }) {
  return (
    <SwitchPrimitive.Root data-slot="switch" data-size={size} className={cn("weir-switch weir-hit peer group/switch", className)} {...props}>
      <SwitchPrimitive.Thumb data-slot="switch-thumb" className="weir-switch-thumb" />
    </SwitchPrimitive.Root>
  );
}

export { Switch };
