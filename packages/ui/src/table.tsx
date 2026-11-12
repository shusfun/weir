// 来源 codedthemes/uiable table；本地改为 iOS 分组列表风格的平面表格：表头用说明字号的次要文字，行之间只有细分隔线，
// 行高不小于 44。可点的行（带 onClick 或 data-object）按下叠填充色。
import { ComponentProps } from "react";
import { cn } from "cn";
import { ScrollArea } from "./scroll-area";

function Table({ className, ...props }: ComponentProps<"table">) {
  return (
    <ScrollArea data-slot="table-container" orientation="horizontal" className="w-full" contentRender={false}>
      <table data-slot="table" className={cn("w-full caption-bottom border-collapse text-body", className)} {...props} />
    </ScrollArea>
  );
}

function TableHeader({ className, ...props }: ComponentProps<"thead">) {
  return <thead data-slot="table-header" className={cn("border-b border-[var(--weir-line-soft)] text-left", className)} {...props} />;
}

function TableBody({ className, ...props }: ComponentProps<"tbody">) {
  return <tbody data-slot="table-body" className={cn("[&_tr:last-child]:border-0", className)} {...props} />;
}

function TableFooter({ className, ...props }: ComponentProps<"tfoot">) {
  return (
    <tfoot data-slot="table-footer" className={cn("border-t border-[var(--weir-line-soft)] bg-[var(--weir-fill-4)] font-medium", className)} {...props} />
  );
}

function TableRow({ className, ...props }: ComponentProps<"tr">) {
  const pressable = !!props.onClick || (props as Record<string, unknown>)["data-object"] !== undefined;
  return (
    <tr
      data-slot="table-row"
      className={cn(
        "border-b border-[var(--weir-line-soft)] transition-colors hover:bg-[var(--weir-fill-4)] data-[state=selected]:bg-[var(--weir-accent-soft)]",
        // 分组行（稿子 .v-grp-h）：凹面底、11px 粗体次要文字、上下各 7
        "data-[group-row]:bg-[var(--weir-well)] data-[group-row]:hover:bg-[var(--weir-well)] [&[data-group-row]>td]:h-auto [&[data-group-row]>td]:py-[7px] [&[data-group-row]>td]:text-fine [&[data-group-row]>td]:font-bold [&[data-group-row]>td]:tracking-[0.05em] [&[data-group-row]>td]:text-[var(--weir-ink-3)]",
        pressable && "weir-press-fill cursor-pointer active:bg-[var(--weir-fill)]",
        className,
      )}
      {...props}
    />
  );
}

function TableHead({ className, ...props }: ComponentProps<"th">) {
  return (
    <th
      data-slot="table-head"
      className={cn("h-[41px] px-[15px] align-middle text-fine font-semibold text-[var(--weir-ink-3)] whitespace-nowrap [&:has([role=checkbox])]:pr-0", className)}
      {...props}
    />
  );
}

function TableCell({ className, ...props }: ComponentProps<"td">) {
  return (
    <td
      data-slot="table-cell"
      className={cn("h-[45px] px-[15px] py-[11px] align-middle [&:has([role=checkbox])]:pr-0", className)}
      {...props}
    />
  );
}

function TableCaption({ className, ...props }: ComponentProps<"caption">) {
  return <caption data-slot="table-caption" className={cn("mt-4 text-meta text-[var(--weir-ink-3)]", className)} {...props} />;
}

export { Table, TableHeader, TableBody, TableFooter, TableHead, TableRow, TableCell, TableCaption };
