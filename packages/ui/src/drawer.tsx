// 底部抽屉：基于 Base UI Drawer，支持拖动把手下滑关闭。入场关键帧在升到位后停顿一拍，
// 内容再落稳——状态靠停住被读出来；拖动时面板跟手（transform），入场只用 translate，两者互不干扰。
// 退场时长随下滑力度变化（--drawer-swipe-strength）。样式见 styles/motion.css 的 .mg-drawer-*。
import { Drawer as DrawerPrimitive } from "@base-ui/react/drawer";
import type { ReactNode } from "react";
import { cn } from "cn";

function Drawer(props: DrawerPrimitive.Root.Props) {
  return <DrawerPrimitive.Root data-slot="drawer" {...props} />;
}

const DrawerTrigger = DrawerPrimitive.Trigger;
const DrawerClose = DrawerPrimitive.Close;

function DrawerContent({
  title,
  description,
  footer,
  className,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  // 底部操作区（按钮），固定在抽屉底部不随内容滚动
  footer?: ReactNode;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <DrawerPrimitive.Portal>
      <DrawerPrimitive.Backdrop className="mg-drawer-backdrop" />
      <DrawerPrimitive.Viewport className="mg-drawer-viewport">
        <DrawerPrimitive.Popup data-slot="drawer-content" className={cn("mg-drawer-popup", className)}>
          <div className="mg-drawer-handle" aria-hidden />
          <DrawerPrimitive.Content className="mg-drawer-content">
            <DrawerPrimitive.Title className="text-[17px] font-semibold tracking-[-0.015em] text-[var(--mg-ink)]">{title}</DrawerPrimitive.Title>
            {description && (
              <DrawerPrimitive.Description className="mt-1 text-[13px] leading-relaxed text-[var(--mg-ink-3)]">{description}</DrawerPrimitive.Description>
            )}
            {children && <div className="mg-drawer-body">{children}</div>}
            {footer && <div className="mg-drawer-footer">{footer}</div>}
          </DrawerPrimitive.Content>
        </DrawerPrimitive.Popup>
      </DrawerPrimitive.Viewport>
    </DrawerPrimitive.Portal>
  );
}

export { Drawer, DrawerTrigger, DrawerClose, DrawerContent };
