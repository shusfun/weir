"use client";

import {
  Children,
  cloneElement,
  createContext,
  isValidElement,
  useContext,
  useId,
  useLayoutEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ComponentProps,
  type CSSProperties,
  type ReactElement,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

// shadcn
import { Button } from "./button";
import { Form } from "./form";

// third-party
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { cn } from "cn";

// assets
import { XIcon } from "lucide-react";
import { originFor, pressOrigin } from "./press-origin";
import { prefersReducedMotion } from "./motion";
import { OverlayBody } from "./overlay-layout";

type DialogSlots = { host: HTMLElement | null; formId: string | null };
const DialogSlotContext = createContext<DialogSlots | null>(null);

function elementChildren(node: ReactElement) {
  return (node.props as { children?: ReactNode }).children;
}

function isDialogBoundary(node: ReactElement) {
  return node.type === Dialog || node.type === DialogContent;
}

function containsFooter(node: ReactNode): boolean {
  if (!isValidElement(node) || isDialogBoundary(node)) return false;
  if (node.type === DialogFooter) return true;
  return Children.toArray(elementChildren(node)).some(containsFooter);
}

function findFooterForm(node: ReactNode): { id?: string } | null {
  for (const child of Children.toArray(node)) {
    if (!isValidElement(child) || isDialogBoundary(child)) continue;
    if ((child.type === "form" || child.type === Form) && containsFooter(child)) {
      return { id: (child.props as { id?: string }).id };
    }
    const found = findFooterForm(elementChildren(child));
    if (found) return found;
  }
  return null;
}

function assignFormId(node: ReactNode, formId: string): ReactNode {
  if (!isValidElement(node) || isDialogBoundary(node)) return node;
  if ((node.type === "form" || node.type === Form) && containsFooter(node)) {
    return cloneElement(node as ReactElement<{ id?: string }>, { id: formId });
  }
  const kids = elementChildren(node);
  if (kids == null) return node;
  let changed = false;
  const next = Children.map(kids, (child) => {
    const mapped = assignFormId(child, formId);
    if (mapped !== child) changed = true;
    return mapped;
  });
  return changed ? cloneElement(node, undefined, next) : node;
}

function isHeader(node: ReactNode) {
  return (
    isValidElement(node) &&
    (node.type === DialogHeader ||
      node.type === DialogTitle ||
      node.type === DialogDescription)
  );
}

function isBlank(node: ReactNode) {
  return typeof node === "string" && node.trim() === "";
}

function Dialog({ ...props }: DialogPrimitive.Root.Props) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />;
}

function DialogTrigger({ ...props }: DialogPrimitive.Trigger.Props) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />;
}

function DialogPortal({ ...props }: DialogPrimitive.Portal.Props) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />;
}

function DialogClose({ ...props }: DialogPrimitive.Close.Props) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />;
}

// 打开中的弹窗按打开顺序排成一摞（二级弹窗、确认框都算）：后打开的比下面那层低一档，下面那层原地不动，
// 只被一层更淡的遮罩压暗。只在弹窗挂载期间登记（含退场动画），关闭后自动移出。
const dialogStack: string[] = [];
const stackListeners = new Set<() => void>();
const notifyStack = () => stackListeners.forEach((l) => l());

function useDialogDepth(id: string) {
  useLayoutEffect(() => {
    dialogStack.push(id);
    notifyStack();
    return () => {
      const i = dialogStack.indexOf(id);
      if (i >= 0) dialogStack.splice(i, 1);
      notifyStack();
    };
  }, [id]);
  return useSyncExternalStore(
    (l) => {
      stackListeners.add(l);
      return () => void stackListeners.delete(l);
    },
    () => Math.max(0, dialogStack.indexOf(id)),
    () => 0,
  );
}

