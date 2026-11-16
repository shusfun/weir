import type { ComponentProps } from "react";
import { cn } from "cn";
import { ScrollArea } from "./scroll-area";

// All modal surfaces reserve the same track and focus-ring space. Keep the
// header and actions outside this body so long content cannot move them.
export function OverlayBody({ className, viewportClassName, ...props }: Omit<ComponentProps<typeof ScrollArea>, "gutter">) {
  return <ScrollArea gutter="stable" className={cn("weir-overlay-body", className)} viewportClassName={cn("weir-focus-gutter", viewportClassName)} {...props} />;
}

// Compact object actions stay together; their parent can move the entire group to a new row.
export function ActionGroup({ className, align = "start", wrap = true, ...props }: ComponentProps<"div"> & { align?: "start" | "end"; wrap?: boolean }) {
  return <div data-slot="action-group" data-align={align} data-wrap={wrap} className={cn("weir-action-group", className)} {...props} />;
}
