// 只读的地址 / 令牌框：末尾是复制图标（CopyButton 纯图标），点框内任意处全选，方便手动复制。
// 订阅地址、分享地址、一次性令牌都用它，不再是「输入框 + 带文字的复制按钮」两块拼。
// 用 setSelectionRange 而不是 select()：iOS Safari 上只读输入框的 select() 不生效。
import type { ComponentProps } from "react";
import { cn } from "cn";
import { CopyButton } from "./copy-button";
import { InputGroup, InputGroupAddon, InputGroupInput } from "./input-group";

function CopyField({
  value,
  label,
  copyLabel = "复制",
  onCopied,
  onError,
  className,
  inputClassName,
}: {
  value: string;
  // label：输入框的无障碍名称，如「订阅地址」
  label: string;
  copyLabel?: string;
  onCopied?: () => void;
  onError?: () => void;
  className?: string;
  inputClassName?: string;
}) {
  const selectAll = (e: { currentTarget: HTMLInputElement }) => e.currentTarget.setSelectionRange(0, e.currentTarget.value.length);
  return (
    <InputGroup data-slot="copy-field" className={cn("bg-[var(--weir-well-deep)]", className)}>
      <InputGroupInput
        readOnly
        aria-label={label}
        value={value}
        spellCheck={false}
        className={cn("font-mono text-xs read-only:bg-transparent", inputClassName)}
        onFocus={selectAll}
        onClick={selectAll}
      />
      <InputGroupAddon align="inline-end">
        <CopyButton value={value} aria-label={`${copyLabel}${label}`} onCopied={onCopied} onError={onError} />
      </InputGroupAddon>
    </InputGroup>
  );
}

export { CopyField };