function DialogOverlay({
  className,
  depth = 0,
  ...props
}: DialogPrimitive.Backdrop.Props & { depth?: number }) {
  return (
    <DialogPrimitive.Backdrop
      data-slot="dialog-overlay"
      // 嵌套的二级弹窗也画遮罩（Base UI 默认只画最外层），这样父弹窗被压暗、一眼看出谁在上面
      forceRender={depth > 0}
      className={cn(
        "fixed inset-0 isolate z-50 transition-opacity duration-[var(--weir-mid)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0",
        depth > 0 ? "bg-[var(--weir-scrim-soft)]" : "bg-[var(--weir-scrim)]",
        className,
      )}
      {...props}
    />
  );
}

// 弹窗宽度档位：sm 确认 / 简单提示，md 常规表单，lg 列表编辑 / 多栏表单，xl 预览 / 大型编辑器。
// 业务侧只选档位，不再用 !important 写死宽度。
const dialogSizes = {
  sm: "sm:max-w-[var(--weir-dialog-sm)]",
  md: "sm:max-w-[var(--weir-dialog-md)]",
  lg: "sm:max-w-[var(--weir-dialog-lg)]",
  xl: "sm:max-w-[var(--weir-dialog-xl)]",
} as const;

// 弹窗靠上固定（参照 Element Plus 的 top: 15vh）：内容增减只改下沿，标题和已经看到的内容不跟着跳；
// 超出视口在弹窗里滚动。二级弹窗按层级再往下错开一档，父弹窗留在原位。
// 桌面：缩放 0.96→1 并淡入（缩放按轻快弹簧），退场淡出并缩回 0.96。
// 640px 以下改为 iOS 底部抽屉（sheet）：圆角顶、顶部把手，按轻快弹簧从底部升起，退场加速下沉；不缩放、不淡入。
// sm 档（确认框、简单提示）按内容高度；其余档固定从顶部 --weir-dialog-sheet-top 铺到底，内容变化时上沿与按钮都不动。
// 命令面板（placement=palette）使用 --weir-dialog-palette-top，窄屏也不变成抽屉。
const sheetBase =
  "max-sm:inset-x-0 max-sm:bottom-0 max-sm:w-full max-sm:max-w-none max-sm:translate-x-0 max-sm:rounded-b-none max-sm:rounded-t-[var(--weir-radius-sheet)] max-sm:border-x-0 max-sm:border-b-0 max-sm:pt-[var(--weir-space-2)] max-sm:pb-[calc(var(--weir-space-5)+env(safe-area-inset-bottom,0px))] max-sm:[transition:translate_var(--weir-spring-dur)_var(--weir-spring)] max-sm:data-[starting-style]:translate-y-full max-sm:data-[starting-style]:scale-100 max-sm:data-[starting-style]:opacity-100 max-sm:data-[ending-style]:translate-y-full max-sm:data-[ending-style]:scale-100 max-sm:data-[ending-style]:opacity-100 max-sm:data-[ending-style]:[transition:translate_var(--weir-slow)_var(--weir-ease-in)]";
const sheetFit = "max-sm:top-auto max-sm:max-h-[calc(100dvh-var(--weir-space-6))]";
const sheetFull = "max-sm:top-[calc(var(--weir-dialog-sheet-top)_+_var(--weir-dialog-depth)_*_var(--weir-space-4))] max-sm:max-h-none";
const anchored = "top-[calc(var(--weir-dialog-top)_+_var(--weir-dialog-depth)_*_var(--weir-space-6))] max-h-[calc(100dvh_-_var(--weir-dialog-top)_-_var(--weir-space-6)_-_var(--weir-dialog-depth)_*_var(--weir-space-6))]";

type ContentProps = DialogPrimitive.Popup.Props & {
  showCloseButton?: boolean;
  size?: keyof typeof dialogSizes;
  placement?: "top" | "palette";
  // layer="top"：确认框一类必须在最上面的弹窗（盖过 dsh 等常驻的浮动窗口）
  layer?: "page" | "top";
};

