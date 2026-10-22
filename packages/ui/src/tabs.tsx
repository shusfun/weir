// MiGate 页内标签：刻线底边，选中项点亮下沿。只过渡颜色，不移动指示器。
import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import { cn } from "cn";

function Tabs({ className, ...props }: TabsPrimitive.Root.Props) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn("flex min-w-0 flex-col gap-4", className)} {...props} />;
}

// 标签条与标签项的样式同时导出：路由型标签（每项是一个链接）复用同一外观，不另写一套 CSS
export const tabListClass =
  "flex min-w-0 items-end gap-1 overflow-x-auto border-b border-[var(--mg-line)] shadow-[0_1px_0_var(--mg-highlight)] [scrollbar-width:thin]";

function TabsList({ className, ...props }: TabsPrimitive.List.Props) {
  return <TabsPrimitive.List data-slot="tabs-list" className={cn(tabListClass, className)} {...props} />;
}

export const tabTriggerClass =
  "relative -mb-px inline-flex h-10 shrink-0 items-center gap-2 border-b-2 border-transparent px-3 text-[13px] font-medium whitespace-nowrap text-[var(--mg-ink-3)] no-underline outline-none transition-colors duration-[var(--mg-fast)] hover:text-[var(--mg-ink)] focus-visible:ring-3 focus-visible:ring-ring aria-selected:border-[var(--mg-accent-text)] aria-selected:text-[var(--mg-ink)] aria-[current=page]:border-[var(--mg-accent-text)] aria-[current=page]:text-[var(--mg-ink)] [&_svg]:size-4";

function TabsTab({ className, ...props }: TabsPrimitive.Tab.Props) {
  return <TabsPrimitive.Tab data-slot="tabs-tab" className={cn(tabTriggerClass, className)} {...props} />;
}

function TabsPanel({ className, ...props }: TabsPrimitive.Panel.Props) {
  return <TabsPrimitive.Panel data-slot="tabs-panel" className={cn("min-w-0 outline-none", className)} {...props} />;
}

export { Tabs, TabsList, TabsTab, TabsPanel };
