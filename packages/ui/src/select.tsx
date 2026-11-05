"use client";

import { ComponentProps } from "react";

// third-party
import { Select as SelectPrimitive } from "@base-ui/react/select";
import { cn } from "cn";
import { ScrollArea } from "./scroll-area";

// assets
import { CheckIcon, ChevronDownIcon, ChevronUpIcon } from "lucide-react";

const Select = SelectPrimitive.Root;

function SelectGroup({ className, ...props }: SelectPrimitive.Group.Props) {
  return (
    <SelectPrimitive.Group
      data-slot="select-group"
      className={cn("scroll-my-1 p-4", className)}
      {...props}
    />
  );
}

function SelectValue({ className, ...props }: SelectPrimitive.Value.Props) {
  return (
    <SelectPrimitive.Value
      data-slot="select-value"
      className={cn("block min-w-0 flex-1 truncate text-left", className)}
      {...props}
    />
  );
}

function SelectTrigger({
  className,
  size = "default",
  children,
  ...props
}: SelectPrimitive.Trigger.Props & {
  size?: "sm" | "default";
}) {
  return (
    <SelectPrimitive.Trigger
      data-slot="select-trigger"
      data-size={size}
      className={cn(
        // 在 Field 内默认撑满一行；工具栏等独立场景保持内容宽度
        // 与输入框同高（桌面 32、触屏 44）、同样白底细边框；按下与展开中都叠一层填充（.weir-key）
        "weir-key weir-input flex h-8 w-fit min-w-0 items-center justify-between gap-2 rounded-[var(--weir-radius)] px-3 text-control whitespace-nowrap text-[var(--weir-ink)] select-none touch:h-11 in-data-[slot=field]:w-full disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive data-placeholder:text-[var(--weir-ink-3)] data-[size=sm]:h-7 touch:data-[size=sm]:h-11 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      {...props}
    >
      {children}
      <SelectPrimitive.Icon
        render={
          <ChevronDownIcon className="pointer-events-none size-4 text-[var(--weir-ink-3)]" />
        }
      />
    </SelectPrimitive.Trigger>
  );
}

function SelectContent({
  className,
  children,
  side = "bottom",
  sideOffset = 4,
  align = "center",
  alignOffset = 0,
  alignItemWithTrigger = true,
  ...props
}: SelectPrimitive.Popup.Props &
  Pick<
    SelectPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset" | "alignItemWithTrigger"
  >) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Positioner
        side={side}
        sideOffset={sideOffset}
        align={align}
        alignOffset={alignOffset}
        alignItemWithTrigger={alignItemWithTrigger}
        className="isolate z-50"
      >
        <SelectPrimitive.Popup
          data-slot="select-content"
          data-align-trigger={alignItemWithTrigger}
          className={cn(
            // 弹出：从触发键处按轻快弹簧放大并淡入，退场只淡出
            "relative isolate z-50 flex max-h-(--available-height) w-(--anchor-width) min-w-36 origin-(--transform-origin) flex-col overflow-hidden rounded-[var(--weir-radius-card)] border border-[var(--weir-line-soft)] bg-popover p-1 text-popover-foreground shadow-[0_8px_28px_-6px_var(--weir-shade-strong)] [transition:opacity_var(--weir-mid)_var(--weir-ease),scale_var(--weir-spring-dur)_var(--weir-spring)] data-[starting-style]:scale-95 data-[starting-style]:opacity-0 data-[ending-style]:opacity-0 data-[ending-style]:[transition-duration:var(--weir-fast)]",
            className,
          )}
          {...props}
        >
          <SelectScrollUpButton />
          {/* 列表本身就是滚动元素（Base UI 据它对齐选中项、显示滚动箭头），外面套覆盖式滚动条 */}
          <ScrollArea className="min-h-0 flex-auto" viewportRender={<SelectPrimitive.List />} contentRender={false}>
            {children}
          </ScrollArea>
          <SelectScrollDownButton />
        </SelectPrimitive.Popup>
      </SelectPrimitive.Positioner>
    </SelectPrimitive.Portal>
  );
}

function SelectLabel({
  className,
  ...props
}: SelectPrimitive.GroupLabel.Props) {
  return (
    <SelectPrimitive.GroupLabel
      data-slot="select-label"
      className={cn("px-3 py-1 text-meta text-[var(--weir-ink-3)]", className)}
      {...props}
    />
  );
}

function SelectItem({
  className,
  children,
  ...props
}: SelectPrimitive.Item.Props) {
  return (
    <SelectPrimitive.Item
      data-slot="select-item"
      className={cn(
        // 行高桌面 32、触屏 44；高亮（悬停 / 方向键）叠浅填充，按下再深一档；选中项右侧打勾
        "weir-press-fill relative flex min-h-8 w-full cursor-default items-center gap-2 rounded-[var(--weir-radius-sm)] py-1 pr-8 pl-3 text-body outline-hidden select-none touch:min-h-11 data-[highlighted]:bg-[var(--weir-fill-3)] active:bg-[var(--weir-fill-2)] data-[highlighted]:active:bg-[var(--weir-fill-2)] data-[selected]:font-semibold data-disabled:pointer-events-none data-disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 *:[span]:last:flex *:[span]:last:items-center *:[span]:last:gap-2",
        className,
      )}
      {...props}
    >
      <SelectPrimitive.ItemText className="flex flex-1 shrink-0 gap-2 whitespace-nowrap">
        {children}
      </SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator
        render={
          <span className="pointer-events-none absolute right-2 flex size-4 items-center justify-center text-[var(--weir-accent-text)]" />
        }
      >
        <CheckIcon className="pointer-events-none" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}

function SelectSeparator({
  className,
  ...props
}: SelectPrimitive.Separator.Props) {
  return (
    <SelectPrimitive.Separator
      data-slot="select-separator"
      className={cn("pointer-events-none mx-2 my-1 h-px bg-[var(--weir-line-soft)]", className)}
      {...props}
    />
  );
}

function SelectScrollUpButton({
  className,
  ...props
}: ComponentProps<typeof SelectPrimitive.ScrollUpArrow>) {
  return (
    <SelectPrimitive.ScrollUpArrow
      data-slot="select-scroll-up-button"
      className={cn(
        "top-0 z-10 flex w-full cursor-default items-center justify-center bg-popover py-1 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      {...props}
    >
      <ChevronUpIcon />
    </SelectPrimitive.ScrollUpArrow>
  );
}

function SelectScrollDownButton({
  className,
  ...props
}: ComponentProps<typeof SelectPrimitive.ScrollDownArrow>) {
  return (
    <SelectPrimitive.ScrollDownArrow
      data-slot="select-scroll-down-button"
      className={cn(
        "bottom-0 z-10 flex w-full cursor-default items-center justify-center bg-popover py-1 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      {...props}
    >
      <ChevronDownIcon />
    </SelectPrimitive.ScrollDownArrow>
  );
}

export {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectScrollDownButton,
  SelectScrollUpButton,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
};
