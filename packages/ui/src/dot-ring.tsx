// 点阵进度环（设计稿运行观测右上角）：五层点组成的圆环，按步骤分成几段；
// 已完成的段是绿色，当前段是强调色，其余是暗点。只画静态 SVG，不做持续动画。
import type { ReactNode } from "react";
import { cn } from "cn";

function DotRing({
  done,
  total,
  label,
  caption,
  size = 381,
  className,
}: {
  /** 已完成的段数；中间显示当前在第几步（done + 1，全部完成时为 total）。缺失时整环画成暗点，中间写未知 */
  done: number | null | undefined;
  total: number;
  label: ReactNode;
  caption?: ReactNode;
  size?: number;
  className?: string;
}) {
  const known = typeof done === "number" && Number.isFinite(done) && total > 0;
  const c = size / 2;
  const dots: { x: number; y: number; r: number; o: number; tone: "ok" | "acc" | "off" }[] = [];
  for (let k = 0; k < 5; k++) {
    const r = size * 0.37 + k * 9;
    const n = 121 + k * 9;
    for (let i = 0; i < n; i++) {
      const seg = Math.floor((i / n) * total);
      const t = ((i / n) * 360 - 90) * (Math.PI / 180);
      const j = Math.sin(i * 12.9898 + k * 78.233) * 2.3;
      const isDone = known && seg < done;
      const cur = known && seg === done;
      dots.push({
        x: c + (r + j) * Math.cos(t),
        y: c + (r + j) * Math.sin(t),
        r: isDone ? 1.9 : cur ? 2.3 - k * 0.2 : 1.3,
        o: isDone ? 0.9 - k * 0.13 : cur ? 1 - k * 0.15 : 0.47 - k * 0.07,
        tone: isDone ? "ok" : cur ? "acc" : "off",
      });
    }
  }
  return (
    <div data-slot="dot-ring" className={cn("weir-dot-ring", className)} style={{ width: size, height: size }}>
      <svg viewBox={`0 0 ${size} ${size}`} width={size} height={size} aria-hidden>
        {dots.map((d, i) => (
          <circle key={i} cx={d.x.toFixed(1)} cy={d.y.toFixed(1)} r={d.r.toFixed(2)} opacity={d.o.toFixed(2)} data-tone={d.tone} />
        ))}
      </svg>
      <div className="weir-dot-ring-center">
        <span className="weir-dot-ring-caption">{caption}</span>
        <span className="weir-dot-ring-value">
          {known ? <>{Math.min(done + 1, total)}<small> / {total}</small></> : "未知"}
        </span>
        <b>{label}</b>
      </div>
    </div>
  );
}

// 体检环（设计稿 VPS 详情左侧）：三层点按项数均分，每段一项；有问题的段是红点，未知的段是暗点，正常是绿点。
function HealthRing({ segments, title, caption, size = 259 }: { segments: ("ok" | "bad" | "warn" | "unknown")[]; title: ReactNode; caption: ReactNode; size?: number }) {
  const c = size / 2;
  const n = Math.max(segments.length, 1);
  const dots: { x: number; y: number; r: number; o: number; tone: string }[] = [];
  for (let k = 0; k < 3; k++) {
    const r = size * 0.38 + k * 9;
    const count = 91 + k * 7;
    for (let i = 0; i < count; i++) {
      const pos = (i / count) * n;
      const seg = Math.floor(pos);
      if (pos % 1 > 0.91) continue;
      const t = ((i / count) * 360 - 90) * (Math.PI / 180);
      const tone = segments[seg] ?? "unknown";
      const bad = tone === "bad" || tone === "warn";
      dots.push({ x: c + r * Math.cos(t), y: c + r * Math.sin(t), r: bad ? 2.3 : 1.7, o: bad ? 1 - k * 0.2 : 0.71 - k * 0.17, tone });
    }
  }
  return (
    <div data-slot="health-ring" className="weir-dot-ring" style={{ width: size, height: size }}>
      <svg viewBox={`0 0 ${size} ${size}`} width={size} height={size} aria-hidden>
        {dots.map((d, i) => (
          <circle key={i} cx={d.x.toFixed(1)} cy={d.y.toFixed(1)} r={d.r.toFixed(2)} opacity={d.o.toFixed(2)} data-tone={d.tone === "ok" ? "ok" : d.tone === "unknown" ? "off" : d.tone} />
        ))}
      </svg>
      <div className="weir-dot-ring-center">
        <span className="text-title1 font-bold leading-[35px]">{title}</span>
        <span className="weir-dot-ring-caption">{caption}</span>
      </div>
    </div>
  );
}

export { DotRing, HealthRing };
