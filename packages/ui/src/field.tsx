// Weir 表单字段：标签 + 控件 + 说明 / 错误。所有“标签在上、控件在下”的表单项都用它，不再手写 .field 容器。
// 校验参照 Element Plus el-form-item：必填项标签前加红色 *；不弹浏览器气泡，错误以红字显示在控件下方、控件描红；
// 改过的字段失焦时校验，出错后每次变化都重新校验；提交时全部校验并聚焦第一个错误字段。
// 规则沿用控件的原生约束（required / minLength / pattern / type 等），文案按标签生成；
// 跨字段规则（如两次密码一致）传 customError；服务端返回的字段错误传 error，或在 <Form errors> 里按 name 给出。
// 在 <Form> 里时说明 / 错误行始终占位，错误出现不推动布局；标签与控件自动关联，不必手写 htmlFor。
import {
  Children,
  cloneElement,
  isValidElement,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ComponentProps,
  type ReactElement,
  type ReactNode,
} from "react";
import { cn } from "cn";
import { useFormContext } from "./form";
import { Label } from "./label";

type Control = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;

// 校验文案用的名称：去掉“（可选）”“（至少 6 位…）”等括注与结尾冒号
function fieldName(label: ReactNode, focus: Element | null) {
  const raw = typeof label === "string" ? label : focus?.getAttribute("aria-label") ?? "";
  return raw.replace(/[（(].*$/, "").replace(/[：:\s]+$/, "").trim();
}

function describe(control: Control, name: string): string {
  const v = control.validity;
  if (v.valid) return "";
  if (v.customError) return control.validationMessage;
  const type = control instanceof HTMLInputElement ? control.type : "";
  // 下拉框 / 开关等组件用隐藏输入承载取值，缺值时提示“请选择”
  const choose =
    control instanceof HTMLSelectElement ||
    control.getAttribute("aria-hidden") === "true" ||
    ["checkbox", "radio", "file"].includes(type);
  if (v.badInput) return type === "number" ? "请输入数字" : "格式不正确";
  if (v.valueMissing) return (choose ? "请选择" : "请输入") + (name || "此项");
  if (v.typeMismatch) return type === "email" ? "请输入有效的邮箱地址" : type === "url" ? "请输入有效的网址" : "格式不正确";
  if (v.tooShort) return `${name || "长度"}至少 ${(control as HTMLInputElement).minLength} 个字符`;
  if (v.tooLong) return `${name || "长度"}最多 ${(control as HTMLInputElement).maxLength} 个字符`;
  if (v.patternMismatch) return (control as HTMLInputElement).title || (name ? name + "格式不正确" : "格式不正确");
  if (v.rangeUnderflow) return `不能小于 ${(control as HTMLInputElement).min}`;
  if (v.rangeOverflow) return `不能大于 ${(control as HTMLInputElement).max}`;
  if (v.stepMismatch) return "数值不符合步长要求";
  return "输入无效";
}

function Field({
  label,
  htmlFor,
  name,
  hint,
  error,
  customError,
  required,
  className,
  children,
  onBlur,
  onChange,
  ...props
}: Omit<ComponentProps<"div">, "children"> & {
  label?: ReactNode;
  // 控件 id；不传时取唯一子元素的 id 或自动生成
  htmlFor?: string;
  // 在 <Form errors> 中的字段名
  name?: string;
  hint?: ReactNode;
  // 外部错误（如服务端返回的字段错误）：显示到用户再次改动该字段为止
  error?: ReactNode;
  // 跨字段 / 自定义规则：非空即视为不通过，提交时与原生约束一起拦截
  customError?: string;
  // 标签前的红色 *；不传时取控件的 required
  required?: boolean;
  children?: ReactNode;
}) {
  const form = useFormContext();
  const box = useRef<HTMLDivElement>(null);
  const autoId = useId();
  const only = Children.count(children) === 1 && isValidElement(Children.only(children)) ? (Children.only(children) as ReactElement<Record<string, unknown>>) : null;
  const childId = only?.props.id as string | undefined;
  const controlId = htmlFor ?? childId ?? autoId;
  const [labelFor, setLabelFor] = useState(controlId);
  const hintId = controlId + "-hint";
  const errorId = controlId + "-error";
  const labelId = controlId + "-label";
  const [active, setActive] = useState(false); // 已校验过（失焦或提交）：之后每次渲染都重新校验
  const [dirty, setDirty] = useState(false);
  const [message, setMessage] = useState("");
  const [dismissed, setDismissed] = useState(false);
  const [needed, setNeeded] = useState(false);
  const [, rerender] = useState(0);
  const external = error || (name ? form?.errors[name] : null) || null;
  useEffect(() => setDismissed(false), [external]);

  // 承载取值与约束的元素：原生输入；下拉框 / 开关是组件内的隐藏输入
  const validityTarget = (): Control | null => {
    const el = box.current;
    if (!el) return null;
    const byId = el.querySelector("#" + CSS.escape(labelFor));
    if (byId?.matches("input, textarea, select")) return byId as Control;
    return el.querySelector<Control>("input:not([type=hidden]), textarea, select");
  };
  // 接收焦点、描红与读屏说明的元素：原生输入，或下拉框的触发按钮
  const focusTarget = (): HTMLElement | null => {
    const el = box.current;
    if (!el) return null;
    const byId = el.querySelector<HTMLElement>("#" + CSS.escape(labelFor));
    if (byId && byId.getAttribute("aria-hidden") !== "true") return byId;
    const v = validityTarget();
    if (v && v.getAttribute("aria-hidden") !== "true") return v;
    return el.querySelector<HTMLElement>("button, [tabindex]:not([tabindex='-1'])");
  };
  const check = (el: Control) => describe(el, fieldName(label, focusTarget()));

  const shown = external && !dismissed ? external : active ? message : "";
  const invalid = !!shown;

  // 每次渲染后：同步自定义规则与必填标记；已校验过的字段随值变化实时更新错误；控件与标签、说明、错误关联
  useLayoutEffect(() => {
    const v = validityTarget();
    if (v) {
      if (customError !== undefined) v.setCustomValidity(customError);
      setNeeded(v.required);
      if (active) setMessage(check(v));
    }
    const f = focusTarget();
    if (!f) return;
    if (!f.id) f.id = controlId;
    setLabelFor(f.id);
    const ours = new Set([hintId, errorId]);
    const others = (f.getAttribute("aria-describedby") ?? "").split(" ").filter((x) => x && !ours.has(x));
    const describedBy = [...others, invalid ? errorId : hint ? hintId : ""].filter(Boolean).join(" ");
    if (describedBy) f.setAttribute("aria-describedby", describedBy);
    else f.removeAttribute("aria-describedby");
    // 下拉框等以 role 声明的控件不会从 <label for> 取名称，显式关联标签；已有 aria-label / aria-labelledby 的不覆盖
    if (label && !f.matches("input, textarea, select") && !f.hasAttribute("aria-label") && (!f.hasAttribute("aria-labelledby") || f.getAttribute("aria-labelledby") === labelId))
      f.setAttribute("aria-labelledby", labelId);
    if (invalid) f.setAttribute("aria-invalid", "true");
    else if (f.getAttribute("aria-invalid") === "true") f.removeAttribute("aria-invalid");
  });

  // 提交 / reportValidity 时浏览器对无效控件派发 invalid：拦下原生气泡，改为显示在字段下方，并聚焦表单里第一个错误
  useEffect(() => {
    const v = validityTarget();
    if (!v) return;
    const onInvalid = (e: Event) => {
      e.preventDefault();
      setActive(true);
      setMessage(check(v));
      const first = v.form?.querySelector(":is(input, textarea, select):invalid") ?? v;
      if (box.current?.contains(first)) {
        const f = focusTarget();
        if (f && document.activeElement !== f) f.focus();
      }
    };
    v.addEventListener("invalid", onInvalid);
    return () => v.removeEventListener("invalid", onInvalid);
  });

  const content = only && !childId && !htmlFor ? cloneElement(only, { id: controlId }) : children;
  const star = required ?? (!!only?.props.required || needed);

  const hintNode = hint ? (
    // block：与错误行同高（行内元素会被父级行高撑高，说明换成错误时布局会跳）
    <small id={hintId} className="block text-[11.5px] leading-[18px] text-[var(--mg-ink-3)]">
      {hint}
    </small>
  ) : null;
  const errorNode = invalid ? (
    <span id={errorId} role="alert" className="block text-xs leading-[18px] text-[var(--mg-bad-ink)] animate-[mg-field-error_160ms_ease-out]">
      {shown}
    </span>
  ) : null;

  return (
    <div
      ref={box}
      data-slot="field"
      data-invalid={invalid || undefined}
      className={cn("grid min-w-0 content-start gap-1.5", className)}
      onBlur={(e) => {
        onBlur?.(e);
        if (!dirty || e.currentTarget.contains(e.relatedTarget as Node | null)) return;
        const v = validityTarget();
        if (!v) return;
        setActive(true);
        setMessage(check(v));
      }}
      onChange={(e) => {
        onChange?.(e);
        setDirty(true);
        if (external) setDismissed(true);
        rerender((n) => n + 1);
      }}
      {...props}
    >
      {label && (
        <Label id={labelId} htmlFor={labelFor}>
          {/* 必填标记只给视觉；读屏通过控件的 required 得知必填，不把 * 读进名称 */}
          {star && (
            <span aria-hidden="true" className="-mr-1 text-[var(--mg-bad-ink)]">
              *
            </span>
          )}
          {label}
        </Label>
      )}
      {content}
      {form ? (
        // 表单内：说明 / 错误共用一行并始终占位（错误优先），错误出现不推动下面的内容
        <div data-slot="field-message" className="-mt-1 min-h-[18px]">
          {errorNode ?? hintNode}
        </div>
      ) : (
        errorNode ?? hintNode
      )}
    </div>
  );
}

export { Field };
