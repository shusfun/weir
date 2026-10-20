import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Card,
  CardContent,
  Button,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import { TrafficHistory } from "../components/traffic-history";
import {
  Activity,
  ArrowUpRight,
  Server,
  Users,
  ShieldCheck,
  AlertTriangle,
  CheckCircle2,
  Clock3,
} from "lucide-react";
import { client } from "../lib/client";
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
export function OverviewPage({ data }: { data: Summary }) {
  const nav = useNavigate();
  const [trafficDevice, setTrafficDevice] = useState("");
  const devices = useQuery({ queryKey: ["devices"], queryFn: client.devices });
  const tasks = useQuery({
    queryKey: ["tasks"],
    queryFn: () => client.tasks(),
  });
  const failures = tasks.data?.items.filter((t) => t.status === "failed") ?? [];
  const observedDevice =
    devices.data?.items.find((d) => String(d.id) === trafficDevice) ??
    devices.data?.items.find(
      (d) =>
        d.status === "approved" ||
        d.status === "online" ||
        d.status === "offline",
    );
  return (
    <div className="stack">
      <div className="overview-welcome">
        <div>
          <div className="eyebrow">WORKSPACE OVERVIEW</div>
          <h2>一处掌握你的网络。</h2>
          <p>设备状态、授权与执行结果，随时可查。</p>
        </div>
        <div className="version-tag">
          <Activity size={15} />
          {data.version ?? "版本未知"}
        </div>
      </div>
      <div className="overview-metrics">
        {[
          {
            title: "用户总数",
            value: data.users,
            icon: Users,
            path: "users",
            note: "账户与额度管理",
            color: "purple",
          },
          {
            title: "在线 Gate",
            value: data.online_devices,
            icon: Server,
            path: "devices",
            note: "共 " + (data.devices ?? "未知") + " 台设备",
            color: "teal",
          },
          {
            title: "执行任务",
            value: data.tasks,
            icon: CheckCircle2,
            path: "tasks",
            note: "查看实际执行回执",
            color: "blue",
          },
          {
            title: "离线 Gate",
            value: data.offline_devices,
            icon: AlertTriangle,
            path: "devices",
            note: "检查连接与心跳",
            color: "amber",
          },
        ].map(({ title, value, icon: Icon, path, note, color }) => (
          <Card key={title} className="overview-stat">
            <CardContent>
              <div className="stat-top">
                <span>{title}</span>
                <span className={"stat-icon " + color}>
                  <Icon size={17} />
                </span>
              </div>
              <div className="stat-number">{value ?? "—"}</div>
              <div className="stat-bottom">
                <span>{note}</span>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={"查看" + title}
                  onClick={() => nav("/" + path)}
                >
                  <ArrowUpRight size={15} />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      {observedDevice && (
        <section className="stack" aria-label="Gate 流量观测">
          <div className="section-heading">
            <h2>按设备观测</h2>
            <Select
              value={String(observedDevice.id)}
              onValueChange={(v) => setTrafficDevice(String(v))}
            >
              <SelectTrigger aria-label="选择流量观测 Gate">
                <SelectValue>{observedDevice.name}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {devices.data?.items.map((d) => (
                  <SelectItem key={d.id} value={String(d.id)}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <TrafficHistory deviceId={observedDevice.id} />
        </section>
      )}
      <div className="overview-columns">
        <Card>
          <CardContent>
            <div className="section-heading">
              <div>
                <h2>Gate 网络</h2>
                <p>最近设备连接状态</p>
              </div>
              <Button variant="ghost" size="sm" onClick={() => nav("/devices")}>
                查看全部 <ArrowUpRight size={14} />
              </Button>
            </div>
            {devices.isPending ? (
              <State loading />
            ) : devices.error ? (
              <State error={devices.error} onRetry={() => devices.refetch()} />
            ) : devices.data.items.length ? (
              devices.data.items.slice(0, 5).map((d) => (
                <div className="network-row" key={d.id}>
                  <span className="network-icon">
                    <Server size={18} />
                  </span>
                  <div>
                    <Button
                      variant="link"
                      onClick={() => nav("/devices/" + d.id)}
                    >
                      {d.name}
                    </Button>
                    <small>最近联系 {date(d.last_seen)}</small>
                  </div>
                  <Status value={d.status} />
                </div>
              ))
            ) : (
              <State empty="还没有 Gate 设备" />
            )}
          </CardContent>
        </Card>
        <Card>
          <CardContent>
            <div className="section-heading">
              <h2>观测与待办</h2>
              <ShieldCheck size={20} />
            </div>
            <div className="observation-item">
              <span
                className={
                  "observation-symbol " +
                  (data.traffic_status === "fresh" ? "teal" : "amber")
                }
              >
                <Activity size={18} />
              </span>
              <div>
                <strong>流量观测</strong>
                <p>
                  {data.traffic_status === "fresh"
                    ? "设备计量已更新"
                    : "部分计量未知，请检查设备采集状态"}
                </p>
              </div>
            </div>
            <div className="observation-item">
              <span className="observation-symbol purple">
                <Clock3 size={18} />
              </span>
              <div>
                <strong>计量未知 Gate：{data.unknown_meters ?? "未知"}</strong>
                <p>缺失或过期数据不会显示为零流量。</p>
              </div>
            </div>
            <div className="observation-item">
              <span className="observation-symbol amber">
                <AlertTriangle size={18} />
              </span>
              <div>
                <strong>
                  最近失败任务：
                  {tasks.isPending
                    ? "…"
                    : tasks.error
                      ? "未知"
                      : failures.length}
                </strong>
                <p>失败任务保留执行原因与关联设备。</p>
              </div>
            </div>
            <Button
              variant="outline"
              className="full-width"
              onClick={() => nav("/events")}
            >
              查看安全事件 <ArrowUpRight size={14} />
            </Button>
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardContent>
          <div className="section-heading">
            <div>
              <h2>最近执行</h2>
              <p>以 Gate 实际回执为准</p>
            </div>
            <Button variant="ghost" size="sm" onClick={() => nav("/tasks")}>
              所有任务 <ArrowUpRight size={14} />
            </Button>
          </div>
          {tasks.isPending ? (
            <State loading />
          ) : tasks.error ? (
            <State error={tasks.error} onRetry={() => tasks.refetch()} />
          ) : (
            tasks.data.items.slice(0, 5).map((t) => (
              <div className="list-row" key={t.id}>
                <div>
                  <strong>{t.operation}</strong>
                  <small>
                    任务 #{t.id} ·{" "}
                    {devices.data?.items.find((d) => d.id === t.device_id)
                      ?.name ?? "Gate #" + t.device_id}{" "}
                    · {date(t.created_at)}
                  </small>
                </div>
                <Status value={t.status} />
              </div>
            ))
          )}
        </CardContent>
      </Card>
      <div className="observation-time">
        中心观测时间 {date(data.observed_at)} · 代理流量直达 Gate
      </div>
    </div>
  );
}
