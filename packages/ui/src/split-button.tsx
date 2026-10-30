// 分体按钮：左边是最常用的操作（如复制订阅地址，可以直接放 CopyButton，保留它的「已复制」反馈），
// 右边的三角展开更多操作。两半拼成一块、各自按下有水波，菜单从三角处弹出。参照 Element Plus 的 Dropdown split-button。
import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "./overlay";

type SplitItem =
  | { label: ReactNode; icon?: ReactNode; onSelect: () => void; danger?: boolean; disabled?: boolean }
  | "separator";

function SplitButton({
  main,
  items,
  size = "sm",
  variant = "outline",
  disabled,
  menuLabel = "更多操作",
  className,
}: {
  // main：左边的按钮，用同样的 size / variant
  main: ReactNode;
  items: SplitItem[];
  size?: "sm" | "default";
  variant?: "outline" | "default";
  disabled?: boolean;
  menuLabel?: string;
  className?: string;
}) {
  return (
    <span data-slot="split-button" className={cn("inline-flex items-stretch [&>[data-slot=button]:first-child]:rounded-r-none", className)}>
      {main}
      <Menu>
        <MenuTrigger
          render={
            <Button
              size={size === "sm" ? "icon-sm" : "icon"}
              variant={variant}
              ripple
              disabled={disabled}
              aria-label={menuLabel}
              className="-ml-px w-7 rounded-l-none [&_svg]:transition-transform [&_svg]:duration-[var(--weir-fast)] data-[popup-open]:[&_svg]:rotate-180"
            />
          }
        >
          <ChevronDown />
        </MenuTrigger>
        <MenuContent align="end">
          {items.map((it, i) =>
            it === "separator" ? (
              <MenuSeparator key={i} />
            ) : (
              <MenuItem key={i} disabled={it.disabled} onClick={it.onSelect} className={it.danger ? "text-[var(--weir-bad-ink)] [&_svg]:!text-[var(--weir-bad-ink)]" : undefined}>
                {it.icon}
                {it.label}
              </MenuItem>
            ),
          )}
        </MenuContent>
      </Menu>
    </span>
  );
}

export { SplitButton };
export type { SplitItem };
