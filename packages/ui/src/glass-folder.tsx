// VPS 玻璃文件夹（设计稿 .fd）：背板是带色调的渐变，夹里露出纸面色的节点文件（微微错开倾斜），
// 前板是磨砂玻璃，写名称、地址、状态标签，底部是 CPU 点阵与国内延迟。整块可点进详情。
// 省电档前板改实底，不做 backdrop-filter。色调按 hue 在两套稿子配色之间取，同一台 VPS 每次一样。
import { type CSSProperties, type ReactNode } from "react";
import { cn } from "cn";
import { Badge } from "./badge";
import { StatusLamp, type LampTone } from "./lamp";

type FolderFile = {
  id: string;
  /** 小标题（稿子是「节点」） */
  kind?: ReactNode;
  title: ReactNode | null;
  /** 端口等等宽小字 */
  meta?: ReactNode | null;
  tone?: LampTone | null;
};

type FolderStatus = { tone: "ok" | "warn" | "bad" | "neutral"; text: ReactNode };

const tilts = [-7, -1, 5];

function GlassFolder({
  name,
  address,
  status,
  files,
  cpu,
  caption,
  metric,
  metricUnit,
  metricLabel,
  hue = 0,
  headEnd,
  href,
  onClick,
  className,
}: {
  name: ReactNode;
  /** 不传就不显示地址行；传 null 显示未知 */
  address?: ReactNode | null;
  /** 右上角状态标签；不传就不显示 */
  status?: FolderStatus | null;
  files: FolderFile[];
  /** CPU 占用百分比（0–100）；缺失时点阵画成未知 */
  cpu?: number | null;
  /** 点阵下的一行说明，如「CPU 22% · 3 个节点」 */
  caption?: ReactNode;
  /** 右下角读数（如国内延迟）；缺失显示未知 */
  metric?: ReactNode | null;
  metricUnit?: ReactNode;
  metricLabel?: ReactNode;
  /** 0 = 蓝调，1 = 紫调；多台 VPS 交替 */
  hue?: 0 | 1;
  /** 标题行右侧的等宽读数（详情页写「CPU 22% · 151 ms」） */
  headEnd?: ReactNode;
  href?: string;
  onClick?: () => void;
  className?: string;
}) {
  const shown = files.slice(0, 3);
  const known = typeof cpu === "number" && Number.isFinite(cpu);
  const on = known ? Math.max(0, Math.min(42, Math.round((cpu / 100) * 42))) : 0;
  const body = (
    <>
      <span className="weir-folder-back" data-hue={hue} aria-hidden />
      {shown.map((file, i) => (
        <span key={file.id} className="weir-folder-file" style={{ "--i": String(i), "--tilt": `${tilts[i]}deg` } as CSSProperties} aria-hidden>
          <span className="weir-folder-file-kind">{file.kind ?? "节点"}</span>
          <span className="weir-folder-file-title">{file.title == null || file.title === "" ? "未知" : file.title}</span>
          {file.meta != null && file.meta !== "" && <span className="weir-folder-file-meta">{file.meta}</span>}
        </span>
      ))}
      <span className="weir-folder-front">
        <span className="weir-folder-head">
          <span className="min-w-0">
            <span className="weir-folder-name">{name}</span>
            {address !== undefined && <span className="weir-folder-addr">{address == null || address === "" ? "未知" : address}</span>}
          </span>
          {headEnd}
          {status && (
            <Badge variant="soft" tone={status.tone === "neutral" ? "neutral" : status.tone}>
              <StatusLamp tone={status.tone === "neutral" ? "off" : status.tone} size="sm" />
              {status.text}
            </Badge>
          )}
        </span>
        <span className="weir-folder-foot">
          <span className="min-w-0">
            <span className="weir-folder-dots" role="img" aria-label={known ? `CPU ${Math.round(cpu)}%` : "CPU：未知"}>
              {Array.from({ length: 42 }, (_, i) => (
                <i key={i} data-on={known && i < on ? (cpu >= 80 ? "warn" : "accent") : undefined} data-unknown={!known || undefined} />
              ))}
            </span>
            {caption && <span className="weir-folder-caption">{caption}</span>}
          </span>
          {metricLabel && (
            <span className="weir-folder-metric">
              <span className="weir-digits">
                {metric == null || metric === "" ? "未知" : metric}
                {metric != null && metric !== "" && metricUnit && <small> {metricUnit}</small>}
              </span>
              <span className="weir-folder-caption">{metricLabel}</span>
            </span>
          )}
        </span>
      </span>
    </>
  );
  const cls = cn("weir-folder", className);
  if (href) {
    return (
      <a data-slot="glass-folder" className={cls} href={href}>
        {body}
      </a>
    );
  }
  if (onClick) {
    return (
      <button type="button" data-slot="glass-folder" className={cls} onClick={onClick}>
        {body}
      </button>
    );
  }
  return (
    <div data-slot="glass-folder" className={cls}>
      {body}
    </div>
  );
}

// 空位（稿子「添加 VPS · 用 SSH 一次装好」）：虚线框，与文件夹同高。
function FolderSlot({ title, caption, onClick, className }: { title: ReactNode; caption?: ReactNode; onClick: () => void; className?: string }) {
  return (
    <button type="button" data-slot="folder-slot" className={cn("weir-folder-slot", className)} onClick={onClick}>
      <span className="weir-folder-slot-plus" aria-hidden>+</span>
      <b>{title}</b>
      {caption && <span>{caption}</span>}
    </button>
  );
}

export { GlassFolder, FolderSlot };
export type { FolderFile, FolderStatus };