function DialogContent({ children, ...rest }: ContentProps) {
  const generatedId = useId();
  const footerForm = findFooterForm(children);
  const formId = footerForm ? footerForm.id || generatedId : null;
  const prepared =
    formId && !footerForm?.id
      ? Children.map(children, (child) => assignFormId(child, formId))
      : children;
  const nodes = Children.toArray(prepared);
  let split = 0;
  while (split < nodes.length && (isBlank(nodes[split]) || isHeader(nodes[split]))) {
    split += 1;
  }
  const header = nodes.slice(0, split).filter((node) => !isBlank(node));
  const body = nodes.slice(split);
  return (
    <DialogPortal>
      <DialogLayer id={generatedId} formId={formId} header={header} body={body} {...rest} />
    </DialogPortal>
  );
}

// DialogLayer 只在弹窗挂载期间存在（DialogPortal 关闭后不渲染子元素），在这里登记层级。
function DialogLayer({
  id,
  formId,
  header,
  body,
  className,
  style,
  showCloseButton = true,
  size = "sm",
  placement = "top",
  layer = "page",
  ...props
}: Omit<ContentProps, "children"> & { id: string; formId: string | null; header: ReactNode[]; body: ReactNode[] }) {
  const depth = useDialogDepth(id);
  const [host, setHost] = useState<HTMLElement | null>(null);
  const [popup, setPopup] = useState<HTMLElement | null>(null);
  const slots = useMemo(() => ({ host, formId }), [host, formId]);
  const sheet = placement === "top";
  // 从点击的地方展开：挂载时（首帧绘制前）把按压点设为缩放中心，0.96→1 的缩放朝按压点收拢；关闭时沿用同一中心收回去。
  // 窄屏是底部抽屉（不缩放），减少动态效果时只淡入。
  useLayoutEffect(() => {
    const p = pressOrigin();
    if (!popup || !p || prefersReducedMotion()) return;
    popup.style.transformOrigin = originFor(popup, p);
  }, [popup]);
  return (
    <>
      <DialogOverlay depth={depth} className={layer === "top" ? "z-[57]" : undefined} />
      <DialogSlotContext.Provider value={slots}>
      <DialogPrimitive.Popup
        ref={setPopup}
        data-slot="dialog-content"
        data-tour="dialog"
        data-size={size}
        data-depth={depth}
        style={{ "--weir-dialog-depth": depth, ...(style as CSSProperties) } as CSSProperties}
        className={cn(
          layer === "top" ? "z-[57]" : "z-50",
          "weir-dialog group/dialog fixed inset-x-0 mx-auto flex w-full max-w-[calc(100%-var(--weir-space-8))] flex-col gap-[var(--weir-space-4)] overflow-hidden rounded-[var(--weir-radius-dialog)] border border-[var(--weir-line-soft)] bg-[var(--weir-raised)] p-[var(--weir-dialog-padding)] text-body text-card-foreground shadow-[var(--weir-shadow-float)] outline-none [transition:opacity_var(--weir-mid)_var(--weir-ease),scale_var(--weir-spring-dur)_var(--weir-spring)] data-[starting-style]:scale-[0.96] data-[starting-style]:opacity-0 data-[ending-style]:scale-[0.96] data-[ending-style]:opacity-0 data-[ending-style]:[transition-duration:var(--weir-fast)]",
          placement === "palette" ? "top-[var(--weir-dialog-palette-top)] max-h-[calc(100dvh-var(--weir-dialog-palette-top)-var(--weir-space-4))]" : cn(anchored, sheetBase, size === "sm" ? sheetFit : sheetFull),
          dialogSizes[size],
          className,
        )}
        {...props}
      >
        {/* 底部抽屉的把手：只是提示可以下滑关闭的样子，关闭仍用右上角关闭键、Esc 与点遮罩 */}
        {sheet && <div aria-hidden data-slot="dialog-handle" className="weir-sheet-handle mb-0 hidden max-sm:block" />}
        {header.length > 0 && (
          <div data-slot="dialog-pinned-header" className="flex shrink-0 flex-col gap-[var(--weir-space-2)]">
            {header}
          </div>
        )}
        <OverlayBody
          data-slot="dialog-body"
          className="min-h-0 min-w-0 flex-auto"
          viewportClassName="weir-focus-gutter"
          contentClassName="flex min-h-full min-w-0 flex-col gap-[var(--weir-space-4)] [&>*]:min-w-0 [&>*]:shrink-0 [&>form]:flex [&>form]:min-w-0 [&>form]:flex-[1_0_auto] [&>form]:flex-col"
        >
          {body}
        </OverlayBody>
        <div ref={setHost} data-slot="dialog-footer-host" className="shrink-0 empty:hidden" />
        {showCloseButton && (
          <DialogPrimitive.Close
            data-slot="dialog-close"
            render={
              <Button
                variant="ghost"
                className="weir-dialog-close absolute rounded-full"
                size="icon-sm"
              />
            }
          >
            <XIcon />
            <span className="sr-only">关闭</span>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Popup>
      </DialogSlotContext.Provider>
    </>
  );
}

function DialogHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  );
}

