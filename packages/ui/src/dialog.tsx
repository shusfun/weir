"use client";

import {
  Children,
  cloneElement,
  createContext,
  isValidElement,
  useContext,
  useId,
  useMemo,
  useState,
  type ComponentProps,
  type ReactElement,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

// shadcn
import { Button } from "./button";

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
    if (child.type === "form" && containsFooter(child)) {
      return { id: (child.props as { id?: string }).id };
    }
    const found = findFooterForm(elementChildren(child));
    if (found) return found;
  }
  return null;
}

function assignFormId(node: ReactNode, formId: string): ReactNode {
  if (!isValidElement(node) || isDialogBoundary(node)) return node;
  if (node.type === "form" && containsFooter(node)) {
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

function DialogOverlay({
  className,
  ...props
}: DialogPrimitive.Backdrop.Props) {
  return (
    <DialogPrimitive.Backdrop
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 isolate z-50 bg-[rgb(20_18_14/0.38)] transition-opacity duration-[var(--mg-mid)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0 dark:bg-[rgb(0_0_0/0.6)]",
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

// 640px 以下居中弹窗改为底部抽屉：升到位后停顿一拍，内容再落稳（关键帧见 styles/motion.css）；
// 退场直接下沉。只改呈现，Dialog 的接口、焦点与关闭行为不变。命令面板（placement=top）不受影响。
const bottomSheet =
  "max-sm:inset-x-0 max-sm:top-auto max-sm:bottom-0 max-sm:w-full max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-b-none max-sm:border-b-0 max-sm:max-h-[calc(100dvh-24px)] max-sm:pb-[calc(20px+env(safe-area-inset-bottom,0px))] max-sm:transition-[translate] max-sm:duration-[260ms] max-sm:data-[open]:animate-[mg-sheet-rise_560ms_linear] max-sm:data-[starting-style]:scale-100 max-sm:data-[starting-style]:opacity-100 max-sm:data-[ending-style]:scale-100 max-sm:data-[ending-style]:opacity-100 max-sm:data-[ending-style]:translate-y-full";
const bottomSheetContent = "max-sm:group-data-[open]/dialog:animate-[mg-sheet-content_560ms_linear]";

function DialogContent({
  className,
  children,
  showCloseButton = true,
  size = "sm",
  placement = "center",
  ...props
}: DialogPrimitive.Popup.Props & {
  showCloseButton?: boolean;
  size?: keyof typeof dialogSizes;
  // top：靠上展示（命令面板），输入结果增减时位置不跳动
  placement?: "center" | "top";
}) {
  const generatedId = useId();
  const footerForm = findFooterForm(children);
  const formId = footerForm ? footerForm.id || generatedId : null;
  const [host, setHost] = useState<HTMLElement | null>(null);
  const slots = useMemo(() => ({ host, formId }), [host, formId]);
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
      <DialogOverlay />
      <DialogSlotContext.Provider value={slots}>
      <DialogPrimitive.Popup
        data-slot="dialog-content"
        data-size={size}
        className={cn(
          "group/dialog fixed left-1/2 z-50 flex max-h-[calc(100dvh-32px)] w-full max-w-[calc(100%-2rem)] -translate-x-1/2 flex-col gap-4 overflow-hidden rounded-[var(--mg-radius-lg)] border border-[var(--mg-line)] bg-[linear-gradient(180deg,var(--mg-panel-top),var(--mg-panel)_64px)] p-6 text-sm text-card-foreground shadow-[inset_0_1px_0_var(--mg-highlight),0_18px_40px_-12px_var(--mg-shade-strong)] outline-none transition-[opacity,scale] duration-[var(--mg-mid)] ease-[var(--mg-ease)] data-[starting-style]:scale-[0.98] data-[starting-style]:opacity-0 data-[ending-style]:scale-[0.98] data-[ending-style]:opacity-0 max-sm:p-5",
          placement === "top" ? "top-[14vh] max-h-[calc(86dvh-16px)]" : cn("top-1/2 -translate-y-1/2", bottomSheet),
          dialogSizes[size],
          className,
        )}
        {...props}
      >
        {header.length > 0 && (
          <div data-slot="dialog-pinned-header" className={cn("flex shrink-0 flex-col gap-2", placement === "center" && bottomSheetContent)}>
            {header}
          </div>
        )}
        <div
          data-slot="dialog-body"
          className={cn(
            "mg-focus-gutter flex min-h-0 min-w-0 flex-auto flex-col overflow-x-hidden overflow-y-auto [&>form]:flex [&>form]:min-h-full [&>form]:min-w-0 [&>form]:flex-[1_0_auto] [&>form]:flex-col",
            placement === "center" && bottomSheetContent,
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
    </DialogPortal>
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

function DialogFooter({
  className,
  showCloseButton = false,
  children,
  ...props
}: ComponentProps<"div"> & {
  showCloseButton?: boolean;
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
        "-mx-6 -mb-6 flex shrink-0 flex-col-reverse gap-2 rounded-b-[var(--mg-radius-lg)] border-t border-[var(--mg-line-soft)] bg-[var(--mg-panel)] px-6 py-4 max-sm:-mx-5 max-sm:-mb-5 max-sm:px-5 max-sm:py-3 sm:flex-row sm:justify-end",
        className,
      )}
      {...props}
    >
      {actions}
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
