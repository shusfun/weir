// 密码 / 令牌输入框：末尾带“显示 / 隐藏”按钮。所有 type="password" 的输入都用它，不再各页手写切换。
// 属性与 Input 相同（type 固定由组件切换）；className 作用于外层组。
import { useState, type ComponentProps } from "react";
import { Eye, EyeOff } from "lucide-react";
import { cn } from "cn";
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "./input-group";

function PasswordInput({ className, disabled, ...props }: Omit<ComponentProps<"input">, "type">) {
  const [shown, setShown] = useState(false);
  return (
    <InputGroup className={className} data-disabled={disabled || undefined}>
      <InputGroupInput {...props} disabled={disabled} type={shown ? "text" : "password"} spellCheck={false} />
      <InputGroupAddon align="inline-end">
        <InputGroupButton
          size="icon-sm"
          disabled={disabled}
          aria-pressed={shown}
          aria-label={shown ? "隐藏密码" : "显示密码"}
          title={shown ? "隐藏密码" : "显示密码"}
          onClick={() => setShown((v) => !v)}
        >
          {shown ? <EyeOff /> : <Eye />}
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  );
}

// 只读展示的密码 / 密钥：默认以圆点遮住，点眼睛查看原文。
function SecretText({ value, label = "密码", className }: { value: string; label?: string; className?: string }) {
  const [shown, setShown] = useState(false);
  return (
    <span className={cn("inline-flex items-center gap-1 align-middle", className)}>
      <span className="font-mono break-all">{shown ? value : "••••••••"}</span>
      <InputGroupButton
        size="icon-xs"
        aria-pressed={shown}
        aria-label={(shown ? "隐藏" : "显示") + label}
        title={(shown ? "隐藏" : "显示") + label}
        onClick={() => setShown((v) => !v)}
      >
        {shown ? <EyeOff /> : <Eye />}
      </InputGroupButton>
    </span>
  );
}

export { PasswordInput, SecretText };
