// 复制到剪贴板：统一权限失败处理与“已复制”反馈（图标 + 文案 2 秒后恢复）。
// 调用方只关心复制什么、成功 / 失败后补充什么提示；不再各自调用 navigator.clipboard。
import { useEffect, useState, type ComponentProps, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "./button";

function CopyButton({
  value,
  children,
  copiedLabel = "已复制",
  icon = true,
  onCopied,
  onError,
  ...props
}: Omit<ComponentProps<typeof Button>, "value" | "onClick" | "onError"> & {
  value: string | (() => string);
  children: ReactNode;
  copiedLabel?: ReactNode;
  icon?: boolean;
  onCopied?: () => void;
  onError?: () => void;
}) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <Button
      type="button"
      {...props}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(typeof value === "function" ? value() : value);
          setCopied(true);
          onCopied?.();
        } catch {
          setCopied(false);
          onError?.();
        }
      }}
    >
      {icon && (copied ? <Check /> : <Copy />)}
      <span aria-live="polite">{copied ? copiedLabel : children}</span>
    </Button>
  );
}

export { CopyButton };
