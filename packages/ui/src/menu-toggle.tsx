// 汉堡变叉：同一个 SVG 里的三条线，打开时上下两条先并到中线再各自转 45°，中线收起；关闭时倒序走回。
// 用户在切换过程中始终看着同一个元件，不会丢失位置。只是图标，语义由外层按钮负责
// （aria-expanded / aria-label）。
// follow：放在弹窗 / 抽屉里时不传 open，跟随最近的 Base UI 弹层状态——
// 弹层刚挂载（data-starting-style）是三条线，入场后变叉，退场（data-closed）时变回三条线。
import type { ComponentProps } from "react";
import { cn } from "cn";

function MenuToggle({
  open = false,
  follow = false,
  className,
  ...props
}: Omit<ComponentProps<"svg">, "children"> & { open?: boolean; follow?: boolean }) {
  return (
    <svg
      data-slot="menu-toggle"
      data-open={(!follow && open) || undefined}
      data-follow={follow || undefined}
      viewBox="0 0 24 24"
      width={24}
      height={24}
      fill="currentColor"
      aria-hidden
      className={cn("mg-menu-toggle", className)}
      {...props}
    >
      <rect className="mg-menu-toggle-top" x="4" y="5" width="16" height="2" rx="1" />
      <rect className="mg-menu-toggle-mid" x="4" y="11" width="16" height="2" rx="1" />
      <rect className="mg-menu-toggle-bottom" x="4" y="17" width="16" height="2" rx="1" />
    </svg>
  );
}

export { MenuToggle };
