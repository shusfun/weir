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
        "fixed inset-0 isolate z-50 bg-gray-900/20 duration-100 supports-backdrop-filter:backdrop-blur-xs data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0",
        className,
      )}
      {...props}
    />
  );
}

function DialogContent({
  className,
  children,
  showCloseButton = true,
  ...props
}: DialogPrimitive.Popup.Props & {
  showCloseButton?: boolean;
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
        className={cn(
          "transition-[opacity,transform,translate,scale] fixed top-1/2 left-1/2 z-50 flex w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 flex-col gap-4 overflow-hidden rounded-lg bg-card p-6 text-base shadow-[0_0_15px_-3px_rgb(0,0,0,0.1)] ring-0 ring-foreground/10 duration-100 outline-none sm:max-w-sm max-h-[calc(100dvh-32px)] data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95",
          className,
        )}
        {...props}
      >
        {header.length > 0 && (
          <div data-slot="dialog-pinned-header" className="flex shrink-0 flex-col gap-2">
            {header}
          </div>
        )}
        <div data-slot="dialog-body" className="flex min-h-0 flex-auto flex-col overflow-x-hidden overflow-y-auto">
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
            <span className="sr-only">Close</span>
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
        "border-t-border-border -mx-6 -mb-6 flex shrink-0 flex-col-reverse gap-2 rounded-b-lg border-t bg-background p-5 sm:flex-row sm:justify-end",
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
      className={cn("h5", className)}
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
        "text-base text-muted-foreground *:[a]:underline *:[a]:underline-offset-3 *:[a]:hover:text-foreground",
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
