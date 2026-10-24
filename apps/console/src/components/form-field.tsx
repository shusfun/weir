// 文本类表单项的便捷写法：标签 + 单行输入 / 多行文本。布局与说明由 @weir/ui 的 Field 负责。
import { Field, Input, Textarea } from "@weir/ui";
export function TextField({
  label,
  name,
  type = "text",
  value,
  onChange,
  required = true,
  autoComplete,
  hint,
  className,
}: {
  label: string;
  name: string;
  type?: string;
  value: any;
  onChange: (v: string) => void;
  required?: boolean;
  autoComplete?: string;
  hint?: string;
  className?: string;
}) {
  return (
    <Field label={label} htmlFor={name} hint={hint} className={className}>
      {type === "textarea" ? (
        <Textarea
          id={name}
          value={value ?? ""}
          rows={5}
          required={required}
          onChange={(e) => onChange(e.target.value)}
          autoComplete="off"
        />
      ) : (
        <Input
          id={name}
          type={type}
          value={value ?? ""}
          onChange={(e) => onChange(e.target.value)}
          required={required}
          autoComplete={autoComplete ?? (type === "password" ? "new-password" : undefined)}
        />
      )}
    </Field>
  );
}
