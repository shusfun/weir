import { Button, Badge } from "@migate/ui";
import { AlertCircle, Inbox, LoaderCircle } from "lucide-react";
import { labels } from "../lib/models";
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
  return (
    <div
      className={"page-state" + (error ? " error" : "")}
      role={error ? "alert" : "status"}
    >
      {loading ? (
        <LoaderCircle className="spin" size={24} />
      ) : error ? (
        <AlertCircle size={28} />
      ) : (
        <Inbox size={30} />
      )}
      <strong>
        {loading
          ? "正在加载数据"
          : error
            ? "暂时无法加载"
            : (empty ?? "暂无记录")}
      </strong>
      <p>
        {error
          ? (detail || "请检查连接后重试。现有数据不会被更改。")
          : loading
            ? "请稍候，正在获取最新状态。"
            : (detail ?? "创建记录后会显示在这里。")}
      </p>
      {!!error && onRetry && (
        <Button variant="outline" onClick={onRetry}>
          重新加载
        </Button>
      )}
    </div>
  );
}
export function Status({ value }: { value: string }) {
  return (
    <Badge className={"status-badge status-" + value} variant="outline">
      <span className="status-dot" />
      {labels[value] ?? value}
    </Badge>
  );
}
