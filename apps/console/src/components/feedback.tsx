import { Notice, Button, Badge, PageState, StatusLamp, toneOf } from "@migate/ui";
import { RefreshCw } from "lucide-react";
import { labels } from "../lib/models";
import { isConflict } from "../api";

// 页面状态：加载 / 空 / 失败沿用旧接口；失败时优先显示服务端给出的中文原因。
export function State({
  loading,
  error,
  empty,
  onRetry,
  detail,
}: {
  loading?: boolean;
  error?: unknown;
  empty?: string;
  onRetry?: () => void;
  detail?: string;
}) {
  if (loading)
    return <PageState kind="loading" title="正在加载数据" detail="请稍候，正在获取最新状态。" />;
  if (error)
    return (
      <PageState
        kind="error"
        title="暂时无法加载"
        detail={detail || (error instanceof Error && error.message) || "请检查连接后重试。现有数据不会被更改。"}
        action={
          onRetry && (
            <Button variant="outline" onClick={onRetry}>
              <RefreshCw />
              重新加载
            </Button>
          )
        }
      />
    );
  // 空状态不带默认说明：“筛选无结果”“等待上报”“设备已移除”等场景含义各不相同，由调用处按需传入
  return <PageState kind="empty" title={empty ?? "暂无记录"} detail={detail} />;
}

// 灯色 → 文字色：熄灭 / 未知 / 选中都按中性文字显示，避免把缺失状态染成正常色
const badgeTone = { ok: "ok", warn: "warn", bad: "bad", info: "info", active: "info", off: "neutral", unknown: "neutral" } as const;

export function Status({ value, label }: { value: string; label?: string }) {
  const tone = toneOf(value);
  return (
    <Badge variant="status" tone={badgeTone[tone]}>
      <StatusLamp tone={tone} />
      {label ?? labels[value] ?? value}
    </Badge>
  );
}

// 版本冲突：统一提示并提供重新读取，避免覆盖其他管理员的修改。
export function ConflictNotice({ error, onReload }: { error: unknown; onReload: () => void }) {
  if (!isConflict(error)) return null;
  return (
    <Notice tone="bad">
      <div className="actions" style={{ justifyContent: "space-between" }}>
        <span>{(error as Error).message}</span>
        <Button size="sm" variant="outline" onClick={onReload}>
          <RefreshCw />
          重新读取最新版本
        </Button>
      </div>
    </Notice>
  );
}
