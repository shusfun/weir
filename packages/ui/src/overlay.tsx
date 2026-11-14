// Weir 浮层：菜单、气泡、提示、侧边面板。浮层是圆角 12 的抬高面板，从触发处按轻快弹簧放大并淡入，退场只淡出；不使用模糊。
import { Menu as MenuPrimitive } from "@base-ui/react/menu";
import { Popover as PopoverPrimitive } from "@base-ui/react/popover";
import { Tooltip, TooltipProvider } from "./tooltip";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { cn } from "cn";
import { XIcon } from "lucide-react";
import { OverlayBody, ActionGroup } from "./overlay-layout";
import { Button } from "./button";
import type { ReactNode } from "react";

const floating =
  "z-50 min-w-44 rounded-[var(--weir-radius-card)] border border-[var(--weir-line-soft)] bg-popover p-1 text-popover-foreground shadow-[0_8px_28px_-6px_var(--weir-shade-strong)] outline-none [transition:opacity_var(--weir-mid)_var(--weir-ease),scale_var(--weir-spring-dur)_var(--weir-spring)] origin-(--transform-origin) data-[starting-style]:scale-95 data-[starting-style]:opacity-0 data-[ending-style]:opacity-0 data-[ending-style]:[transition-duration:var(--weir-fast)]";

const Menu = MenuPrimitive.Root;
const MenuTrigger = MenuPrimitive.Trigger;

function MenuContent({
  className,
  side = "bottom",
  align = "end",
  sideOffset = 6,
  ...props
}: MenuPrimitive.Popup.Props & { side?: "top" | "bottom" | "left" | "right"; align?: "start" | "center" | "end"; sideOffset?: number }) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Positioner side={side} align={align} sideOffset={sideOffset} className="z-50">
        <MenuPrimitive.Popup data-slot="menu-content" className={cn(floating, className)} {...props} />
      </MenuPrimitive.Positioner>
    </MenuPrimitive.Portal>
  );
}

function MenuItem({ className, ...props }: MenuPrimitive.Item.Props) {
  return (
    <MenuPrimitive.Item
      data-slot="menu-item"
      className={cn(
        // 行高桌面 32、触屏 44；高亮（悬停 / 方向键）叠浅填充，按下再深一档
        "weir-press-fill flex min-h-8 cursor-default items-center gap-2 rounded-[var(--weir-radius-sm)] px-3 text-body outline-none select-none touch:min-h-11 data-[highlighted]:bg-[var(--weir-fill-3)] active:bg-[var(--weir-fill-2)] data-[highlighted]:active:bg-[var(--weir-fill-2)] data-disabled:opacity-50 [&_svg]:size-4 [&_svg]:text-[var(--weir-ink-3)]",
        className,
      )}
      {...props}
    />
  );
}

function MenuLabel({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("weir-engraved px-3 pt-2 pb-1", className)}>{children}</div>;
}

function MenuSeparator({ className }: { className?: string }) {
  return <MenuPrimitive.Separator className={cn("mx-2 my-1 h-px bg-[var(--weir-line-soft)]", className)} />;
}

const Popover = PopoverPrimitive.Root;
const PopoverTrigger = PopoverPrimitive.Trigger;

function PopoverContent({
  className,
  align = "end",
  sideOffset = 8,
  ...props
}: PopoverPrimitive.Popup.Props & { align?: "start" | "center" | "end"; sideOffset?: number }) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Positioner align={align} sideOffset={sideOffset} className="z-50">
        <PopoverPrimitive.Popup data-slot="popover-content" className={cn(floating, "w-80 p-0", className)} {...props} />
      </PopoverPrimitive.Positioner>
    </PopoverPrimitive.Portal>
  );
}

// 侧边面板：移动导航与宽编辑面板。只位移 transform，按轻快弹簧滑入，不改变尺寸。
function Sheet(props: DialogPrimitive.Root.Props) {
  return <DialogPrimitive.Root data-slot="sheet" {...props} />;
}
const SheetClose = DialogPrimitive.Close;

function SheetContent({
  side = "left",
  title,
  description,
  className,
  close,
  finalFocus,
  footer,
  children,
}: {
  side?: "left" | "right";
  title: ReactNode;
  description?: ReactNode;
  className?: string;
  // 自定义关闭键（如与触发键同位的 MenuToggle）；传入后不再渲染右上角的默认关闭键
  close?: ReactNode;
  // 关闭后焦点落点：触发键不是 Sheet 的 Trigger（如在顶栏里单独控制 open）时传入它的 ref
  finalFocus?: DialogPrimitive.Popup.Props["finalFocus"];
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-[var(--weir-scrim)] transition-opacity duration-[var(--weir-mid)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0" />
      <DialogPrimitive.Popup
        data-slot="sheet-content"
        data-tour="dialog"
        finalFocus={finalFocus}
        data-side={side}
        className={cn(
          "fixed inset-y-0 z-50 flex w-[min(var(--weir-dialog-sm),var(--weir-sheet-width))] flex-col gap-[var(--weir-space-4)] overflow-hidden border-[var(--weir-line-soft)] bg-[var(--weir-panel)] p-[var(--weir-dialog-padding)] shadow-[0_0_30px_-10px_var(--weir-shade-strong)] outline-none transition-transform duration-[var(--weir-spring-dur)] ease-[var(--weir-spring)] data-[ending-style]:duration-[var(--weir-slow)] data-[ending-style]:ease-[var(--weir-ease-in)] data-[side=left]:left-0 data-[side=left]:border-r data-[side=right]:right-0 data-[side=right]:border-l data-[side=left]:data-[starting-style]:-translate-x-full data-[side=left]:data-[ending-style]:-translate-x-full data-[side=right]:data-[starting-style]:translate-x-full data-[side=right]:data-[ending-style]:translate-x-full",
          className,
        )}
      >
        <div data-slot="sheet-header" className="flex shrink-0 items-start justify-between gap-[var(--weir-space-3)]">
          <div className="min-w-0">
            <DialogPrimitive.Title className="text-dialog font-semibold">{title}</DialogPrimitive.Title>
            {description && (
              <DialogPrimitive.Description className="mt-1 text-meta text-[var(--weir-ink-3)]">{description}</DialogPrimitive.Description>
            )}
          </div>
          {close ?? (
            <DialogPrimitive.Close
              aria-label="关闭"
              render={<Button variant="ghost" size="icon-sm" />}
            >
              <XIcon />
            </DialogPrimitive.Close>
          )}
        </div>
        <OverlayBody className="flex-1">{children}</OverlayBody>
        {footer && <ActionGroup align="end" className="weir-overlay-footer">{footer}</ActionGroup>}
      </DialogPrimitive.Popup>
    </DialogPrimitive.Portal>
  );
}

export {
  Menu, MenuTrigger, MenuContent, MenuItem, MenuLabel, MenuSeparator,
  Popover, PopoverTrigger, PopoverContent,
  TooltipProvider, Tooltip,
  Sheet, SheetContent, SheetClose,
};
