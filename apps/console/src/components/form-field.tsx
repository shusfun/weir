// 文本类表单项的便捷写法：标签 + 单行输入 / 多行文本；type="password" 时带显示 / 隐藏按钮。布局、说明与校验由 @weir/ui 的 Field 负责。
// 默认不必填；必填项显式传 required。
import { Field, Input, PasswordInput, Textarea } from "@weir/ui";
export function TextField({
  label,
  name,
  field,
  type = "text",
  value,
  onChange,
  required = false,
  autoComplete,
  hint,
  customError,
  placeholder,
  className,
}: {
  label: string;
  // 控件 id
  name: string;
  // 在 <Form errors> 中的字段名
  field?: string;
  type?: string;
  value: string | number | null | undefined;
  onChange: (v: string) => void;
  required?: boolean;
  autoComplete?: string;
  hint?: string;
  customError?: string;
  placeholder?: string;
  className?: string;
}) {
  const common = {
    id: name,
    value: value ?? "",
    required,
    placeholder,
    onChange: (e: { target: { value: string } }) => onChange(e.target.value),
  };
  return (
    <Field label={label} htmlFor={name} name={field} hint={hint} customError={customError} className={className}>
      {type === "textarea" ? (
        <Textarea {...common} rows={5} autoComplete="off" />
      ) : type === "password" ? (
        <PasswordInput {...common} autoComplete={autoComplete ?? "new-password"} />
      ) : (
        <Input {...common} type={type} autoComplete={autoComplete} />
      )}
    </Field>
  );
}
