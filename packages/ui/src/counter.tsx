// 机械滚动计数器：每一位是一条 0-9 数字带（6 份循环），数值变化时各位独立滚动。
// 所有位同一时长，低位走的距离更长所以更快；弹簧曲线带轻微过冲。首次显示不滚动。
import { useLayoutEffect, useRef, useState } from "react";
import { cn } from "cn";

const copies = 6;
const middle = 30; // 静止时停在第 4 份，向上向下各可走 29 格
const strip = Array.from({ length: copies * 10 }, (_, i) => i % 10);

function Column({ digit, turns, dir, tick }: { digit: number; turns: number; dir: number; tick: number }) {
  const [state, setState] = useState({ index: middle + digit, animate: false });
  const prev = useRef(digit);
  useLayoutEffect(() => {
    const p = prev.current;
    prev.current = digit;
    if (!dir) return;
    const steps = (dir > 0 ? (digit - p + 10) % 10 : (p - digit + 10) % 10) + 10 * turns;
    if (!steps) return;
    setState({ index: middle + p + dir * steps, animate: true });
  }, [digit, turns, dir, tick]);
  return (
    <span className="mg-counter-digit" aria-hidden>
      <span
        className="mg-counter-strip"
        style={{
          transform: `translateY(${-state.index * 1.2}em)`,
          transition: state.animate ? "transform var(--mg-slow) var(--mg-spring)" : "none",
        }}
        // 停稳后无动画地归位到中间一份，下次可继续双向滚动
        onTransitionEnd={() => setState({ index: middle + digit, animate: false })}
      >
        {strip.map((n, i) => (
          <span key={i}>{n}</span>
        ))}
      </span>
    </span>
  );
}

const numeric = /^([+-]?[\d,]*\.?\d+)(.*)$/;
// 只有“数字 + 可选单位”才滚动；日期、版本号、未知、长期有效等保持静态
export function isCountable(value: unknown) {
  if (typeof value === "number") return Number.isFinite(value);
  if (typeof value !== "string") return false;
  const m = numeric.exec(value.trim());
  return !!m && !/\d/.test(m[2]);
}

function Counter({ value, framed = false, className }: { value: string | number; framed?: boolean; className?: string }) {
  const text = String(value).trim();
  const match = numeric.exec(text);
  const head = match ? match[1] : text;
  const tail = match ? match[2] : "";
  const amount = Number(head.replace(/[,.]/g, ""));
  // 渲染期派生上一次数值，保证子列拿到的是本次变化量
  const [snap, setSnap] = useState({ amount, delta: 0, tick: 0 });
  if (snap.amount !== amount) setSnap({ amount, delta: amount - snap.amount, tick: snap.tick + 1 });
  const dir = Math.sign(snap.delta);
  const chars = [...head];
  let position = chars.filter((c) => /\d/.test(c)).length;
  return (
    <span className={cn("mg-counter", className)} data-framed={framed} role="img" aria-label={text}>
      {chars.map((c, i) => {
        if (!/\d/.test(c)) return <span key={"s" + i} className="mg-counter-static" aria-hidden>{c}</span>;
        position -= 1;
        const turns = Math.min(2, Math.floor(Math.abs(snap.delta) / 10 ** (position + 1)));
        return <Column key={"d" + (chars.length - i)} digit={Number(c)} turns={turns} dir={dir} tick={snap.tick} />;
      })}
      {tail && <span className="mg-counter-static mg-readout-unit" aria-hidden>{tail}</span>}
    </span>
  );
}

export { Counter };
