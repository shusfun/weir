// 展示类小部件：图标方块 / 头像、代码与指纹块、可选卡片、勾选行、步骤条。
// 业务页面只组合这些部件，不再各自手写圆角凹槽、选中描边与步骤编号样式。
import { type ComponentProps, type ReactElement, type ReactNode } from "react";
import { useRender } from "@base-ui/react/use-render";
import { mergeProps } from "@base-ui/react/merge-props";
import { Check } from "lucide-react";
import { cn } from "cn";

// ---- 图标方块 / 头像 ----
const tileSizes = {
  sm: "size-8 rounded-[var(--weir-radius-sm)] text-meta font-semibold [&_svg]:size-4",
  md: "size-10 rounded-[var(--weir-radius)] [&_svg]:size-5",
  lg: "size-11 rounded-[var(--weir-radius-card)] [&_svg]:size-6",
  xl: "size-13 rounded-[var(--weir-radius-dialog)] [&_svg]:size-7",
} as const;
const tileTones = {
  // 填充底 + 强调色图标：安全方式、登录页头
  accent: "weir-well text-[var(--weir-accent-text)]",
  // 填充底 + 中性图标 / 文字：账户头像
  neutral: "weir-well text-[var(--weir-ink-2)]",
  // 浅强调底：公开页卡片（随页面主题色）
  soft: "bg-[var(--weir-accent-soft)] text-[var(--weir-accent-text)]",
} as const;

function IconTile({
  size = "md",
  tone = "accent",
  className,
  ...props
}: ComponentProps<"span"> & { size?: keyof typeof tileSizes; tone?: keyof typeof tileTones }) {
  return (
    <span
      data-slot="icon-tile"
      aria-hidden={props["aria-label"] ? undefined : true}
      className={cn("inline-grid shrink-0 place-items-center", tileTones[tone], tileSizes[size], className)}
      {...props}
    />
  );
}

// ---- 代码 / 指纹 / 恢复码 ----
function CodeBlock({ className, ...props }: ComponentProps<"code">) {
  return (
    <code
      data-slot="code-block"
      className={cn(
        "weir-well block px-3 py-2 font-mono text-meta break-all text-[var(--weir-ink-2)]",
        className,
      )}
      {...props}
    />
  );
}

// ---- 可选卡片：几张卡片里选一张（客户端、下载来源） ----
// 可点的卡片（render 成按钮 / 链接，或带 onClick）按下缩小并叠填充色
function ChoiceCard({
  selected = false,
  featured = false,
  render,
  className,
  children,
  ...props
}: useRender.ComponentProps<"div"> & { selected?: boolean; featured?: boolean }) {
  const pressable = !!render || !!props.onClick;
  return useRender({
    defaultTagName: "div",
    render,
    props: mergeProps<"div">(
      {
        children,
        className: cn(
          "relative flex min-w-0 flex-col gap-2 rounded-[var(--weir-radius-card)] border border-[var(--weir-line-soft)] bg-[var(--weir-panel)] p-4",
          pressable && "weir-press cursor-pointer",
          featured && "gap-3 border-[var(--weir-accent-text)] bg-[var(--weir-accent-soft)] p-5",
          selected && "border-[var(--weir-accent-text)] shadow-[0_0_0_1px_var(--weir-accent-text)]",
          className,
        ),
        "data-selected": selected || undefined,
      } as ComponentProps<"div">,
      props,
    ),
  });
}

// ---- 勾选行：列表里每行一个复选框 + 名称 / 说明。按下叠填充色，行间是细分隔线，行高不小于 44 ----
function OptionRow({
  inset = false,
  compact = false,
  className,
  children,
  ...props
}: ComponentProps<"label"> & { inset?: boolean; compact?: boolean }) {
  return (
    <label
      data-slot="option-row"
      className={cn(
        // 文字列只匹配不带 data-slot 的 span：复选框（Base UI 渲染为 span）不能被撑开
        "weir-press-fill relative flex min-w-0 cursor-pointer items-start gap-3 text-body wrap-anywhere [&>span:not([data-slot])]:grid [&>span:not([data-slot])]:min-w-0 [&>span:not([data-slot])]:flex-1 [&>span:not([data-slot])]:gap-1 [&>[data-slot=checkbox]]:mt-px [&_small]:block",
        compact ? "min-h-8 items-center touch:min-h-11" : "min-h-11 border-b border-[var(--weir-line-soft)] py-3",
        inset && "px-4",
        className,
      )}
      {...props}
    >
      {children}
    </label>
  );
}

// ---- 步骤条：初始化向导（纵向）与订阅流水线（横向导航）共用 ----
type StepStatus = "complete" | "current" | "upcoming";

