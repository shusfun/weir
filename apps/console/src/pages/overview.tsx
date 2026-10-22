// 仪表盘：读数窗 + 设备机架 + 观测与待办。缺失数据显示“未知”，不补零。
import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { SectionHeader,
  Card, CardHeader, CardTitle, CardDescription, CardAction, CardContent, Button, PageHeader, Readout, StatusLamp, toneOf,
  ModuleCard, useLiveSwitch,
  Select, SelectTrigger, SelectValue, SelectContent, SelectItem,
} from "@migate/ui";
import { Activity, ArrowUpRight } from "lucide-react";
import { TrafficHistory } from "../components/traffic-history";
import { client } from "../lib/client";
import { api } from "../api";
import { date } from "../lib/models";
import { Status, State } from "../components/feedback";

interface Summary {
  users?: number;
  devices?: number;
  tasks?: number;
  online_devices?: number;
  offline_devices?: number;
  unknown_meters?: number;
  traffic_status?: string;
  version?: string;
  observed_at?: string;
}

export function OverviewPage() {
  const summary = useQuery({ queryKey: ["overview"], queryFn: () => api("/admin/overview") as Promise<Summary>, refetchInterval: 30000 });
  // 模块开关：关闭后停止该模块轮询
  const [devicesLive, setDevicesLive] = useLiveSwitch("overview-devices");
  const [tasksLive, setTasksLive] = useLiveSwitch("overview-tasks");
  const devices = useQuery({ queryKey: ["devices"], queryFn: client.devices, refetchInterval: devicesLive ? 15000 : false });
  const tasks = useQuery({ queryKey: ["tasks"], queryFn: () => client.tasks(), refetchInterval: tasksLive ? 10000 : false });
  const [trafficDevice, setTrafficDevice] = useState("");
  if (summary.isPending) return <State loading />;
  if (summary.error) return <State error={summary.error} onRetry={() => summary.refetch()} />;
  const data = summary.data;
  const failures = tasks.data?.items.filter((t) => t.status === "failed") ?? [];
  const observed =
    devices.data?.items.find((d) => String(d.id) === trafficDevice) ??
    devices.data?.items.find((d) => ["approved", "online", "offline"].includes(d.status));
  const failedCount = tasks.isPending ? undefined : tasks.error ? null : failures.length;
  const deviceName = (id: number) => devices.data?.items.find((d) => d.id === id)?.name ?? "Gate #" + id;

  return (
    <div className="stack">
      <PageHeader
        title="一处掌握你的网络"
        description="设备状态、授权与执行结果；以 Gate 实际回执为准。"
        actions={<span className="version-tag"><Activity size={13} />{data.version ?? "版本未知"}</span>}
      />
      <div className="instrument-row">
        <Link className="readout-link" to="/devices" aria-label="查看在线 Gate">
          <Readout label={<span className="readout-head">在线 Gate<StatusLamp tone={data.online_devices ? "ok" : "off"} /></span>} value={data.online_devices} framed caption={"共 " + (data.devices ?? "未知") + " 台设备"} />
        </Link>
        <Link className="readout-link" to="/devices" aria-label="查看离线 Gate">
          <Readout label={<span className="readout-head">离线 Gate<StatusLamp tone={data.offline_devices === undefined ? "unknown" : data.offline_devices ? "warn" : "ok"} /></span>} value={data.offline_devices} framed caption="检查连接与心跳" />
        </Link>
        <Link className="readout-link" to="/users" aria-label="查看用户">
          <Readout label="用户总数" value={data.users} framed caption="账户、额度与有效期" />
        </Link>
        <Link className="readout-link" to="/ops/tasks" aria-label="查看执行任务">
          <Readout label="执行任务" value={data.tasks} framed caption="以设备回执为准" />
        </Link>
      </div>

      {observed && (
        <section className="stack" aria-label="Gate 流量观测">
          <SectionHeader flush title="按设备观测" actions={<Select value={String(observed.id)} onValueChange={(v) => setTrafficDevice(String(v))}>
              <SelectTrigger aria-label="选择流量观测 Gate"><SelectValue>{observed.name}</SelectValue></SelectTrigger>
              <SelectContent>
                {devices.data?.items.map((d) => <SelectItem key={d.id} value={String(d.id)}>{d.name}</SelectItem>)}
              </SelectContent>
            </Select>} />
          <TrafficHistory deviceId={observed.id} />
        </section>
      )}

      <div className="overview-columns">
        <ModuleCard title="Gate 网络" description="最近设备连接状态" live={devicesLive} onLiveChange={setDevicesLive}
          actions={<Button variant="ghost" size="sm" render={<Link to="/devices" />}>查看全部 <ArrowUpRight /></Button>}>
            {devices.isPending ? <State loading /> : devices.error ? <State error={devices.error} onRetry={() => devices.refetch()} /> : devices.data.items.length ? (
              devices.data.items.slice(0, 6).map((d) => (
                <div className="rack-mini" key={d.id}>
                  <StatusLamp tone={toneOf(d.status)} />
                  <div>
                    <Link to={"/devices/" + d.id}>{d.name || "未命名 Gate"}</Link>
                    <small>最近联系 {date(d.last_seen)}</small>
                  </div>
                  <Status value={d.status} />
                </div>
              ))
            ) : <State empty="还没有 Gate 设备" detail="在 Gate 网络页生成配对码后，VPS 上的 Agent 会主动连接中心。" />}
        </ModuleCard>
        <Card>
          <CardHeader><CardTitle>观测与待办</CardTitle></CardHeader>
          <CardContent>
            <div className="observation-item">
              <StatusLamp tone={data.traffic_status === "fresh" ? "ok" : data.traffic_status ? "warn" : "unknown"} />
              <div><strong>流量观测</strong><p>{data.traffic_status === "fresh" ? "设备计量已更新" : "部分计量未知，请检查设备采集状态"}</p></div>
            </div>
            <div className="observation-item">
              <StatusLamp tone={data.unknown_meters === undefined ? "unknown" : data.unknown_meters ? "warn" : "ok"} />
              <div><strong>计量未知 Gate：{data.unknown_meters ?? "未知"}</strong><p>缺失或过期数据不会显示为零流量。</p></div>
            </div>
            <div className="observation-item">
              <StatusLamp tone={failedCount === undefined || failedCount === null ? "unknown" : failedCount ? "bad" : "ok"} />
              <div><strong>最近失败任务：{failedCount === undefined ? "…" : failedCount === null ? "未知" : failedCount}</strong><p>失败任务保留执行原因与关联设备。</p></div>
            </div>
            <Button variant="outline" className="full-width" render={<Link to="/ops/events" />}>查看安全事件 <ArrowUpRight /></Button>
          </CardContent>
        </Card>
      </div>

      <ModuleCard title="最近执行" description="以 Gate 实际回执为准" live={tasksLive} onLiveChange={setTasksLive}
        actions={<Button variant="ghost" size="sm" render={<Link to="/ops/tasks" />}>任务中心 <ArrowUpRight /></Button>}>
          {tasks.isPending ? <State loading /> : tasks.error ? <State error={tasks.error} onRetry={() => tasks.refetch()} /> : !tasks.data.items.length ? <State empty="暂无执行任务" /> : (
            tasks.data.items.slice(0, 5).map((t) => (
              <div className="list-row" key={t.id}>
                <div><strong>{t.operation}</strong><small>任务 #{t.id} · {deviceName(t.device_id)} · {date(t.created_at)}</small></div>
                <Status value={t.status} />
              </div>
            ))
          )}
      </ModuleCard>
      <div className="observation-time">中心观测时间 {date(data.observed_at)}</div>
    </div>
  );
}
