// 液态滑块：基于 Base UI Slider（键盘、读屏、表单语义照旧）。轨道填充、滑钮、数值气泡由同一个
// CSS 变量 --mg-slider-p 驱动，用同一条曲线、同一时长（--mg-settle / --mg-decel）一起走，数字用
// Counter 滚动且时长相同——三条动画同频，看起来是同一个东西。
// 拖动时（reason = drag）去掉过渡直接跟手；点击轨道、键盘调整时三者一起滑到位。
// 只动 transform：滑钮位置 = 比例 × 轨道宽度（ResizeObserver 写入 --mg-slider-w），填充用 scaleX。
import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Slider as SliderPrimitive } from "@base-ui/react/slider";
import { cn } from "cn";
import { Counter } from "./counter";

function Slider({
  value,
  defaultValue,
  onValueChange,
  onValueCommitted,
  min = 0,
  max = 100,
  format = (v) => v,
  showValue = true,
  className,
  "aria-label": ariaLabel,
  ...props
}: Omit<SliderPrimitive.Root.Props<number>, "value" | "defaultValue"> & {
  value?: number;
  defaultValue?: number;
  // 气泡里的文字；返回数字时用 Counter 滚动
  format?: (value: number) => ReactNode;
  showValue?: boolean;
  "aria-label"?: string;
}) {
  const [inner, setInner] = useState(defaultValue ?? min);
  const [follow, setFollow] = useState(false);
  const [width, setWidth] = useState(0);
  // 首次测量和轨道宽度变化时不做过渡，否则滑钮会从左端滑到当前值
  const [still, setStill] = useState(true);
  const track = useRef<HTMLDivElement>(null);
  const current = value ?? inner;
  const ratio = max > min ? Math.min(1, Math.max(0, (current - min) / (max - min))) : 0;
  useLayoutEffect(() => {
    const el = track.current;
    if (!el) return;
    let frame = 0;
    const measure = () => {
      setStill(true);
      setWidth(el.offsetWidth);
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => requestAnimationFrame(() => setStill(false)));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, []);
  const shown = format(current);
  return (
    <SliderPrimitive.Root
      data-slot="slider"
      data-follow={follow || still || undefined}
      className={cn("mg-slider", className)}
      style={{ "--mg-slider-p": ratio, "--mg-slider-w": width + "px" } as CSSProperties}
      value={current}
      min={min}
      max={max}
      onValueChange={(next, details) => {
        setFollow(details.reason === "drag");
        setInner(next as number);
        onValueChange?.(next, details);
      }}
      onValueCommitted={(next, details) => {
        setFollow(false);
        onValueCommitted?.(next, details);
      }}
      {...props}
    >
      <SliderPrimitive.Control className="mg-slider-control">
        <SliderPrimitive.Track ref={track} className="mg-slider-track">
          <span className="mg-slider-fill-clip" aria-hidden>
            <span className="mg-slider-fill" />
          </span>
          <SliderPrimitive.Thumb className="mg-slider-thumb" aria-label={ariaLabel}>
            <span className="mg-slider-knob" aria-hidden />
            {showValue && (
              <span className="mg-slider-bubble" aria-hidden>
                {/* 拖动时数字直接跟手；松手或键盘调整时与滑钮同频滚动 */}
                {!follow && typeof shown === "number" ? <Counter value={shown} /> : shown}
              </span>
            )}
          </SliderPrimitive.Thumb>
        </SliderPrimitive.Track>
      </SliderPrimitive.Control>
    </SliderPrimitive.Root>
  );
}

export { Slider };
