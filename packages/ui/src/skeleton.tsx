// 骨架落位：骨架直接放进真实的表格、卡片、列表容器，每行高度是当前字号下的一行（1lh），
// 第二行起按说明文字（11.5px、上间距 3px）排，和真实内容逐行对齐，数据到达那刻不跳。
// 静态显示，不做常驻闪烁动画。外层由调用方（如页面 State）提供 role="status" 和读屏文字。
import type { CSSProperties, ReactNode } from "react";
import { cn } from "cn";
import { Card, CardContent } from "./card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./table";

const widths = ["72%", "48%", "60%", "36%", "54%"];
// 表格单元格宽度由内容决定，百分比会把骨架条压成一个点：表格里改用 em 宽度
const cellWidths = ["7em", "4.5em", "5.5em", "3.5em", "6em"];

function SkeletonLine({ width, className }: { width?: string; className?: string }) {
  return <span aria-hidden className={cn("mg-skeleton-line", className)} style={width ? ({ "--mg-skeleton-w": width } as CSSProperties) : undefined} />;
}

// 一段文字的骨架：第一行跟随当前字号，其余行按说明文字排
function SkeletonText({
  lines = 1,
  seed = 0,
  cell = false,
  className,
}: {
  lines?: number;
  seed?: number;
  // 放在表格单元格里：用固定 em 宽度撑开列
  cell?: boolean;
  className?: string;
}) {
  const set = cell ? cellWidths : widths;
  return (
    <span aria-hidden className={cn("block min-w-0", className)}>
      {Array.from({ length: lines }, (_, i) => (
        <SkeletonLine key={i} width={set[(seed + i) % set.length]} className={i > 0 ? "mt-[3px] text-[11.5px]" : undefined} />
      ))}
    </span>
  );
}

// 表格骨架：表头用真实列名，行用真实的 TableRow / TableCell，单元格内边距和行高与数据表一致
function SkeletonTable({
  columns,
  rows = 5,
  lines = 1,
  card = true,
  className,
}: {
  columns: ReactNode[];
  rows?: number;
  lines?: number;
  // 真实表格不在卡片里时传 false，只保留横向滚动容器
  card?: boolean;
  className?: string;
}) {
  const table = (
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((c, i) => (
                <TableHead key={i}>{c}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: rows }, (_, r) => (
              <TableRow key={r} className="hover:bg-transparent">
                {columns.map((_, c) => (
                  <TableCell key={c}>
                    <SkeletonText cell lines={c === 0 ? lines : 1} seed={r + c} />
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
  );
  if (!card)
    return (
      <div aria-hidden className={cn("overflow-x-auto", className)}>
        {table}
      </div>
    );
  return (
    <Card className={className} aria-hidden>
      <CardContent className="overflow-x-auto">{table}</CardContent>
    </Card>
  );
}

// 列表骨架：rowClassName 传入真实列表行的类（如控制台的 list-row），内边距与分隔线一致
function SkeletonList({
  rows = 4,
  lines = 2,
  action = true,
  className,
  rowClassName,
}: {
  rows?: number;
  lines?: number;
  // 右侧是否有按钮位（按钮高度 32px，比文字高时由它决定行高）
  action?: boolean;
  className?: string;
  rowClassName?: string;
}) {
  return (
    <div aria-hidden className={className}>
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} className={cn(rowClassName ?? "flex items-center justify-between gap-3.5 border-b border-[var(--mg-line-soft)] py-[13px] last:border-b-0")}>
          <SkeletonText lines={lines} seed={r} className="flex-1 text-[13px]" />
          {action && <span className="mg-skeleton block h-8 w-16 shrink-0" />}
        </div>
      ))}
    </div>
  );
}

// 卡片骨架：真实 Card / CardContent，标题行 + 若干说明行
function SkeletonCards({ count = 2, lines = 2, className }: { count?: number; lines?: number; className?: string }) {
  return (
    <div aria-hidden className={cn("grid gap-4", className)}>
      {Array.from({ length: count }, (_, i) => (
        <Card key={i}>
          <CardContent>
            <SkeletonText lines={1} seed={i} className="text-base font-semibold" />
            <SkeletonText lines={Math.max(0, lines - 1)} seed={i + 2} className="text-[11.5px] [&>*]:mt-[3px]" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

export { SkeletonLine, SkeletonText, SkeletonTable, SkeletonList, SkeletonCards };
