// 覆盖式滚动条：底层仍是原生滚动（触控板惯性、键盘翻页、读屏、scrollIntoView 照常），
// 隐藏系统滚动条，改画一条浮在内容上的细条，不占布局宽度，出现滚动条时内容不左右跳。
// 只在滚动中、或鼠标停在滚动条上时显示；停止滚动后 Base UI 约 500 ms 取消 data-scrolling，
// 再经 CSS 过渡延迟淡出（合计约 600 ms），样式见 styles/controls.css 的 .weir-scroll-*。
//
// 结构：根（className，定尺寸：max-h / h / flex-1 / 边框）→ 视口（viewportClassName，真正的滚动元素，
// 留焦点环余量用 weir-focus-gutter 写在这里）→ 内容（contentClassName，排版：stack / grid / gap / 内边距）。
// 监听 scroll、读写 scrollTop 用 viewportRef；不需要内容层时 contentRender={false}，子元素直接放进视口。
import { useCallback, useEffect, useState, type ComponentProps, type CSSProperties, type ReactElement, type ReactNode, type Ref } from "react";
import { ScrollArea as ScrollAreaPrimitive } from "@base-ui/react/scroll-area";
import { cn } from "cn";
import { prefersReducedMotion } from "./motion";

type Orientation = "vertical" | "horizontal" | "both";

type ScrollAreaProps = Omit<ComponentProps<"div">, "children" | "content"> & {
  children?: ReactNode;
  // 允许滚动的方向；另一方向裁掉，不出现滚动条
  orientation?: Orientation;
  viewportClassName?: string;
  viewportRef?: Ref<HTMLDivElement>;
  // 视口上的其他属性（aria-label、role、onScroll 等）
  viewportProps?: Omit<ComponentProps<"div">, "ref" | "className" | "children">;
  // 视口改用别的元素渲染（例如 Select.List，让它自己就是滚动元素）
  viewportRender?: ReactElement;
  contentClassName?: string;
  // 内容层改用别的元素渲染（例如 <ul />，子元素是 <li>）；false 表示不要内容层
  contentRender?: ReactElement | false;
};

const overflowFor: Record<Orientation, CSSProperties> = {
  vertical: { overflowX: "hidden", overflowY: "scroll" },
  horizontal: { overflowX: "scroll", overflowY: "hidden" },
  both: { overflow: "scroll" },
};

function ScrollArea({
  className,
  children,
  orientation = "vertical",
  viewportClassName,
  viewportRef,
  viewportProps,
  viewportRender,
  contentClassName,
  contentRender,
  ...props
}: ScrollAreaProps) {
  const { style: viewportStyle, ...restViewport } = viewportProps ?? {};
  return (
    <ScrollAreaPrimitive.Root data-slot="scroll-area" data-orientation={orientation} className={cn("weir-scroll-root", className)} {...props}>
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        ref={viewportRef}
        render={viewportRender}
        className={cn("weir-scroll-viewport", viewportClassName)}
        style={{ ...overflowFor[orientation], ...viewportStyle }}
        {...restViewport}
      >
        {contentRender === false ? (
          children
        ) : (
          // 内容层只用于测量内容尺寸变化（新增行时拇指长度跟着变），不改变排版；
          // Base UI 默认给它 min-width: fit-content，纵向滚动时去掉，避免宽内容把视口撑出横向滚动
          <ScrollAreaPrimitive.Content
            data-slot="scroll-area-content"
            render={contentRender}
            className={cn("weir-scroll-content", contentClassName)}
            style={orientation === "vertical" ? { minWidth: 0 } : undefined}
          >
            {children}
          </ScrollAreaPrimitive.Content>
        )}
      </ScrollAreaPrimitive.Viewport>
      {orientation !== "horizontal" && <ScrollBar orientation="vertical" />}
      {orientation !== "vertical" && <ScrollBar orientation="horizontal" />}
      {orientation === "both" && <ScrollAreaPrimitive.Corner className="weir-scroll-corner" />}
    </ScrollAreaPrimitive.Root>
  );
}

// 整页滚动区（桌面）：铺满视口，代替 html / body 的滚动（全局样式让 html / body 不再滚动，因此不出现占位的系统滚动条）。
// 页面里的 position: sticky、scrollIntoView 以这个视口为滚动容器照常工作。
// 焦点停在 body 上时（刚加载、点了空白处），翻页键与方向键原本滚动文档，现在转给视口并把焦点移进去，
// 之后的按键由视口原生处理。
const pageKeys: Record<string, (el: HTMLElement) => number> = {
  PageDown: (el) => el.clientHeight * 0.875,
  PageUp: (el) => -el.clientHeight * 0.875,
  ArrowDown: () => 40,
  ArrowUp: () => -40,
  Home: (el) => -el.scrollTop,
  End: (el) => el.scrollHeight,
};

// 触屏设备（pointer: coarse）不套覆盖式滚动区，回到浏览器原生整页滚动：手机地址栏随滚动收起、有原生回弹。
// 只在挂载时判定一次：中途切换会整棵重挂、丢掉页面状态，而混合设备换输入方式时两种滚动都能用。
const coarsePointer = () => typeof window !== "undefined" && window.matchMedia("(pointer: coarse)").matches;

function PageScrollArea({ className, viewportRef, ...props }: ScrollAreaProps) {
  const [native] = useState(coarsePointer);
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!viewport || native) return;
    const onKey = (e: KeyboardEvent) => {
      const active = document.activeElement;
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
      if (active && active !== document.body && active !== document.documentElement) return;
      const space = e.key === " ";
      const delta = space ? (e.shiftKey ? -1 : 1) * viewport.clientHeight * 0.875 : pageKeys[e.key]?.(viewport);
      if (delta === undefined) return;
      e.preventDefault();
      viewport.focus({ preventScroll: true });
      viewport.scrollBy({ top: delta, behavior: prefersReducedMotion() ? "auto" : "smooth" });
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [viewport, native]);
  const refs = useCallback(
    (el: HTMLDivElement | null) => {
      setViewport(el);
      if (typeof viewportRef === "function") viewportRef(el);
      else if (viewportRef) viewportRef.current = el;
    },
    [viewportRef],
  );
  if (native) {
    const { children, viewportProps, style } = props;
    // 原生滚动：文档自己滚，这里只是普通容器；viewportRef 指向它，调用方的 ref 不失效
    return (
      <div data-page-scroll="native" ref={refs} className={className} style={{ ...viewportProps?.style, ...style }}>
        {children}
      </div>
    );
  }
  return <ScrollArea data-page-scroll="" className={cn("weir-page-scroll", className)} viewportRef={refs} {...props} />;
}

function ScrollBar({ orientation }: { orientation: "vertical" | "horizontal" }) {
  return (
    <ScrollAreaPrimitive.Scrollbar data-slot="scroll-area-scrollbar" orientation={orientation} className="weir-scrollbar">
      <ScrollAreaPrimitive.Thumb data-slot="scroll-area-thumb" className="weir-scroll-thumb" />
    </ScrollAreaPrimitive.Scrollbar>
  );
}

export { ScrollArea, PageScrollArea };
export type { ScrollAreaProps };
