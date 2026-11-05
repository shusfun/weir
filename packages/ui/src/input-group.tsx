// 来源 codedthemes/uiable src/components/ui/input-group.tsx。去掉 use client，改为本地引用以适配 Vite。
import { ComponentProps } from "react";

import { Button } from "./button";
import { Input } from "./input";
import { Textarea } from "./textarea";

import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";
import { SearchIcon } from "lucide-react";

function InputGroup({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="input-group"
      role="group"
      className={cn(
        // 外框画底色、细边框与焦点描边，里面的输入框透明；焦点只认输入框本身（组里的按钮聚焦时画按钮自己的焦点环）
        "weir-input group/input-group relative flex h-8 w-full min-w-0 items-center transition-colors touch:h-11 has-[[data-slot=input-group-control]:focus]:outline-2 has-[[data-slot=input-group-control]:focus]:outline-offset-0 has-[[data-slot=input-group-control]:focus]:outline-[var(--weir-focus)] has-disabled:opacity-50 has-[[data-slot][aria-invalid=true]]:border-destructive has-[>[data-align=block-end]]:h-auto has-[>[data-align=block-end]]:flex-col has-[>[data-align=block-start]]:h-auto has-[>[data-align=block-start]]:flex-col has-[>textarea]:h-auto has-[>[data-align=block-end]]:[&>input]:pt-3 has-[>[data-align=block-start]]:[&>input]:pb-3 has-[>[data-align=inline-end]]:[&>input]:pr-1 has-[>[data-align=inline-start]]:[&>input]:pl-1",
        className,
      )}
      {...props}
    />
  );
}

const inputGroupAddonVariants = cva(
  "flex h-auto cursor-text items-center justify-center gap-2 text-body font-medium text-[var(--weir-ink-3)] select-none group-data-[disabled=true]/input-group:opacity-50 [&>svg:not([class*='size-'])]:size-4",
  {
    variants: {
      align: {
        "inline-start":
          "order-first pl-3 has-[>button]:pl-1",
        "inline-end":
          "order-last pr-3 has-[>button]:pr-0.5 touch:has-[>button]:pr-0",
        "block-start":
          "order-first w-full justify-start px-3 pt-3 group-has-[>input]/input-group:pt-2 [.border-b]:pb-3",
        "block-end":
          "order-last w-full justify-start px-3 pb-3 group-has-[>input]/input-group:pb-2 [.border-t]:pt-3",
      },
    },
    defaultVariants: {
      align: "inline-start",
    },
  },
);

function InputGroupAddon({
  className,
  align = "inline-start",
  ...props
}: ComponentProps<"div"> & VariantProps<typeof inputGroupAddonVariants>) {
  return (
    <div
      role="group"
      data-slot="input-group-addon"
      data-align={align}
      className={cn(inputGroupAddonVariants({ align }), className)}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("button")) {
          return;
        }
        e.currentTarget.parentElement?.querySelector("input")?.focus();
      }}
      {...props}
    />
  );
}

// 组内按钮的尺寸直接对应 Button 的尺寸：桌面比输入框（32）矮一档，触屏 icon-sm 与输入框同为 44
const inputGroupButtonSize = { xs: "xs", sm: "sm", "icon-xs": "icon-xs", "icon-sm": "icon-sm" } as const;
const inputGroupButtonVariants = cva("rounded-[var(--weir-radius-sm)] shadow-none", {
  variants: { size: { xs: "", sm: "", "icon-xs": "", "icon-sm": "" } },
  defaultVariants: { size: "xs" },
});

function InputGroupButton({
  className,
  type = "button",
  variant = "ghost",
  size = "xs",
  ...props
}: Omit<ComponentProps<typeof Button>, "size" | "type"> &
  VariantProps<typeof inputGroupButtonVariants> & {
    type?: "button" | "submit" | "reset";
  }) {
  return (
    <Button
      type={type}
      data-size={size}
      variant={variant}
      size={inputGroupButtonSize[size ?? "xs"]}
      className={cn(inputGroupButtonVariants({ size }), className)}
      {...props}
    />
  );
}

function InputGroupText({ className, ...props }: ComponentProps<"span">) {
  return (
    <span
      className={cn(
        "flex items-center gap-2 text-body text-[var(--weir-ink-3)] [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      {...props}
    />
  );
}

function InputGroupInput({ className, ...props }: ComponentProps<"input">) {
  return (
    <Input
      data-slot="input-group-control"
      className={cn(
        // 盖住外框上下各 1px 的透明边框：命中区与外框同高（触屏 44）
        "-my-px h-[calc(100%+2px)] flex-1 rounded-none border-0 bg-transparent py-0 focus:outline-none read-only:bg-transparent disabled:bg-transparent touch:h-[calc(100%+2px)]",
        className,
      )}
      {...props}
    />
  );
}

function InputGroupTextarea({ className, ...props }: ComponentProps<"textarea">) {
  return (
    <Textarea
      data-slot="input-group-control"
      className={cn(
        "flex-1 resize-none rounded-none border-0 bg-transparent py-2 focus:outline-none read-only:bg-transparent disabled:bg-transparent",
        className,
      )}
      {...props}
    />
  );
}

// 列表 / 工具栏搜索框：前置放大镜图标，默认 280px，窄屏撑满。className 作用于外层组。
function SearchInput({ className, ...props }: ComponentProps<"input">) {
  return (
    <InputGroup className={cn("w-[280px] max-w-full max-sm:w-full", className)}>
      <InputGroupAddon>
        <SearchIcon className="text-[var(--weir-ink-3)]" />
      </InputGroupAddon>
      <InputGroupInput {...props} />
    </InputGroup>
  );
}

export {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupText,
  InputGroupInput,
  InputGroupTextarea,
  SearchInput,
};
