import { useState, lazy, Suspense } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useMatch, useSearchParams } from "react-router-dom";
import {
  Button,
  Card,
  CardContent,
  Input,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@migate/ui";
import {
  Search,
  Server,
  ArrowLeft,
  ArrowUpRight,
  KeyRound,
  Activity,
  ShieldCheck,
  Clock,
} from "lucide-react";
import { TasksPage } from "./tasks";
const GateTopology = lazy(() =>
  import("../components/gate-topology").then((m) => ({
    default: m.GateTopology,
  })),
);
import {GateHost} from "../components/gate-host";
import { GateCores } from "../components/gate-cores";
import { GateConfig } from "../components/gate-config";
import { OutboundPools } from "../components/outbound-pools";
import { DeviceIdentity } from "../components/device-identity";
import { client } from "../lib/client";
import { date, bytes, type Device, type Resource } from "../lib/models";
import { State, Status } from "../components/feedback";
import { Metric } from "../components/metric";
import { TrafficHistory } from "../components/traffic-history";
const tabs = [
  ["overview", "概览"],
  ["inbounds", "入站"],
  ["grants", "客户端授权"],
  ["outbounds", "出站"],
  ["routing", "路由"],
  ["topology", "拓扑"],
  ["cores", "内核"],
  ["certificates", "证书"],
  ["tasks", "执行任务"],
] as const;
export function DevicesPage() {
  const nav = useNavigate();
  const deviceId = useMatch("/devices/:deviceId")?.params.deviceId;
  const [params, SP] = useSearchParams();
  const tab = params.get("tab") ?? "overview";
  const [search, S] = useState("");
  const [state, F] = useState("all");

  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: client.devices,
    refetchInterval: 15000,
  });
  const d = devices.data?.items.find((d) => d.id === Number(deviceId));
  const resources = useQuery({
    queryKey: ["device-resources", deviceId],
    enabled: !!deviceId,
    refetchInterval: 5000,
    queryFn: async () => {
      const names = [
        "device-inbound",
        "device-outbound",
        "device-routing",
        "device-certificate",
      ];
      const all = await Promise.all(names.map(client.resources));
      return Object.fromEntries(
        names.map((name, i) => [
          name,
          all[i].items.filter((r) => r.owner_id === Number(deviceId)),
        ]),
      ) as Record<string, Resource[]>;
    },
  });
  const grants = useQuery({
    queryKey: ["grants"],
    queryFn: client.grants,
    refetchInterval: 5000,
    enabled: !!deviceId,
  });
  const tasks = useQuery({
    queryKey: ["tasks", deviceId],
    queryFn: () => client.tasks("?device_id=" + deviceId),
    enabled: !!deviceId,
    refetchInterval: !!deviceId ? 10000 : false,
  });
  if (devices.isPending) return <State loading />;
  if (devices.error)
    return <State error={devices.error} onRetry={() => devices.refetch()} />;
  if (deviceId) {
    if (!d) return <State empty="设备不存在或已移除" />;
    const ownedTasks =
      tasks.data?.items.filter((t) => t.device_id === d.id) ?? [];
    const ownedGrants =
      grants.data?.items.filter((g) => g.device_id === d.id) ?? [];
    return (
      <div className="stack">
        <Button
          className="back-button"
          variant="ghost"
          onClick={() => nav("/devices")}
        >
          <ArrowLeft size={16} />
          全部 Gate
        </Button>
        <div className="entity-heading">
          <span className="entity-avatar">
            <Server size={28} />
          </span>
          <div>
            <h2>{d.name || "未命名 Gate"}</h2>
            <p>
              设备 #{d.id} · 最后联系 {date(d.last_seen)}
            </p>
          </div>
          <Status value={d.status} />
          <DeviceIdentity device={d} devices={devices.data?.items ?? []} />
        </div>
        <div className="detail-tabs" role="tablist" aria-label="Gate 详情">
          {tabs.map(([key, title]) => (
            <Button
              key={key}
              variant="ghost"
              role="tab"
              aria-selected={tab === key}
              onClick={() => SP({ tab: key })}
            >
              {title}
            </Button>
          ))}
        </div>
        {tab === "outbounds" && <OutboundPools device={d}/>}
        {resources.error && (
          <State error={resources.error} onRetry={() => resources.refetch()} />
        )}
        {tab === "overview" && (
          <>
            <GateHost device={d}/>
            <TrafficHistory deviceId={d.id} />
            <div className="metric-grid">
              <Metric
                title="已生效入站"
                value={
                  resources.isPending
                    ? "…"
                    : String(resources.data?.["device-inbound"].length ?? 0)
                }
              />
              <Metric
                title="客户端授权"
                value={
                  grants.isPending
                    ? "…"
                    : String(ownedGrants.filter((g) => g.enabled).length)
                }
              />
              <Metric
                title="Agent 版本"
                value={String(d.metadata.version ?? "未知")}
              />
            </div>
            <Card>
              <CardContent>
                <div className="section-heading">
                  <h2>设备身份</h2>
                  <KeyRound size={20} />
                </div>
                <div className="detail-grid">
                  <div>
                    <span>认证状态</span>
                    <strong>
                      {d.status === "approved"
                        ? "独立设备身份有效"
                        : d.status === "offline"
                          ? "身份有效，设备离线"
                          : "查看设备状态"}
                    </strong>
                  </div>
                  <div>
                    <span>计量状态</span>
                    <strong>
                      {d.status === "approved" &&
                      d.metadata.traffic_status === "fresh"
                        ? "已更新"
                        : "未知"}
                    </strong>
                  </div>
                  <div className="full">
                    <span>公钥指纹</span>
                    <code className="fingerprint">{d.fingerprint}</code>
                  </div>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <div className="section-heading">
                  <h2>最近执行任务</h2>
                  <Button variant="ghost" onClick={() => SP({ tab: "tasks" })}>
                    查看全部 <ArrowUpRight size={15} />
                  </Button>
                </div>
                {tasks.isPending ? (
                  <State loading />
                ) : tasks.error ? (
                  <State error={tasks.error} />
                ) : (
                  ownedTasks.slice(0, 5).map((t) => (
                    <div className="list-row" key={t.id}>
                      <div>
                        <strong>{t.operation}</strong>
                        <small>
                          #{t.id} · {date(t.created_at)}
                        </small>
                      </div>
                      <Status value={t.status} />
                    </div>
                  ))
                )}
                {!tasks.isPending && !tasks.error && !ownedTasks.length && (
                  <State empty="暂无执行任务" />
                )}
              </CardContent>
            </Card>
          </>
        )}
        {["inbounds", "outbounds", "routing", "certificates"].includes(tab) && (
          <Card>
            <CardContent>
              {resources.isPending || grants.isPending ? (
                <State loading />
              ) : resources.error || grants.error ? (
                <State
                  error={resources.error || grants.error}
                  onRetry={() => {
                    void resources.refetch();
                    void grants.refetch();
                  }}
                />
              ) : (
                <GateConfig
                  device={d}
                  deviceId={d.id}
                  grants={ownedGrants}
                  tab={tab}
                  resources={resources.data ?? {}}
                  tasks={tasks.data?.items ?? []}
                />
              )}
            </CardContent>
          </Card>
        )}
        {tab === "grants" && (
          <Card>
            <CardContent>
              <div className="section-heading">
                <h2>独立客户端授权</h2>
                <Button
                  variant="outline"
                  onClick={() => nav("/grants?device=" + d.id)}
                >
                  管理授权
                </Button>
              </div>
              {grants.isPending ? (
                <State loading />
              ) : grants.error ? (
                <State error={grants.error} />
              ) : ownedGrants.length ? (
                <div className="list-rows">
                  {ownedGrants.map((g) => (
                    <div className="list-row" key={g.id}>
                      <div>
                        <strong>
                          授权 #{g.id} · 用户 #{g.user_id}
                        </strong>
                        <small>
                          入站 #{g.inbound_id} · 已用 {bytes(g.used)}
                        </small>
                      </div>
                      <Status value={g.status} />
                    </div>
                  ))}
                </div>
              ) : (
                <State empty="暂无客户端授权" />
              )}
            </CardContent>
          </Card>
        )}
        {tab === "tasks" && <TasksPage deviceId={d.id} />}
        {tab === "topology" &&
          (resources.isPending || grants.isPending ? (
            <State loading />
          ) : resources.error || grants.error ? (
            <State
              error={resources.error || grants.error}
              onRetry={() => {
                void resources.refetch();
                void grants.refetch();
              }}
            />
          ) : (
            <Suspense fallback={<State loading />}>
              <GateTopology
                resources={resources.data ?? {}}
                grants={ownedGrants}
                go={(tab) => SP({ tab })}
              />
            </Suspense>
          ))}
        {tab === "cores" && (
          <Card>
            <CardContent>
              <GateCores device={d} tasks={tasks.data?.items ?? []} />
            </CardContent>
          </Card>
        )}
      </div>
    );
  }
  const rows = (devices.data?.items ?? []).filter(
    (d) =>
      (state === "all" || d.status === state) &&
      (d.name.toLowerCase().includes(search.toLowerCase()) ||
        String(d.id) === search),
  );
  const online = (devices.data?.items ?? []).filter(
    (d) => d.status === "approved",
  ).length;
  return (
    <div className="stack">
      <div className="page-intro">
        <div>
          <h2>你的 Gate 网络</h2>
          <p>独立设备身份，主动连接中心。点击设备查看配置与执行状态。</p>
        </div>
        <DeviceIdentity devices={devices.data?.items ?? []} />
      </div>
      <div className="metric-grid">
        <Metric
          title="设备总数"
          value={String(devices.data?.items.length ?? 0)}
        />
        <Metric
          title="在线设备"
          value={String(online)}
          caption="最近 60 秒内联系"
        />
        <Metric
          title="离线设备"
          value={String(
            (devices.data?.items ?? []).filter((d) => d.status === "offline")
              .length,
          )}
        />
      </div>
      <Card className="table-card">
        <div className="table-toolbar">
          <div className="search-field">
            <Search size={17} />
            <Input
              aria-label="搜索 Gate"
              placeholder="搜索设备名称…"
              value={search}
              onChange={(e) => S(e.target.value)}
            />
          </div>
          <div className="filter-pills">
            {[
              ["all", "全部"],
              ["approved", "在线"],
              ["offline", "离线"],
              ["pending", "待配对"],
            ].map(([key, title]) => (
              <Button
                size="sm"
                variant={state === key ? "secondary" : "ghost"}
                key={key}
                onClick={() => F(key)}
              >
                {title}
              </Button>
            ))}
          </div>
        </div>
        {rows.length ? (
          <div className="device-cards">
            {rows.map((d) => (
              <DeviceCard
                key={d.id}
                device={d}
                onOpen={() => nav("/devices/" + d.id)}
              />
            ))}
          </div>
        ) : (
          <State empty="没有匹配的设备" />
        )}
      </Card>
    </div>
  );
}
function DeviceCard({
  device: d,
  onOpen,
}: {
  device: Device;
  onOpen: () => void;
}) {
  return (
    <article className="device-card">
      <div className="section-heading">
        <span className="device-symbol">
          <Server size={22} />
        </span>
        <Status value={d.status} />
      </div>
      <Button className="device-name" variant="link" onClick={onOpen}>
        {d.name || "未命名 Gate"}
        <ArrowUpRight size={16} />
      </Button>
      <p>
        设备 #{d.id} · Agent {String(d.metadata.version ?? "未知")}
      </p>
      <div className="device-facts">
        <span>
          <Clock size={14} />
          {date(d.last_seen)}
        </span>
        <span>
          <ShieldCheck size={14} />
          {d.status === "revoked" ? "身份已撤销" : "独立设备身份"}
        </span>
      </div>
      <Button className="device-open" variant="outline" onClick={onOpen}>
        查看设备
      </Button>
    </article>
  );
}
