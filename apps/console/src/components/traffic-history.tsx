import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@migate/ui/chart";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import { SectionHeader,
  Button,
  Card,
  CardContent,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@migate/ui";
import { RefreshCw } from "lucide-react";
import { api } from "../api";
import { State } from "./feedback";
import { bytes, date } from "../lib/models";

type Sample = {
  at: string;
  total: string;
  quality: string;
  reason?: string;
  delta_bytes: string | null;
  interval_seconds: number;
  report_at: string;
};
type History = {
  items: Sample[];
  observed_at: string;
  source: string;
  retention_days: number;
};
const windows: Record<string, string> = {
  "60": "最近 1 小时",
  "360": "最近 6 小时",
  "1440": "最近 24 小时",
};
const reasons: Record<string, string> = {
  device_offline: "设备离线",
  meter_unknown: "计量未更新",
  sample_gap: "采样间隔不足或中断",
  grant_set_changed: "授权集合变化",
  counter_reset: "计数器重置或无效",
};
export function TrafficHistory({ deviceId }: { deviceId: number }) {
  const [window, setWindow] = useState("60"),
    [details, setDetails] = useState(false);
  const query = useQuery({
    queryKey: ["traffic-history", deviceId, window],
    queryFn: () =>
      api(
        `/admin/devices/${deviceId}/traffic-history?minutes=${window}`,
      ) as Promise<History>,
    refetchInterval: 30000,
  });
  const rows = query.data?.items ?? [];
  const points: { at: number; rate: number | null }[] = [];
  for (const [i, row] of rows.entries()) {
    const at = Date.parse(row.at),
      previous = rows[i - 1];
    // Explicit null separator prevents connecting a curve across missing time;
    // it is never treated as a recorded sample or zero traffic.
    if (previous && at - Date.parse(previous.at) > 120000)
      points.push({ at: Date.parse(previous.at) + 60000, rate: null });
    const delta =
      row.delta_bytes !== null && /^\d+$/.test(row.delta_bytes)
        ? Number(row.delta_bytes)
        : NaN;
    points.push({
      at,
      rate:
        row.quality === "fresh" &&
        Number.isSafeInteger(delta) &&
        row.interval_seconds > 0
          ? delta / row.interval_seconds
          : null,
    });
  }
  const known = points.filter((p) => p.rate !== null),
    latest = rows.at(-1);
  const current =
    latest &&
    Date.now() - Date.parse(latest.at) <= 120000 &&
    latest.quality === "fresh" &&
    latest.delta_bytes !== null;
  return (
    <Card className="traffic-history-card">
      <CardContent className="stack">
        <SectionHeader align="start" title="流量历史" description="Gate 授权计量上报 · 按实际采样间隔折算，不代表实时网卡速率。" actions={<div className="actions">
            <Select value={window} onValueChange={(v) => setWindow(String(v))}>
              <SelectTrigger aria-label="流量时间范围">
                <SelectValue>{windows[window]}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {Object.entries(windows).map(([v, name]) => (
                  <SelectItem key={v} value={v}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              aria-label="刷新流量历史"
              disabled={query.isFetching}
              onClick={() => query.refetch()}
            >
              <RefreshCw size={15} />
            </Button>
          </div>} />
        {query.isPending ? (
          <State loading />
        ) : query.isError ? (
          <State error={query.error} onRetry={() => query.refetch()} />
        ) : !rows.length ? (
          <div className="page-state" role="status">
            <strong>尚无历史采样</strong>
            <p>
              每分钟保存一次；首个样本建立基线，下一次有效上报后才计算区间用量。不会补画安装前的数据。
            </p>
          </div>
        ) : (
          <>
            <div className="metric-grid">
              <div>
                <span className="muted">最近采样区间</span>
                <strong className="block">
                  {current ? bytes(Number(latest.delta_bytes)) : "未知"}
                </strong>
              </div>
              <div>
                <span className="muted">有效区间 / 实际采样</span>
                <strong className="block">
                  {known.length} / {rows.length}
                </strong>
              </div>
              <div>
                <span className="muted">最近采样时间</span>
                <strong className="block">{date(latest?.at)}</strong>
              </div>
            </div>
            {known.length ? (
              <ChartContainer
                className="traffic-history-chart"
                config={{ rate: { label: "平均用量速率", color: "var(--mg-chart-1)" } }}
                style={{ height: 240, width: "100%", minWidth: 0 }}
              >
                <LineChart
                  data={points}
                  accessibilityLayer
                  margin={{ left: 4, right: 12, top: 12, bottom: 4 }}
                >
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis
                    dataKey="at"
                    type="number"
                    domain={["dataMin", "dataMax"]}
                    tickFormatter={(value) =>
                      new Date(value).toLocaleTimeString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                      })
                    }
                    minTickGap={40}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis
                    width={72}
                    tick={{ fontSize: 10 }}
                    tickFormatter={(value) => bytes(value).replace(" ", "\u00a0") + "/s"}
                    tickLine={false}
                    axisLine={false}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        labelFormatter={(value) =>
                          date(new Date(Number(value)).toISOString())
                        }
                        formatter={(value) => bytes(Number(value)) + "/s"}
                      />
                    }
                  />
                  <Line
                    dataKey="rate"
                    type="linear"
                    stroke="var(--color-rate)"
                    strokeWidth={2}
                    connectNulls={false}
                    dot={{ r: 2 }}
                    isAnimationActive={false}
                  />
                </LineChart>
              </ChartContainer>
            ) : (
              <div role="status" className="page-state">
                <strong>目前没有可计算的有效区间</strong>
                <p>
                  首个样本、断连、计量缺失或授权变化均显示未知，不折算为零流量。
                </p>
              </div>
            )}
            <p className="muted">
              有效区间才绘制曲线，缺口不连线。计量包含上下行合计，有采样及在途误差；历史保留{" "}
              {query.data!.retention_days} 天。
            </p>
            <Button variant="ghost" onClick={() => setDetails(!details)}>
              {details ? "收起采样明细" : "查看最近采样明细"}
            </Button>
            {details && (
              <div className="stack" aria-label="实际采样明细">
                {[...rows]
                  .reverse()
                  .slice(0, 20)
                  .map((row) => (
                    <div className="list-row" key={row.at}>
                      <div>
                        <strong>{date(row.at)}</strong>
                        <small>
                          {row.quality === "fresh"
                            ? "计量上报有效"
                            : (reasons[row.reason ?? ""] ?? "未知")}{" "}
                          · 上报 {date(row.report_at)}
                        </small>
                      </div>
                      <span>
                        {row.quality === "fresh" && row.delta_bytes !== null
                          ? bytes(Number(row.delta_bytes))
                          : "未知"}
                      </span>
                    </div>
                  ))}
              </div>
            )}
          </>
        )}
        {query.data && (
          <p className="muted">
            中心读取时间 {date(query.data.observed_at)} ·
            来源：已接受的授权累计计量
          </p>
        )}
      </CardContent>
    </Card>
  );
}
