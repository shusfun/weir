// 路由型标签：每个标签是一个真实地址，可收藏、可后退；当前项由 NavLink 标记 aria-current。
// 外观复用 @migate/ui 的标签样式（tabListClass / tabTriggerClass）。
import { NavLink } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import { tabListClass, tabTriggerClass } from "@migate/ui";

export function TabNav({
  items,
  label,
}: {
  items: readonly { to: string; label: string; icon?: LucideIcon; end?: boolean }[];
  label: string;
}) {
  return (
    <nav className={tabListClass} aria-label={label}>
      {items.map(({ to, label: text, icon: Icon, end }) => (
        <NavLink key={to} to={to} end={end} className={tabTriggerClass}>
          {Icon && <Icon />}
          {text}
        </NavLink>
      ))}
    </nav>
  );
}
