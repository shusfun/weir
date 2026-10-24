// Weir 浮层：菜单、气泡、提示、侧边面板。浮层统一为凸起面板，仅淡入，不使用模糊。
import { Menu as MenuPrimitive } from "@base-ui/react/menu";
import { Popover as PopoverPrimitive } from "@base-ui/react/popover";
import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { cn } from "cn";
import { XIcon } from "lucide-react";
import type { ReactNode } from "react";

const floating =
  "z-50 min-w-44 rounded-[var(--mg-radius)] border border-[var(--mg-line)] bg-popover p-1 text-popover-foreground shadow-[inset_0_1px_0_var(--mg-highlight),0_10px_24px_-8px_var(--mg-shade-strong)] outline-none transition-[opacity,transform] duration-[var(--mg-fast)] origin-(--transform-origin) data-[starting-style]:scale-[0.98] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0";

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
        "flex min-h-8 cursor-default items-center gap-2 rounded-[var(--mg-radius-sm)] px-2.5 text-[13px] outline-none select-none data-[highlighted]:bg-[var(--mg-accent-soft)] data-[highlighted]:text-[var(--mg-accent-soft-ink)] data-disabled:opacity-50 [&_svg]:size-4 [&_svg]:text-[var(--mg-ink-3)]",
        className,
      )}
      {...props}
    />
  );
}

function MenuLabel({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("mg-engraved px-2.5 pt-2 pb-1", className)}>{children}</div>;
}

function MenuSeparator({ className }: { className?: string }) {
  return <MenuPrimitive.Separator className={cn("mx-1 my-1 h-px bg-[var(--mg-line)]", className)} />;
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

const TooltipProvider = TooltipPrimitive.Provider;

function Tooltip({
  content,
  children,
  side = "right",
}: {
  content: ReactNode;
  children: TooltipPrimitive.Trigger.Props["render"];
  side?: "top" | "right" | "bottom" | "left";
}) {
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger render={children} />
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Positioner side={side} sideOffset={8} className="z-50">
          <TooltipPrimitive.Popup className="rounded-[var(--mg-radius-sm)] border border-[var(--mg-line)] bg-[var(--mg-ink)] px-2 py-1 text-xs text-[var(--mg-base)] shadow-[0_4px_10px_-4px_var(--mg-shade-strong)] transition-opacity duration-[var(--mg-fast)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0">
            {content}
          </TooltipPrimitive.Popup>
        </TooltipPrimitive.Positioner>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}

// 侧边面板：移动导航与宽编辑面板。只位移 transform，不改变尺寸。
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
}) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-[rgb(20_18_14/0.38)] transition-opacity duration-[var(--mg-mid)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0 dark:bg-[rgb(0_0_0/0.6)]" />
      <DialogPrimitive.Popup
        data-slot="sheet-content"
        finalFocus={finalFocus}
        data-side={side}
        className={cn(
          "fixed inset-y-0 z-50 flex w-[min(320px,88vw)] flex-col gap-4 overflow-hidden border-[var(--mg-line)] bg-[var(--mg-panel)] p-5 shadow-[0_0_30px_-10px_var(--mg-shade-strong)] outline-none transition-transform duration-[var(--mg-mid)] ease-[var(--mg-ease)] data-[side=left]:left-0 data-[side=left]:border-r data-[side=right]:right-0 data-[side=right]:border-l data-[side=left]:data-[starting-style]:-translate-x-full data-[side=left]:data-[ending-style]:-translate-x-full data-[side=right]:data-[starting-style]:translate-x-full data-[side=right]:data-[ending-style]:translate-x-full",
          className,
        )}
      >
        <div data-slot="sheet-header" className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <DialogPrimitive.Title className="text-base font-semibold">{title}</DialogPrimitive.Title>
            {description && (
              <DialogPrimitive.Description className="mt-1 text-xs text-[var(--mg-ink-3)]">{description}</DialogPrimitive.Description>
            )}
          </div>
          {close ?? (
            <DialogPrimitive.Close
              aria-label="关闭"
              className="inline-flex size-8 items-center justify-center rounded-[var(--mg-radius-sm)] text-[var(--mg-ink-3)] hover:bg-[var(--mg-well)] focus-visible:ring-3 focus-visible:ring-ring"
            >
              <XIcon className="size-4" />
            </DialogPrimitive.Close>
          )}
        </div>
        <div className="mg-focus-gutter min-h-0 flex-1 overflow-y-auto">{children}</div>
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
