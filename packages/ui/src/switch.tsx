// 来源 codedthemes/uiable switch；本地改为拟物拨杆：内凹轨道 + 凸起拨钮。
// 弹性开关：按住时拨钮横向拉长，切换时行进中保持拉长、落位压回原宽并回弹。
// 只有用户亲手切换过才播放落位动画（data-moved），首屏渲染和数据回填不会让所有开关一起抖动。
import { useState } from "react";
import { Switch as SwitchPrimitive } from "@base-ui/react/switch";
import { cn } from "cn";

function Switch({
  className,
  size = "default",
  onCheckedChange,
  ...props
}: SwitchPrimitive.Root.Props & { size?: "sm" | "default" }) {
  const [moved, setMoved] = useState(false);
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      data-moved={moved || undefined}
      className={cn("mg-switch peer group/switch", className)}
      onCheckedChange={(checked, details) => {
        setMoved(true);
        onCheckedChange?.(checked, details);
      }}
      {...props}
    >
      <SwitchPrimitive.Thumb data-slot="switch-thumb" className="mg-switch-thumb" />
    </SwitchPrimitive.Root>
  );
}

export { Switch };
