// 成功勾选：对勾不是直接出现，而是沿路径一笔画出（stroke-dashoffset），可选外圈先画、对勾后画。
// pathLength 统一为 1，任何尺寸下动画节奏一致。挂载即播放；需要重播时换 key。
import type { ComponentProps } from "react";
import { cn } from "cn";

// animate=false 时直接显示画好的对勾（例如首屏已勾选的复选框，不让一整列同时描绘）。
function CheckMark({
  circle = false,
  animate = true,
  className,
  ...props
}: Omit<ComponentProps<"svg">, "children"> & { circle?: boolean; animate?: boolean }) {
  return (
    <svg
      data-slot="check-mark"
      data-circle={circle || undefined}
      data-animate={animate || undefined}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={circle ? 2 : 3}
      strokeLinecap="round"
      strokeLinejoin="round"
      width={24}
      height={24}
      aria-hidden
      // 与 lucide 图标一致不带尺寸类，按钮等容器的 [&_svg] 尺寸规则照常生效
      className={cn("mg-check-mark", className)}
      {...props}
    >
      {circle && <circle className="mg-check-mark-ring" cx="12" cy="12" r="10" pathLength={1} />}
      <path className="mg-check-mark-tick" d={circle ? "M7.5 12.5l3 3 6-6.5" : "M5 12.5l4.5 4.5L19 7.5"} pathLength={1} />
    </svg>
  );
}

export { CheckMark };
