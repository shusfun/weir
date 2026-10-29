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
        "fixed inset-0 isolate z-50 transition-opacity duration-[var(--mg-mid)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0",
        depth > 0 ? "bg-[rgb(20_18_14/0.22)] dark:bg-[rgb(0_0_0/0.4)]" : "bg-[rgb(20_18_14/0.38)] dark:bg-[rgb(0_0_0/0.6)]",
        className,
      )}
      {...props}
    />
  );
}

// 弹窗宽度档位：sm 确认 / 简单提示，md 常规表单，lg 列表编辑 / 多栏表单，xl 预览 / 大型编辑器。
// 业务侧只选档位，不再用 !important 写死宽度。
const dialogSizes = {
  sm: "sm:max-w-sm",
  md: "sm:max-w-[560px]",
  lg: "sm:max-w-[720px]",
  xl: "sm:max-w-[900px]",
} as const;

// 弹窗靠上固定（参照 Element Plus 的 top: 15vh）：内容增减只改下沿，标题和已经看到的内容不跟着跳；
// 超出视口在弹窗里滚动。二级弹窗按层级再往下错开一档，父弹窗留在原位。
// 640px 以下改为底部抽屉：升到位后停顿一拍，内容再落稳（关键帧见 styles/motion.css），退场直接下沉。
// sm 档（确认框、简单提示）按内容高度；其余档固定从顶部 8dvh 铺到底，内容变化时上沿与按钮都不动。
// 命令面板（placement=palette）固定在 14vh，窄屏也不变成抽屉。
const sheetBase =
  "max-sm:inset-x-0 max-sm:bottom-0 max-sm:w-full max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-b-none max-sm:border-b-0 max-sm:pb-[calc(20px+env(safe-area-inset-bottom,0px))] max-sm:transition-[translate] max-sm:duration-[260ms] max-sm:data-[open]:animate-[mg-sheet-rise_560ms_linear] max-sm:data-[starting-style]:scale-100 max-sm:data-[starting-style]:opacity-100 max-sm:data-[ending-style]:scale-100 max-sm:data-[ending-style]:opacity-100 max-sm:data-[ending-style]:translate-y-full";
const sheetFit = "max-sm:top-auto max-sm:max-h-[calc(100dvh-24px)]";
const sheetFull = "max-sm:top-[calc(8dvh_+_var(--mg-dialog-depth)_*_16px)] max-sm:max-h-none";
const anchored = "top-[calc(12vh_+_var(--mg-dialog-depth)_*_24px)] max-h-[calc(88vh_-_24px_-_var(--mg-dialog-depth)_*_24px)]";
const bottomSheetContent = "max-sm:group-data-[open]/dialog:animate-[mg-sheet-content_560ms_linear]";

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
  const slots = useMemo(() => ({ host, formId }), [host, formId]);
  const sheet = placement === "top";
  return (
    <>
      <DialogOverlay depth={depth} className={layer === "top" ? "z-[57]" : undefined} />
      <DialogSlotContext.Provider value={slots}>
      <DialogPrimitive.Popup
        data-slot="dialog-content"
        data-size={size}
        data-depth={depth}
        style={{ "--mg-dialog-depth": depth, ...(style as CSSProperties) } as CSSProperties}
        className={cn(
          layer === "top" ? "z-[57]" : "z-50",
          "group/dialog fixed left-1/2 flex w-full max-w-[calc(100%-2rem)] -translate-x-1/2 flex-col gap-4 overflow-hidden rounded-[var(--mg-radius-lg)] border border-[var(--mg-line)] bg-[linear-gradient(180deg,var(--mg-panel-top),var(--mg-panel)_64px)] p-6 text-sm text-card-foreground shadow-[inset_0_1px_0_var(--mg-highlight),0_18px_40px_-12px_var(--mg-shade-strong)] outline-none transition-[opacity,scale] duration-[var(--mg-mid)] ease-[var(--mg-ease)] data-[starting-style]:scale-[0.98] data-[starting-style]:opacity-0 data-[ending-style]:scale-[0.98] data-[ending-style]:opacity-0 max-sm:p-5",
          placement === "palette" ? "top-[14vh] max-h-[calc(86dvh-16px)]" : cn(anchored, sheetBase, size === "sm" ? sheetFit : sheetFull),
          dialogSizes[size],
          className,
        )}
        {...props}
      >
        {header.length > 0 && (
          <div data-slot="dialog-pinned-header" className={cn("flex shrink-0 flex-col gap-2", sheet && bottomSheetContent)}>
            {header}
          </div>
        )}
        <div
          data-slot="dialog-body"
          className={cn(
            "mg-focus-gutter flex min-h-0 min-w-0 flex-auto flex-col overflow-x-hidden overflow-y-auto [&>form]:flex [&>form]:min-h-full [&>form]:min-w-0 [&>form]:flex-[1_0_auto] [&>form]:flex-col",
            sheet && bottomSheetContent,
          )}
        >
          {body}
        </div>
        <div ref={setHost} data-slot="dialog-footer-host" className="shrink-0 empty:hidden" />
        {showCloseButton && (
          <DialogPrimitive.Close
            data-slot="dialog-close"
            render={
              <Button
                variant="ghost"
                className="absolute top-2 right-2"
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
  children,
  ...props
}: ComponentProps<"div"> & {
  showCloseButton?: boolean;
  error?: ReactNode;
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
        "-mx-6 -mb-6 flex shrink-0 flex-col-reverse gap-2 rounded-b-[var(--mg-radius-lg)] border-t border-[var(--mg-line-soft)] bg-[var(--mg-panel)] px-6 py-4 max-sm:-mx-5 max-sm:-mb-5 max-sm:px-5 max-sm:py-3 sm:flex-row sm:items-start sm:justify-end",
        className,
      )}
      {...props}
    >
      {actions}
      {error && (
        <p role="alert" className="m-0 min-w-0 text-[13px] leading-[1.5] text-[var(--mg-bad-ink)] sm:order-first sm:mr-auto sm:self-center">
          {error}
        </p>
      )}
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
      className={cn("pr-8 text-[17px] font-semibold tracking-[-0.015em] text-[var(--mg-ink)]", className)}
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
        "text-[13px] leading-relaxed text-[var(--mg-ink-3)] *:[a]:underline *:[a]:underline-offset-3 *:[a]:hover:text-foreground",
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
