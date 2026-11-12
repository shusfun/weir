import { Card, CardContent, CardHeader, Readout, SectionHeader } from "@weir/ui";
import { State } from "../../../components/feedback";
import { date } from "../../../lib/models";

export type WorkerStatisticsData = {
  from: string;
  until: string;
  task_states: Record<string, number>;
  items: {
    gate_id: number; kind: string; attempts: number; active: number;
    succeeded: number; failed: number; cancelled: number; unknown: number;
    finished: number; measured_results: number; result_bytes: number | null;
    mean_duration_ms: number | null; failure_rate: number | null;
  }[];
};

export function WorkerStatistics({ data, loading, error, onRetry, labels }: {
  data?: WorkerStatisticsData; loading: boolean; error: Error | null;
  onRetry: () => void; labels: Record<string, string>;
}) {
  return <section className="stack" data-tour="worker-statistics">
    <SectionHeader title="任务统计" description="按 VPS 和任务类型汇总数据库中的执行尝试，包含重试。失败率以已知成功和失败为分母，取消及未知另列。" />
    {loading || error ? <State loading={loading} error={error} onRetry={onRetry} /> : !data ? <State empty="任务统计未知" /> : <>
      <small>执行开始时间：{date(data.from)} — {date(data.until)}。结果正文大小为解压后、中心加密前的测量值；缺测记录不计为 0。</small>
      {data.items.length === 0 ? <State empty="此时间段尚无执行尝试" /> : data.items.map((item) => <Card key={`${item.gate_id}-${item.kind}`}>
        <CardHeader title={labels[item.kind] ?? item.kind} description={<>VPS {item.gate_id} · {item.attempts} 次尝试 · 执行中 {item.active} · 成功 {item.succeeded} · 失败 {item.failed} · 取消 {item.cancelled} · 未知 {item.unknown}</>} />
        <CardContent>
          <Readout label="平均耗时" value={item.mean_duration_ms == null ? null : Math.round(item.mean_duration_ms)} unit="ms" caption={`${item.finished} 次已结束尝试`} />
          <Readout label="失败率" value={item.failure_rate == null ? null : (item.failure_rate * 100).toFixed(1)} unit="%" />
          <Readout label="已测结果正文" value={item.result_bytes} unit="B" caption={`已测量 ${item.measured_results} / ${item.finished} 次已结束尝试；未测部分未知`} />
        </CardContent>
      </Card>)}
    </>}
  </section>;
}