// error：提交失败的原因，显示在按钮旁（宽屏在左、窄屏在按钮上方），出现时按钮位置不变。
function DialogFooter({
  className,
  showCloseButton = false,
  error,
  status,
  children,
  ...props
}: ComponentProps<"div"> & {
  showCloseButton?: boolean;
  error?: ReactNode;
  // status：等待提示或结果（<WaitText>），与错误共用按钮左侧这一位；有错误时只显示错误
  status?: ReactNode;
}) {
  const slot = useContext(DialogSlotContext);
  const formId = slot?.formId ?? null;
  const actions = formId
    ? Children.map(children, (child) => {
        if (!isValidElement(child)) return child;
        const button = child.props as { type?: string; form?: string };
        if (button.type === "submit" && !button.form) {
          return cloneElement(child as ReactElement<{ form?: string }>, { form: formId });
        }
        return child;
      })
    : children;
  const footer = (
    <div
      data-slot="dialog-footer"
      className={cn(
        "weir-dialog-footer flex shrink-0 flex-col-reverse gap-[var(--weir-space-2)] rounded-b-[var(--weir-radius-dialog)] border-t border-[var(--weir-line-soft)] bg-[var(--weir-raised)] sm:flex-row sm:flex-wrap sm:items-center sm:justify-end",
        className,
      )}
      {...props}
    >
      {actions}
      {error ? (
        <p role="alert" className="m-0 min-w-0 text-body text-[var(--weir-bad-ink)] sm:order-first sm:mr-auto sm:self-center">
          {error}
        </p>
      ) : status ? (
        <div role="status" className="m-0 min-w-0 text-body text-[var(--weir-ink-2)] sm:order-first sm:mr-auto sm:self-center">
          {status}
        </div>
      ) : null}
      {showCloseButton && (
        <DialogPrimitive.Close render={<Button variant="outline" />}>
          Close
        </DialogPrimitive.Close>
      )}
    </div>
  );
  if (!slot) return footer;
  if (!slot.host) return null;
  return createPortal(footer, slot.host);
}

function DialogTitle({ className, ...props }: DialogPrimitive.Title.Props) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn("pr-8 text-dialog font-semibold tracking-[-0.015em] text-[var(--weir-ink)] touch:pr-11", className)}
      {...props}
    />
  );
}

function DialogDescription({
  className,
  ...props
}: DialogPrimitive.Description.Props) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn(
        "text-body text-[var(--weir-ink-3)] *:[a]:underline *:[a]:underline-offset-3 *:[a]:hover:text-foreground",
        className,
      )}
      {...props}
    />
  );
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
};
