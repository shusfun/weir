// 按住确认：按住一段时间才触发，进度从左往右填满。键盘按住回车或空格同样有效。
// 省电档不做过渡，进度仍要按住才会触发。松手或移出即取消。
import { useEffect, useRef, useState, type ComponentProps, type CSSProperties, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { cn } from "cn";
import { buttonVariants } from "./button";

function holdMs() {
  if (typeof document === "undefined") return 600;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--weir-settle");
  const n = parseFloat(raw);
  return Number.isFinite(n) && n > 0 ? n : 600;
}

function HoldButton({
  onHold,
  children,
  className,
  variant = "default",
  size = "default",
  disabled,
  ...props
}: Omit<ComponentProps<"button">, "onClick"> & {
  onHold: () => void;
  /** 主次由材质区分；同一操作组沿用 Button 的尺寸档位。 */
  variant?: "default" | "destructive" | "outline";
  size?: "default" | "sm" | "lg";
}) {
  const [ratio, setRatio] = useState(0);
  const raf = useRef(0);
  const done = useRef(false);
  useEffect(() => () => cancelAnimationFrame(raf.current), []);
  useEffect(() => {
    if (disabled) stop();
  }, [disabled]);

  function stop() {
    cancelAnimationFrame(raf.current);
    raf.current = 0;
    if (!done.current) setRatio(0);
  }

  function start() {
    if (disabled || raf.current) return;
    done.current = false;
    const ms = holdMs();
    const t0 = performance.now();
    const tick = (now: number) => {
      if (document.visibilityState === "hidden") {
        stop();
        return;
      }
      const next = Math.min(1, (now - t0) / ms);
      setRatio(next);
      if (next >= 1) {
        done.current = true;
        raf.current = 0;
        onHold();
        setRatio(0);
        done.current = false;
        return;
      }
      raf.current = requestAnimationFrame(tick);
    };
    raf.current = requestAnimationFrame(tick);
  }

  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (e.repeat || (e.key !== "Enter" && e.key !== " ")) return;
    e.preventDefault();
    start();
  }

  return (
    <button
      {...props}
      type="button"
      disabled={disabled}
      data-slot="hold-button"
      className={cn(buttonVariants({ variant, size }), "weir-hold", className)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(ratio * 100)}
      aria-valuetext={ratio > 0 ? "按住确认中" : "按住以确认"}
      onPointerDown={(e: ReactPointerEvent<HTMLButtonElement>) => {
        if (e.button !== 0) return;
        start();
      }}
      onPointerUp={stop}
      onPointerLeave={stop}
      onPointerCancel={stop}
      onKeyDown={onKeyDown}
      onKeyUp={stop}
      onBlur={stop}
    >
      <span className="weir-hold-fill" style={{ "--hold": String(ratio) } as CSSProperties} aria-hidden />
      <span className="weir-hold-label">{children}</span>
    </button>
  );
}

export { HoldButton };
