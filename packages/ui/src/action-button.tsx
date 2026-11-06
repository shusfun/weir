import { useEffect, useRef, useState, type ComponentProps } from "react";
import { AlertCircle, LoaderCircle, X } from "lucide-react";
import { Button } from "./button";
import { CheckMark } from "./check-mark";

export type ActionState = "idle" | "busy" | "ok" | "warn" | "error";

// 即时返回也先绘出处理中态，再接终态；等待只影响反馈，不延迟真实请求。
export function ActionButton({ state = "idle", busyText = "正在处理…", children, disabled, ...props }: Omit<ComponentProps<typeof Button>, "busy"> & { state?: ActionState }) {
  const [shown, setShown] = useState<ActionState>(state);
  const since = useRef(0);
  useEffect(() => {
    if (state === "busy") {
      since.current = performance.now();
      setShown("busy");
      return;
    }
    const delay = shown === "busy" ? Math.max(0, 180 - (performance.now() - since.current)) : 0;
    const timer = setTimeout(() => setShown(state), delay);
    return () => clearTimeout(timer);
  }, [state]); // shown 只在阶段切换时读取，避免重启动效
  const iconOnly = props.size?.startsWith("icon");
  return (
    <Button {...props} disabled={disabled || state === "busy" || shown === "busy"} aria-busy={state === "busy" || shown === "busy" || undefined}>
      <span className="weir-action-content" data-state={shown}>
        <span className="weir-action-rest" aria-hidden={shown !== "idle" || undefined}>{children}</span>
        <span className="weir-action-busy" aria-hidden={shown !== "busy" || undefined}><LoaderCircle className="animate-spin motion-reduce:animate-none" />{!iconOnly && busyText}</span>
        <span className="weir-action-ok" aria-hidden={shown !== "ok" || undefined}>{shown === "ok" && <CheckMark />}{!iconOnly && "已完成"}</span>
        <span className="weir-action-warn" aria-hidden={shown !== "warn" || undefined}><AlertCircle />{!iconOnly && "已保存，检查未知"}</span>
        <span className="weir-action-error" aria-hidden={shown !== "error" || undefined}><X />{!iconOnly && "未完成，重试"}</span>
      </span>
    </Button>
  );
}
