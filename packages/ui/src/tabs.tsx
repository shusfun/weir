// MiGate 页内标签：刻线底边，选中项点亮下沿。只过渡颜色，不移动指示器。
import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import { cn } from "cn";

function Tabs({ className, ...props }: TabsPrimitive.Root.Props) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn("flex min-w-0 flex-col gap-4", className)} {...props} />;
}

// 标签条与标签项的样式同时导出：路由型标签（每项是一个链接）复用同一外观，不另写一套 CSS。
// 底部刻线用内阴影绘制、标签不再用 -mb-px 压线：否则内容比容器高 1px，overflow 会冒出纵向滚动条。
// 窄屏标签过多时仍可横向滑动，但不显示滚动条。
export const tabListClass =
  "flex min-w-0 items-end gap-0.5 overflow-x-auto overflow-y-hidden shadow-[inset_0_-1px_0_var(--mg-line),0_1px_0_var(--mg-highlight)] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden";

function TabsList({ className, ...props }: TabsPrimitive.List.Props) {
  return <TabsPrimitive.List data-slot="tabs-list" className={cn(tabListClass, className)} {...props} />;
}

export const tabTriggerClass =
  "relative inline-flex h-10 shrink-0 items-center gap-2 border-b-2 border-transparent px-3 text-[13px] font-medium whitespace-nowrap text-[var(--mg-ink-3)] no-underline outline-none transition-colors duration-[var(--mg-fast)] hover:text-[var(--mg-ink)] focus-visible:rounded-t-md focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset aria-selected:border-[var(--mg-accent-text)] aria-selected:text-[var(--mg-ink)] aria-[current=page]:border-[var(--mg-accent-text)] aria-[current=page]:text-[var(--mg-ink)] [&_svg]:size-4";

function TabsTab({ className, ...props }: TabsPrimitive.Tab.Props) {
  return <TabsPrimitive.Tab data-slot="tabs-tab" className={cn(tabTriggerClass, className)} {...props} />;
}

function TabsPanel({ className, ...props }: TabsPrimitive.Panel.Props) {
  return <TabsPrimitive.Panel data-slot="tabs-panel" className={cn("min-w-0 outline-none", className)} {...props} />;
}

export { Tabs, TabsList, TabsTab, TabsPanel };