function Steps({
  orientation = "vertical",
  className,
  ...props
}: ComponentProps<"ol"> & { orientation?: "vertical" | "horizontal" }) {
  return (
    <ol
      data-slot="steps"
      data-orientation={orientation}
      className={cn(
        "group/steps m-0 list-none p-0",
        orientation === "vertical"
          ? "grid gap-4 max-md:grid-flow-col max-md:auto-cols-fr max-md:gap-2"
          : "weir-segmented flex items-stretch gap-0.5 rounded-[var(--weir-radius)] p-0.5",
        className,
      )}
      {...props}
    />
  );
}

// render：横向导航时传入路由链接元素（如 <NavLink to=…/>）；separator 为横向步骤间的分隔图标；
// icon：用图标代替序号（完成后统一显示对勾）
function Step({
  index,
  label,
  caption,
  status,
  icon,
  render,
  separator,
}: {
  index: number;
  label: ReactNode;
  caption?: ReactNode;
  status: StepStatus;
  icon?: ReactNode;
  render?: ReactElement;
  separator?: ReactNode;
}) {
  const body = (
    <>
      <span
        aria-hidden
        className={cn(
          "grid size-6 shrink-0 place-items-center rounded-full border font-mono text-fine font-semibold group-data-[orientation=vertical]/steps:size-8 group-data-[orientation=vertical]/steps:text-meta [&_svg]:size-4",
          status === "current"
            ? "border-transparent bg-[var(--weir-accent)] text-[var(--weir-accent-ink)]"
            : status === "complete"
              ? "border-transparent bg-[var(--weir-ok-soft)] text-[var(--weir-ok-ink)]"
              : "border-[var(--weir-line)] bg-[var(--weir-panel)] text-[var(--weir-ink-3)]",
        )}
      >
        {status === "complete" ? <Check className="size-3.5" /> : (icon ?? index + 1)}
      </span>
      <span className="min-w-0">
        <strong className="block text-body font-medium whitespace-nowrap max-md:group-data-[orientation=vertical]/steps:text-meta">{label}</strong>
        {caption && <small className="block text-fine whitespace-nowrap max-md:hidden group-data-[orientation=vertical]/steps:text-meta">{caption}</small>}
      </span>
    </>
  );
  const itemClass = cn(
    "flex min-w-0 items-center gap-3 no-underline",
    // 纵向步骤在窄屏改为横排四列、文字居中
    "max-md:group-data-[orientation=vertical]/steps:flex-col max-md:group-data-[orientation=vertical]/steps:gap-1.5 max-md:group-data-[orientation=vertical]/steps:text-center",
    status === "current" ? "text-[var(--weir-ink)]" : status === "complete" ? "text-[var(--weir-ok-ink)]" : "text-[var(--weir-ink-3)]",
    // 横向：每步是可点击的标签，当前步点亮为选中色
    // 横向：分段控件同款，当前步是白色滑块；可点（render 为链接）时按下缩小并叠填充色。触屏行高 44
    render && "weir-press",
    "group-data-[orientation=horizontal]/steps:flex-[1_0_auto] group-data-[orientation=horizontal]/steps:min-h-10 group-data-[orientation=horizontal]/steps:rounded-[var(--weir-radius-9)] group-data-[orientation=horizontal]/steps:px-3 group-data-[orientation=horizontal]/steps:py-1 group-data-[orientation=horizontal]/steps:hover:text-[var(--weir-ink)] touch:group-data-[orientation=horizontal]/steps:min-h-11 max-md:group-data-[orientation=horizontal]/steps:px-2",
    status === "current" &&
      "group-data-[orientation=horizontal]/steps:bg-[var(--weir-key-selected)] group-data-[orientation=horizontal]/steps:shadow-[0_0_0_0.5px_var(--weir-key-selected-ring),0_3px_8px_var(--weir-key-selected-glow)]",
    "focus-visible:outline-offset-[-2px]",
  );
  const item = useRender({
    defaultTagName: "span",
    render,
    props: { className: itemClass, children: body, "aria-current": render && status === "current" ? "step" : undefined },
  });
  return (
    <li className="flex min-w-0 items-center gap-0.5 group-data-[orientation=horizontal]/steps:flex-[1_0_auto]" aria-current={!render && status === "current" ? "step" : undefined}>
      {separator && index > 0 && <span className="flex-none text-[var(--weir-ink-4)] max-md:hidden">{separator}</span>}
      {item}
    </li>
  );
}

export { IconTile, CodeBlock, ChoiceCard, OptionRow, Steps, Step };
export type { StepStatus };
