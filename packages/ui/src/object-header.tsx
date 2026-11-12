// 对象详情页的页头（设计稿 BVpsDetail / BNodeDetail / BUserDetail）：
// 顶栏位置是面包屑加操作，下面是状态行、大号对象名、等宽地址行，可选洞察横幅；右侧可放主视觉（文件夹、群像等）。
import type { ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { cn } from "cn";
import { StatusLamp, type LampTone } from "./lamp";

function ObjectHeader({
  back,
  crumb,
  actions,
  tone,
  status,
  title,
  meta,
  banner,
  aside,
  keepAside = false,
  className,
}: {
  /** 返回链接（路由组件由调用方给，保证后退可用） */
  back: ReactNode;
  /** 面包屑最后一段，通常是对象名 */
  crumb: ReactNode;
  actions?: ReactNode;
  tone: LampTone;
  status: ReactNode;
  title: ReactNode;
  meta?: ReactNode;
  banner?: ReactNode;
  aside?: ReactNode;
  /** 流量等主要业务读数在窄屏也保留；装饰性主视觉默认隐藏。 */
  keepAside?: boolean;
  className?: string;
}) {
  return (
    <header data-slot="object-header" data-keep-aside={keepAside || undefined} className={cn("weir-object-header", className)}>
      <div className="weir-object-top">
        <nav className="weir-crumb" aria-label="位置">
          <ArrowLeft aria-hidden />
          {back}
          <span aria-hidden>/</span>
          <b>{crumb}</b>
        </nav>
        {actions && <div className="flex flex-wrap items-center gap-[9px]">{actions}</div>}
      </div>
      <div className={cn("weir-object-hero", aside && "has-aside")}>
        <div className="min-w-0">
          <div className="flex items-center gap-[9px] text-meta text-[var(--weir-ink-2)]">
            <StatusLamp tone={tone} />
            <span>{status}</span>
          </div>
          <h1 data-slot="page-title" className="m-0 mt-[9px] text-detail font-bold tracking-[-0.03em] text-[var(--weir-ink)]">{title}</h1>
          {meta && <div className="mt-[5px] font-[family-name:var(--weir-mono)] text-meta text-[var(--weir-ink-3)]">{meta}</div>}
          {banner && <div className="mt-[21px]">{banner}</div>}
        </div>
        {aside && <div className="weir-object-aside">{aside}</div>}
      </div>
    </header>
  );
}

export { ObjectHeader };
