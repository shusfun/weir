// Weir 机架面板：设备头部与设备列表行。compact 去掉机架耳，用于列表。
import { ComponentProps, ReactNode } from "react";
import { cn } from "cn";

function Faceplate({
  compact = false,
  className,
  ...props
}: ComponentProps<"div"> & { compact?: boolean }) {
  return <div data-slot="faceplate" data-compact={compact} className={cn("weir-faceplate", className)} {...props} />;
}

function FaceplateTitle({
  title,
  meta,
  className,
}: {
  title: ReactNode;
  meta?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0 flex-1", className)}>
      <div className="truncate text-headline font-semibold tracking-[-0.015em] text-[var(--weir-ink)]">{title}</div>
      {meta && <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-meta text-[var(--weir-ink-3)]">{meta}</div>}
    </div>
  );
}

export { Faceplate, FaceplateTitle };
