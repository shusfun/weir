// 模块卡片：右上角拟物开关控制该模块的实时刷新。关闭后停止轮询、内容去色变暗，并标注暂停时刻，
// 避免把旧数据当成实时数据。开关状态按浏览器记住。
import { useEffect, useState, type ReactNode } from "react";
import { Card, CardHeader, CardTitle, CardDescription, CardAction, CardContent } from "./card";
import { Switch } from "./switch";
import { StatusLamp } from "./lamp";

export function useLiveSwitch(key: string, initial = true) {
  const storageKey = "migate-live:" + key;
  const [live, setLive] = useState(() => {
    try {
      const v = localStorage.getItem(storageKey);
      return v === null ? initial : v === "1";
    } catch {
      return initial;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, live ? "1" : "0");
    } catch {
      // 仅本次生效
    }
  }, [live]);
  return [live, setLive] as const;
}

function ModuleCard({
  title,
  description,
  live,
  onLiveChange,
  actions,
  className,
  children,
}: {
  title: string;
  description?: ReactNode;
  live: boolean;
  onLiveChange: (live: boolean) => void;
  actions?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  const [pausedAt, setPausedAt] = useState<string | null>(null);
  useEffect(() => {
    setPausedAt(live ? null : new Date().toLocaleTimeString("zh-CN", { hour12: false }));
  }, [live]);
  return (
    <Card className={className}>
      <CardHeader>
        <div className="min-w-0">
          <CardTitle>{title}</CardTitle>
          {description && <CardDescription>{description}</CardDescription>}
        </div>
        <CardAction>
          {actions}
          <div className="mg-module-toggle">
            <StatusLamp tone={live ? "active" : "off"} />
            <span>{live ? "实时" : "已暂停"}</span>
            <Switch size="sm" checked={live} onCheckedChange={onLiveChange} aria-label={title + "实时刷新"} />
          </div>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!live && (
          <p className="mg-module-paused" role="status">
            已暂停实时刷新，显示的是 {pausedAt ?? "暂停前"} 的数据。
          </p>
        )}
        <div className="mg-module-body" data-live={live}>
          {children}
        </div>
      </CardContent>
    </Card>
  );
}

export { ModuleCard };
