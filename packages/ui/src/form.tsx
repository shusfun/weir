// Weir 表单：参照 Element Plus el-form。统一字段间距并为每个字段预留说明 / 错误行（错误出现时布局不跳动）；
// 提交时浏览器先按控件约束校验，Field 拦下原生气泡、显示在字段下方并聚焦第一个错误；通过后才调用 onSubmit。
// busy 时整张表单禁用；errors 按字段 name 分发到对应 Field（服务端返回的字段错误），用户改动该字段后自动隐藏；
// 服务端字段错误返回后聚焦第一个出错的字段（提交期间表单禁用，焦点已丢失）。
// 弹窗里的 <Form> 与 <form> 一样：DialogFooter 里的提交按钮自动关联到它。
import { createContext, useContext, useEffect, useMemo, useRef, type ComponentProps, type FormEvent, type ReactNode } from "react";
import { cn } from "cn";

type FormContextValue = { errors: Partial<Record<string, ReactNode>> };
const FormContext = createContext<FormContextValue | null>(null);

function useFormContext() {
  return useContext(FormContext);
}

function Form({
  busy = false,
  errors,
  onSubmit,
  className,
  children,
  ref: outerRef,
  ...props
}: Omit<ComponentProps<"form">, "onSubmit"> & {
  busy?: boolean;
  errors?: Partial<Record<string, ReactNode>>;
  onSubmit?: (e: FormEvent<HTMLFormElement>) => void;
}) {
  const value = useMemo(() => ({ errors: errors ?? {} }), [errors]);
  const ref = useRef<HTMLFormElement>(null);
  const hasErrors = !!errors && Object.values(errors).some(Boolean);
  // Field 的布局副作用先于此处执行，出错的控件已带 aria-invalid。
  useEffect(() => {
    if (!hasErrors || busy) return;
    ref.current?.querySelector<HTMLElement>("[aria-invalid=true]")?.focus();
  }, [errors, hasErrors, busy]);
  return (
    <FormContext.Provider value={value}>
      <form
        ref={(el) => {
          ref.current = el;
          if (typeof outerRef === "function") outerRef(el);
          else if (outerRef) outerRef.current = el;
        }}
        data-slot="form"
        aria-busy={busy || undefined}
        onSubmit={(e) => {
          e.preventDefault();
          if (!busy) onSubmit?.(e);
        }}
        {...props}
      >
        <fieldset disabled={busy} className={cn("m-0 grid min-w-0 gap-3 border-0 p-0", className)}>
          {children}
        </fieldset>
      </form>
    </FormContext.Provider>
  );
}

// 表单内的多列行：窄屏单列，sm 起两列 / 三列。替代各页手写的 grid gap-* sm:grid-cols-*。
function FormGrid({ cols = 2, className, ...props }: ComponentProps<"div"> & { cols?: 2 | 3 }) {
  return (
    <div
      data-slot="form-grid"
      className={cn("grid min-w-0 gap-3", cols === 2 ? "sm:grid-cols-2" : "sm:grid-cols-2 lg:grid-cols-3", className)}
      {...props}
    />
  );
}

export { Form, FormGrid, useFormContext };
