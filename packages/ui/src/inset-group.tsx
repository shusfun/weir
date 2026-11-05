// iOS inset grouped：一张卡片就是一组，组里的行用分隔线隔开，不再在卡片里套带边框的小卡片。
// 卡片内容区已有左右内边距，分隔线从文字起点开始、不贯穿卡片（等同 iOS 的缩进分隔线）。
import type { ComponentProps, ElementType, ReactNode } from "react";
import { cn } from "cn";
import { Card, CardContent } from "./card";
import { SectionHeader } from "./page";

// InsetGroup：外层唯一的一张卡片，可带区块标题；其余属性（如 dataObject）给到卡片本身。
function InsetGroup({
  title,
  description,
  actions,
  contentClassName,
  children,
  ...props
}: Omit<ComponentProps<typeof Card>, "title"> & {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  contentClassName?: string;
}) {
  return (
    <Card {...props}>
      <CardContent className={cn("stack", contentClassName)}>
        {title && <SectionHeader flush title={title} description={description} actions={actions} />}
        {children}
      </CardContent>
    </Card>
  );
}

// InsetRows / InsetRow：组里的行。as="ul" / "li" 时是真列表。首行不留上边距、末行不留下边距，由卡片内边距收口。
function InsetRows<T extends ElementType = "div">({ as, className, ...props }: { as?: T; className?: string } & Omit<ComponentProps<T>, "as" | "className">) {
  const Tag: ElementType = as ?? "div";
  return <Tag data-slot="inset-rows" className={cn("m-0 grid min-w-0 list-none divide-y divide-[var(--weir-line-soft)] p-0", className)} {...props} />;
}

function InsetRow<T extends ElementType = "div">({ as, className, ...props }: { as?: T; className?: string } & Omit<ComponentProps<T>, "as" | "className">) {
  const Tag: ElementType = as ?? "div";
  return <Tag data-slot="inset-row" className={cn("min-w-0 py-3 first:pt-0 last:pb-0", className)} {...props} />;
}

export { InsetGroup, InsetRows, InsetRow };
