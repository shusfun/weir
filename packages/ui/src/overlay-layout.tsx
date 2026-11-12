import type { ComponentProps } from "react";
import { cn } from "cn";
import { ScrollArea } from "./scroll-area";

// All modal surfaces reserve the same track and focus-ring space. Keep the
// header and actions outside this body so long content cannot move them.
export function OverlayBody({ className, viewportClassName, ...props }: Omit<ComponentProps<typeof ScrollArea>, "gutter">) {
  return <ScrollArea gutter="stable" className={cn("weir-overlay-body", className)} viewportClassName={cn("weir-focus-gutter", viewportClassName)} {...props} />;
}

// Action groups wrap as a unit; individual buttons retain their size contract.
export function ActionGroup({ className, align = "start", ...props }: ComponentProps<"div"> & { align?: "start" | "end" }) {
  return <div data-slot="action-group" data-align={align} className={cn("weir-action-group", className)} {...props} />;
}
