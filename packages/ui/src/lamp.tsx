// Weir 状态灯：亮 / 警告 / 故障 / 信息 / 熄灭 / 未知。未知为空心虚线，避免把缺失数据显示成正常。
import { ComponentProps } from "react";
import { cn } from "cn";

// active：选中 / 开启指示灯（#2f4bd0）
export type LampTone = "ok" | "warn" | "bad" | "info" | "active" | "off" | "unknown";

const tones: Record<string, LampTone> = {
  approved: "ok", online: "ok", active: "ok", enabled: "ok", applied: "ok", success: "ok",
  completed: "ok", sent: "ok", resolved: "ok", fresh: "ok", available: "ok",
  pending: "warn", queued: "warn", recovery: "warn", unapplied: "warn", observed: "warn", partial: "warn", stale: "warn",
  running: "info",
  failed: "bad", revoked: "bad", expired: "bad", critical: "bad", error: "bad",
  offline: "off", disabled: "off", inactive: "off", empty: "off",
};

export function toneOf(status: string | undefined | null): LampTone {
  return (status && tones[status]) || "unknown";
}

function StatusLamp({
  tone,
  size,
  label,
  className,
  ...props
}: ComponentProps<"span"> & { tone: LampTone; size?: "sm" | "lg"; label?: string }) {
  return (
    <span
      data-slot="lamp"
      data-tone={tone}
      data-size={size}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className={cn("mg-lamp", className)}
      {...props}
    />
  );
}

export { StatusLamp };
