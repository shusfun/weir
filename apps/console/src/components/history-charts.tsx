// 流量与主机历史图表：1 / 6 / 24 小时。数据缺失显示「未知」，不补零，缺口不连线。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Segmented } from "@weir/ui";
import { ChartContainer, ChartTooltip, type ChartConfig } from "@weir/ui/chart";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import { api } from "../api";
import { bytes } from "../lib/models";
import { State } from "./feedback";

interface Point {
  at: string;
  traffic: number | null;
  cpu?: number | null;
  mem_used?: number | null;
  mem_total?: number | null;
  disk_used?: number | null;
  disk_total?: number | null;
}

const ranges = [["1", "1 小时"], ["6", "6 小时"], ["24", "24 小时"]] as const;
const time = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
const pct = (used?: number | null, total?: number | null) => (used != null && total ? (used / total) * 100 : null);

type Series = { key: string; label: string; color: string; format: (v: number) => string };

function Chart({ data, series, unit }: { data: Record<string, unknown>[]; series: Series[]; unit?: "%" }) {
  const config = Object.fromEntries(series.map((s) => [s.key, { label: s.label, color: s.color }])) as ChartConfig;
  return (
    <ChartContainer config={config} className="h-40 w-full">
      <LineChart data={data} margin={{ left: 4, right: 8, top: 8 }}>
        <CartesianGrid vertical={false} strokeDasharray="3 3" />
        <XAxis dataKey="at" tickFormatter={time} minTickGap={36} tickLine={false} axisLine={false} fontSize={11} />
        <YAxis width={unit ? 34 : 56} tickFormatter={(v: number) => (unit ? v.toFixed(0) + "%" : series[0].format(v))} domain={unit ? [0, 100] : [0, "auto"]} tickLine={false} axisLine={false} fontSize={11} />
        <ChartTooltip
          content={({ active, payload, label }) =>
            active && payload ? (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line)] bg-[var(--weir-panel)] px-2.5 py-1.5 text-xs shadow">
                <div className="mb-1 text-[var(--weir-ink-3)]">{time(String(label))}</div>
                {series.map((s) => {
                  const v = (payload[0]?.payload as Record<string, unknown>)[s.key];
                  return <div key={s.key}>{s.label}：{typeof v === "number" ? s.format(v) : "未知"}</div>;
                })}
              </div>
            ) : null
          }
        />
        {series.map((s) => <Line key={s.key} dataKey={s.key} stroke={s.color} strokeWidth={1.75} dot={false} connectNulls={false} isAnimationActive={false} />)}
      </LineChart>
    </ChartContainer>
  );
}

// HistoryCharts：VPS 详情显示流量、CPU、内存、磁盘；用户详情只显示流量。
export function HistoryCharts({ path, host }: { path: string; host?: boolean }) {
  const [hours, setHours] = useState<(typeof ranges)[number][0]>("1");
  const q = useQuery({ queryKey: ["history", path, hours], queryFn: async () => (await api(`${path}?hours=${hours}`)).points as Point[], refetchInterval: 60000 });
  const data = (q.data ?? []).map((p) => ({ at: p.at, traffic: p.traffic, cpu: p.cpu ?? null, mem: pct(p.mem_used, p.mem_total), disk: pct(p.disk_used, p.disk_total) }));
  const known = data.some((p) => p.traffic !== null || p.cpu !== null);
  return (
    <div className="stack">
      <Segmented aria-label="时间范围" value={hours} onValueChange={setHours} options={ranges} />
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : !known ? (
        <small className="text-[var(--weir-ink-3)]">这段时间没有数据（未知）：VPS 离线或尚未上报。</small>
      ) : (
        <>
          <Chart data={data} series={[{ key: "traffic", label: "流量", color: "var(--weir-accent)", format: (v) => bytes(v) }]} />
          {host && <Chart unit="%" data={data} series={[
            { key: "cpu", label: "CPU", color: "var(--weir-info)", format: (v) => v.toFixed(1) + "%" },
            { key: "mem", label: "内存", color: "var(--weir-warn)", format: (v) => v.toFixed(1) + "%" },
            { key: "disk", label: "磁盘", color: "var(--weir-ok)", format: (v) => v.toFixed(1) + "%" },
          ]} />}
        </>
      )}
    </div>
  );
}
