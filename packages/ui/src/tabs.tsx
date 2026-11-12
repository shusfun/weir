// Weir 页内标签：底部细分隔线，选中项下沿为主色。按下缩小并叠填充色（.weir-press），只过渡颜色与缩放，不移动指示器。
import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import { cn } from "cn";

function Tabs({ className, ...props }: TabsPrimitive.Root.Props) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn("flex min-w-0 flex-col gap-4", className)} {...props} />;
}

// 标签条与标签项的样式同时导出：路由型标签（每项是一个链接）复用同一外观，不另写一套 CSS。
// 底部分隔线用内阴影绘制、标签不再用 -mb-px 压线：否则内容比容器高 1px，overflow 会冒出纵向滚动条。
// 窄屏标签过多时仍可横向滑动，但不显示滚动条。标签条横向可滚动，焦点环画在标签内侧，免得被裁。
export const tabListClass =
  "weir-tabs-list flex min-w-0 items-end overflow-x-auto overflow-y-hidden shadow-[inset_0_-1px_0_var(--weir-line-soft)] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden";

function TabsList({ className, ...props }: TabsPrimitive.List.Props) {
  return <TabsPrimitive.List data-slot="tabs-list" className={cn(tabListClass, className)} {...props} />;
}

export const tabTriggerClass =
  "weir-tabs-tab weir-press relative inline-flex shrink-0 items-center justify-center gap-2 border-b-2 border-transparent text-body whitespace-nowrap text-[var(--weir-ink-3)] no-underline hover:text-[var(--weir-ink)] focus-visible:outline-offset-[-2px] aria-selected:border-[var(--weir-accent)] aria-selected:font-semibold aria-selected:text-[var(--weir-ink)] aria-[current=page]:border-[var(--weir-accent)] aria-[current=page]:font-semibold aria-[current=page]:text-[var(--weir-ink)] [&_svg]:size-4";

function TabsTab({ className, ...props }: TabsPrimitive.Tab.Props) {
  return <TabsPrimitive.Tab data-slot="tabs-tab" className={cn(tabTriggerClass, className)} {...props} />;
}

// 面板保留 Base UI 默认的 tabindex=0：面板里没有可聚焦元素时，键盘用户靠它进入内容、用方向键滚动。
// 聚焦时画主色内描边（外描边会被页面或滚动区裁掉）。
function TabsPanel({ className, ...props }: TabsPrimitive.Panel.Props) {
  return (
    <TabsPrimitive.Panel
      data-slot="tabs-panel"
      data-tour="tab-panel"
      className={cn("min-w-0 rounded-[var(--weir-radius-sm)] focus-visible:outline-offset-[-2px]", className)}
      {...props}
    />
  );
}

export { Tabs, TabsList, TabsTab, TabsPanel };
