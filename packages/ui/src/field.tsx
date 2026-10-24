// Weir 表单字段：标签 + 控件 + 说明 / 错误。所有“标签在上、控件在下”的表单项都用它，不再手写 .field 容器。
// 只有一个控件子元素且提供 htmlFor 时，自动把说明 / 错误挂到控件的 aria-describedby，错误时标记 aria-invalid。
import { Children, cloneElement, isValidElement, type ComponentProps, type ReactElement, type ReactNode } from "react";
import { cn } from "cn";
import { Label } from "./label";

function Field({
  label,
  htmlFor,
  hint,
  error,
  className,
  children,
  ...props
}: Omit<ComponentProps<"div">, "children"> & {
  label?: ReactNode;
  htmlFor?: string;
  hint?: ReactNode;
  error?: ReactNode;
  children?: ReactNode;
}) {
  const hintId = hint && htmlFor ? htmlFor + "-hint" : undefined;
  const errorId = error && htmlFor ? htmlFor + "-error" : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;
  const only = Children.count(children) === 1 ? Children.only(children) : null;
  const control =
    describedBy && isValidElement(only)
      ? cloneElement(only as ReactElement<Record<string, unknown>>, {
          "aria-describedby": [(only.props as { "aria-describedby"?: string })["aria-describedby"], describedBy].filter(Boolean).join(" "),
          ...(error ? { "aria-invalid": true } : {}),
        })
      : children;
  return (
    <div data-slot="field" className={cn("grid min-w-0 gap-1.5", className)} {...props}>
      {label && <Label htmlFor={htmlFor}>{label}</Label>}
      {control}
      {hint && (
        <small id={hintId} className="text-[11.5px] leading-relaxed text-[var(--mg-ink-3)]">
          {hint}
        </small>
      )}
      {error && (
        <p id={errorId} role="alert" className="m-0 text-xs leading-relaxed text-[var(--mg-bad-ink)]">
          {error}
        </p>
      )}
    </div>
  );
}

export { Field };
