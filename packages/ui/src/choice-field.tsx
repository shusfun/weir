// 开关 / 勾选项：整行可点。替代各页手写的 <Label className="actions"><Switch aria-label=…/>…</Label>。
// 文字即控件的可访问名称（不含说明），不必再重复 aria-label；description 显示在文字下方并关联为控件说明。
// placement="end"：设置行样式，文字在左、控件靠右（系统设置、安装向导里的策略开关）。
import { useId, type ReactNode } from "react";
import { cn } from "cn";
import { Checkbox } from "./checkbox";
import { Switch } from "./switch";

type Common = {
  label: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
  className?: string;
  id?: string;
  placement?: "start" | "end";
};

function ChoiceRow({ id, label, description, disabled, className, placement = "start", control }: Common & { id: string; control: ReactNode }) {
  return (
    <label
      htmlFor={id}
      data-slot="choice-field"
      data-disabled={disabled || undefined}
      className={cn(
        // 整行可点；placement=end 是设置行（iOS 设置里的开关行），行高不小于 44
        "flex min-w-0 cursor-pointer items-start gap-3 text-body text-[var(--weir-ink)] select-none touch:min-h-11 touch:items-center data-[disabled]:cursor-not-allowed data-[disabled]:opacity-55",
        placement === "end" && "min-h-11 flex-row-reverse items-center justify-between gap-6",
        className,
      )}
    >
      <span className="flex h-[var(--weir-leading-body)] shrink-0 items-center">{control}</span>
      <span className={cn("grid min-w-0", placement === "end" && "gap-0.5")}>
        <span id={id + "-label"} className={cn(placement === "end" && "font-[550]")}>{label}</span>
        {description && (
          <small id={id + "-description"} className="text-meta text-[var(--weir-ink-3)]">
            {description}
          </small>
        )}
      </span>
    </label>
  );
}

function SwitchField({
  checked,
  onCheckedChange,
  id,
  size,
  ...rest
}: Common & { checked: boolean; onCheckedChange: (checked: boolean) => void; size?: "sm" | "default" }) {
  const auto = useId();
  const controlId = id ?? auto;
  return (
    <ChoiceRow
      {...rest}
      id={controlId}
      control={
        <Switch
          id={controlId}
          size={size}
          checked={checked}
          disabled={rest.disabled}
          aria-labelledby={controlId + "-label"}
          aria-describedby={rest.description ? controlId + "-description" : undefined}
          onCheckedChange={(v) => onCheckedChange(v)}
        />
      }
    />
  );
}

function CheckboxField({
  checked,
  onCheckedChange,
  id,
  ...rest
}: Common & { checked: boolean; onCheckedChange: (checked: boolean) => void }) {
  const auto = useId();
  const controlId = id ?? auto;
  return (
    <ChoiceRow
      {...rest}
      id={controlId}
      control={
        <Checkbox
          id={controlId}
          checked={checked}
          disabled={rest.disabled}
          aria-labelledby={controlId + "-label"}
          aria-describedby={rest.description ? controlId + "-description" : undefined}
          onCheckedChange={(v) => onCheckedChange(v)}
        />
      }
    />
  );
}

export { SwitchField, CheckboxField };
