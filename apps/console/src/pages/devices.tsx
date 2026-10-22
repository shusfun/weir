import { useState, lazy, Suspense } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams, Link } from "react-router-dom";
import { CodeBlock, SearchInput, SectionHeader, Button, Card, CardContent, Faceplate, FaceplateTitle, StatusLamp, toneOf, Segmented, PageHeader } from "@migate/ui";
import { TabNav } from "../shell/tab-nav";
import { deviceTabs } from "../shell/navigation";
import {
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
  const { deviceId, tab: tabParam } = useParams();
  const tab = tabParam ?? "overview";
  const SP = ({ tab }: { tab: string }) => nav("/devices/" + deviceId + (tab === "overview" ? "" : "/" + tab));
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
          className="back-button pl-1 text-[var(--mg-ink-3)]"
          variant="ghost"
          onClick={() => nav("/devices")}
        >
          <ArrowLeft size={16} />
          全部 Gate
        </Button>
        <Faceplate className="device-plate">
          <StatusLamp tone={toneOf(d.status)} size="lg" />
          <FaceplateTitle
            eyebrow={"Gate · #" + d.id}
            title={d.name || "未命名 Gate"}
            meta={
              <>
                <span>Agent {String(d.metadata.version ?? "未知")}</span>
                <span>最后联系 {date(d.last_seen)}</span>
              </>
            }
          />
          <div className="device-plate-actions">
            <Status value={d.status} />
            <DeviceIdentity device={d} devices={devices.data?.items ?? []} />
          </div>
        </Faceplate>
        <TabNav
          label="Gate 详情"
          items={deviceTabs.map((t) => ({ to: "/devices/" + d.id + (t.key ? "/" + t.key : ""), label: t.label, icon: t.icon, end: !t.key }))}
        />
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
                <SectionHeader title="设备身份" actions={<KeyRound size={20} />} />
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
                    <CodeBlock>{d.fingerprint}</CodeBlock>
                  </div>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <SectionHeader title="最近执行任务" actions={<Button variant="ghost" onClick={() => SP({ tab: "tasks" })}>
                    查看全部 <ArrowUpRight size={15} />
                  </Button>} />
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
              <SectionHeader title="独立客户端授权" actions={<Button variant="outline" render={<Link to={"/grants?device=" + d.id} />}>
                  管理授权
                </Button>} />
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
      <PageHeader
        title="你的 Gate 网络"
        description="每台 Gate 拥有独立设备身份并主动连接中心。打开设备查看入站、出站、路由与执行回执。"
        actions={<DeviceIdentity devices={devices.data?.items ?? []} />}
      />
      <div className="metric-grid">
        <Metric title="设备总数" value={String(devices.data?.items.length ?? 0)} />
        <Metric title="在线设备" value={String(online)} caption="最近 60 秒内联系" />
        <Metric
          title="离线设备"
          value={String((devices.data?.items ?? []).filter((d) => d.status === "offline").length)}
        />
      </div>
      <Card className="table-card">
        <div className="table-toolbar">
          <SearchInput aria-label="搜索 Gate" placeholder="搜索设备名称或编号…" value={search} onChange={(e) => S(e.target.value)} />
          <Segmented
            aria-label="按状态筛选设备"
            size="sm"
            fullWidth="mobile"
            value={state}
            onValueChange={F}
            options={[["all", "全部"], ["approved", "在线"], ["offline", "离线"], ["pending", "待配对"]]}
          />
          <span className="result-count">{rows.length} 台</span>
        </div>
        {rows.length ? (
          <div className="rack-list padded">
            {rows.map((d) => (
              <DeviceCard key={d.id} device={d} onOpen={() => nav("/devices/" + d.id)} />
            ))}
          </div>
        ) : (
          <State empty={search || state !== "all" ? "没有匹配的设备" : "还没有 Gate 设备"} detail="生成一次性配对码后，在 VPS 上运行 Agent 完成配对。" />
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
  // 1U 机架面板：状态灯 · 名称 · 关键事实 · 打开
  return (
    <Faceplate compact className="rack-row">
      <StatusLamp tone={toneOf(d.status)} size="lg" label={"状态：" + d.status} />
      <div className="rack-name">
        <button type="button" onClick={onOpen}>{d.name || "未命名 Gate"}</button>
        <small>#{d.id}</small>
      </div>
      <div className="rack-fact">
        <span>Agent</span>
        <span>{String(d.metadata.version ?? "未知")}</span>
      </div>
      <div className="rack-fact">
        <span><Clock size={11} style={{ display: "inline", verticalAlign: "-1px" }} /> 最后联系</span>
        <span>{date(d.last_seen)}</span>
      </div>
      <div className="row-actions">
        <Status value={d.status} />
        <Button variant="outline" size="sm" onClick={onOpen} aria-label={"打开 " + (d.name || "Gate #" + d.id)}>
          打开 <ArrowUpRight />
        </Button>
      </div>
    </Faceplate>
  );
}
