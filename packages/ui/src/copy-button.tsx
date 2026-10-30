// 复制到剪贴板：统一权限失败处理与“已复制”反馈。调用方只关心复制什么、成功 / 失败后补充什么提示；
// 不再各自调用 navigator.clipboard。不给 children 时是纯图标按钮（必须给 aria-label），旁边紧挨着复制内容时用。
// 反馈：复制图标缩小淡出、对勾放大淡入并一笔画出；2 秒后反向切回。两个图标叠在同一格，按钮宽度不变。
import { useEffect, useState, type ComponentProps, type ReactNode } from "react";
import { Copy } from "lucide-react";
import { Button } from "./button";
import { CheckMark } from "./check-mark";

function CopyIcon({ copied, replay }: { copied: boolean; replay: number }) {
  return (
    <span data-slot="copy-icon" data-copied={copied || undefined} className="weir-copy-icon" aria-hidden>
      <Copy className="weir-copy-icon-copy" />
      {/* replay 只在成功时增加：换 key 让对勾每次重新描绘；切回时不重画，只随外层淡出 */}
      <CheckMark key={replay} animate={replay > 0} className="weir-copy-icon-check" />
    </span>
  );
}

function CopyButton({
  value,
  children,
  copiedLabel = "已复制",
  icon = true,
  size,
  variant,
  onCopied,
  onError,
  ...props
}: Omit<ComponentProps<typeof Button>, "value" | "onClick" | "onError" | "children"> & {
  value: string | (() => string);
  children?: ReactNode;
  copiedLabel?: string;
  icon?: boolean;
  onCopied?: () => void;
  onError?: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [replay, setReplay] = useState(0);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied, replay]);
  const iconOnly = children === undefined;
  return (
    <Button
      type="button"
      size={size ?? (iconOnly ? "icon-sm" : undefined)}
      variant={variant ?? (iconOnly ? "ghost" : undefined)}
      title={iconOnly ? (copied ? copiedLabel : props["aria-label"]) : undefined}
      {...props}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(typeof value === "function" ? value() : value);
          setCopied(true);
          setReplay((n) => n + 1);
          onCopied?.();
        } catch {
          setCopied(false);
          onError?.();
        }
      }}
    >
      {(icon || iconOnly) && <CopyIcon copied={copied} replay={replay} />}
      {iconOnly ? (
        <span className="sr-only" aria-live="polite">{copied ? copiedLabel : ""}</span>
      ) : (
        <span aria-live="polite">{copied ? copiedLabel : children}</span>
      )}
    </Button>
  );
}

export { CopyButton };
