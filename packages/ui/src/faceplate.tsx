// Weir 机架面板：设备头部与设备列表行。compact 去掉机架耳，用于列表。
import { ComponentProps, ReactNode } from "react";
import { cn } from "cn";

function Faceplate({
  compact = false,
  className,
  ...props
}: ComponentProps<"div"> & { compact?: boolean }) {
  return <div data-slot="faceplate" data-compact={compact} className={cn("mg-faceplate", className)} {...props} />;
}

function FaceplateTitle({
  eyebrow,
  title,
  meta,
  className,
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  meta?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0 flex-1", className)}>
      {eyebrow && <div className="mg-engraved mb-1">{eyebrow}</div>}
      <div className="truncate text-[17px] font-semibold tracking-[-0.015em] text-[var(--mg-ink)]">{title}</div>
      {meta && <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--mg-ink-3)]">{meta}</div>}
    </div>
  );
}

export { Faceplate, FaceplateTitle };
