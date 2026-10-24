// 展示类小部件：图标方块 / 头像、代码与指纹块、可选卡片、勾选行、步骤条。
// 业务页面只组合这些部件，不再各自手写圆角凹槽、选中描边与步骤编号样式。
import { type ComponentProps, type ReactElement, type ReactNode } from "react";
import { useRender } from "@base-ui/react/use-render";
import { mergeProps } from "@base-ui/react/merge-props";
import { Check } from "lucide-react";
import { cn } from "cn";
import { Ripple } from "./ripple";

// ---- 图标方块 / 头像 ----
const tileSizes = {
  sm: "size-[30px] rounded-[9px] text-xs font-[650] [&_svg]:size-4",
  md: "size-10 rounded-[10px] [&_svg]:size-[21px]",
  lg: "size-11 rounded-xl [&_svg]:size-[22px]",
  xl: "size-[52px] rounded-xl [&_svg]:size-7",
} as const;
const tileTones = {
  // 凹槽底 + 强调色图标：安全方式、登录页头
  accent: "mg-well text-[var(--mg-accent-text)]",
  // 凹槽底 + 中性图标 / 文字：账户头像
  neutral: "mg-well text-[var(--mg-ink-2)]",
  // 浅强调底：公开页卡片（随页面主题色）
  soft: "bg-[var(--mg-accent-soft)] text-[var(--mg-accent-text)]",
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
        "mg-well block px-3 py-2.5 font-mono text-[11.5px] leading-relaxed break-all text-[var(--mg-ink-2)]",
        className,
      )}
      {...props}
    />
  );
}

// ---- 可选卡片：几张卡片里选一张（客户端、下载来源） ----
// ripple：可点的卡片（render 成按钮 / 链接，或带 onClick）默认从按压点散开水波
function ChoiceCard({
  selected = false,
  featured = false,
  ripple,
  render,
  className,
  children,
  ...props
}: useRender.ComponentProps<"div"> & { selected?: boolean; featured?: boolean; ripple?: boolean }) {
  const wave = ripple ?? (!!render || !!props.onClick);
  return useRender({
    defaultTagName: "div",
    render,
    props: mergeProps<"div">(
      {
        children: (
          <>
            {children}
            {wave && <Ripple />}
          </>
        ),
        className: cn(
          "relative flex min-w-0 flex-col gap-2.5 rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3.5",
          "bg-[linear-gradient(180deg,var(--mg-raised),var(--mg-panel))] shadow-[inset_0_1px_0_var(--mg-highlight),0_1px_2px_var(--mg-shade)]",
          featured &&
            "gap-3 rounded-[var(--mg-radius-lg)] border-[color-mix(in_srgb,var(--mg-accent-text)_45%,var(--mg-line))] bg-[linear-gradient(180deg,var(--mg-raised),var(--mg-accent-soft))] p-[18px]",
          selected && "border-[var(--mg-accent-text)] shadow-[inset_0_1px_0_var(--mg-highlight),0_0_0_2px_var(--mg-ring)]",
          className,
        ),
        "data-selected": selected || undefined,
      } as ComponentProps<"div">,
      props,
    ),
  });
}

// ---- 勾选行：列表里每行一个复选框 + 名称 / 说明 ----
function OptionRow({
  inset = false,
  compact = false,
  ripple = true,
  className,
  children,
  ...props
}: ComponentProps<"label"> & { inset?: boolean; compact?: boolean; ripple?: boolean }) {
  return (
    <label
      data-slot="option-row"
      className={cn(
        // 文字列只匹配不带 data-slot 的 span：复选框（Base UI 渲染为 span）和水波层不能被撑开
        "relative flex min-w-0 cursor-pointer items-start gap-3 wrap-anywhere [&>span:not([data-slot])]:grid [&>span:not([data-slot])]:min-w-0 [&>span:not([data-slot])]:flex-1 [&>span:not([data-slot])]:gap-[3px] [&>[data-slot=checkbox]]:mt-0.5 [&_small]:block",
        compact ? "items-center gap-2.5 text-[12.5px]" : "border-b border-[var(--mg-line-soft)] py-3",
        inset && "px-3.5",
        className,
      )}
      {...props}
    >
      {children}
      {ripple && <Ripple />}
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
          : "mg-segmented flex items-stretch gap-0.5 rounded-[var(--mg-radius-lg)] p-[3px]",
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
          "grid size-[22px] shrink-0 place-items-center rounded-full border font-mono text-[11px] font-semibold group-data-[orientation=vertical]/steps:size-[34px] group-data-[orientation=vertical]/steps:text-xs [&_svg]:size-[17px]",
          status === "current"
            ? "border-[var(--mg-accent-lo)] bg-[var(--mg-accent)] text-[var(--mg-accent-ink)] shadow-[0_0_8px_1px_color-mix(in_srgb,var(--mg-accent)_40%,transparent)]"
            : status === "complete"
              ? "border-[var(--mg-line)] bg-[var(--mg-well)] text-[var(--mg-ok)]"
              : "border-[var(--mg-line)] bg-[var(--mg-panel)] text-[var(--mg-ink-3)]",
        )}
      >
        {status === "complete" ? <Check className="size-3.5" /> : (icon ?? index + 1)}
      </span>
      <span className="min-w-0">
        <strong className="block text-[13px] font-[550] whitespace-nowrap max-md:group-data-[orientation=vertical]/steps:text-[11.5px]">{label}</strong>
        {caption && <small className="mt-0.5 block text-[11px] whitespace-nowrap max-md:hidden group-data-[orientation=vertical]/steps:text-[11.5px]">{caption}</small>}
      </span>
    </>
  );
  const itemClass = cn(
    "flex min-w-0 items-center gap-3 no-underline",
    // 纵向步骤在窄屏改为横排四列、文字居中
    "max-md:group-data-[orientation=vertical]/steps:flex-col max-md:group-data-[orientation=vertical]/steps:gap-1.5 max-md:group-data-[orientation=vertical]/steps:text-center",
    status === "current" ? "text-[var(--mg-ink)]" : status === "complete" ? "text-[var(--mg-ok-ink)]" : "text-[var(--mg-ink-3)]",
    // 横向：每步是可点击的标签，当前步点亮为选中色
    "group-data-[orientation=horizontal]/steps:flex-[1_0_auto] group-data-[orientation=horizontal]/steps:rounded-[calc(var(--mg-radius-lg)-3px)] group-data-[orientation=horizontal]/steps:px-3.5 group-data-[orientation=horizontal]/steps:py-[9px] group-data-[orientation=horizontal]/steps:hover:text-[var(--mg-ink)] max-md:group-data-[orientation=horizontal]/steps:px-2.5",
    status === "current" &&
      "group-data-[orientation=horizontal]/steps:bg-[var(--mg-key-selected)] group-data-[orientation=horizontal]/steps:shadow-[inset_0_0_0_1px_var(--mg-key-selected-ring)]",
    "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--mg-accent-text)]",
  );
  const item = useRender({
    defaultTagName: "span",
    render,
    props: { className: itemClass, children: body, "aria-current": render && status === "current" ? "step" : undefined },
  });
  return (
    <li className="flex min-w-0 items-center gap-0.5 group-data-[orientation=horizontal]/steps:flex-[1_0_auto]" aria-current={!render && status === "current" ? "step" : undefined}>
      {separator && index > 0 && <span className="flex-none text-[var(--mg-ink-engraved)] max-md:hidden">{separator}</span>}
      {item}
    </li>
  );
}

export { IconTile, CodeBlock, ChoiceCard, OptionRow, Steps, Step };
export type { StepStatus };
