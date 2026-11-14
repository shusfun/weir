import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip";
import type { ReactNode } from "react";
const TooltipProvider = TooltipPrimitive.Provider;

function Tooltip({
  content,
  children,
  side = "right",
}: {
  content: ReactNode;
  children: TooltipPrimitive.Trigger.Props["render"];
  side?: "top" | "right" | "bottom" | "left";
}) {
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger render={children} />
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Positioner side={side} sideOffset={8} className="z-50">
          <TooltipPrimitive.Popup className="rounded-[var(--weir-radius-sm)] bg-[var(--weir-ink)] px-2 py-1 text-meta text-[var(--weir-panel)] shadow-[0_4px_12px_-4px_var(--weir-shade-strong)] transition-opacity duration-[var(--weir-fast)] data-[starting-style]:opacity-0 data-[ending-style]:opacity-0">
            {content}
          </TooltipPrimitive.Popup>
        </TooltipPrimitive.Positioner>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}

export { Tooltip, TooltipProvider };
