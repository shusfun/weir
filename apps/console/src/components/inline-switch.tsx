// 列表行内开关：拨动后立即提交，等服务端确认并刷新列表后才落定；失败回弹并提示原因。
import { useState } from "react";
import { Switch, useToast } from "@migate/ui";

export function InlineSwitch({
  checked,
  onChange,
  label,
  on = "已启用",
  off = "已停用",
}: {
  checked: boolean;
  onChange: (value: boolean) => Promise<void>;
  label: string;
  on?: string;
  off?: string;
}) {
  const toast = useToast();
  const [pending, setPending] = useState<boolean | null>(null);
  const value = pending ?? checked;
  return (
    <span className="inline-switch" data-pending={pending !== null}>
      <Switch
        size="sm"
        checked={value}
        disabled={pending !== null}
        aria-label={label}
        onCheckedChange={async (next) => {
          setPending(next);
          try {
            await onChange(next);
          } catch (e) {
            toast("未能切换", { description: e instanceof Error ? e.message : undefined, tone: "bad" });
          } finally {
            setPending(null);
          }
        }}
      />
      <span>{value ? on : off}</span>
    </span>
  );
}
