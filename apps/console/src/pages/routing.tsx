import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams, useNavigate } from "react-router-dom";
import {
  Button,
  Card,
  CardContent,
  Label,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import { client } from "../lib/client";
import { State } from "../components/feedback";
import { GateConfig } from "../components/gate-config";
import type { Resource } from "../lib/models";

export function RoutingPage() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const [tab, setTab] = useState("routing");
  const devices = useQuery({ queryKey: ["devices"], queryFn: client.devices });
  const selected = params.get("device") ?? "";
  const device = devices.data?.items.find((d) => String(d.id) === selected);
  const resources = useQuery({
    queryKey: ["device-resources", selected],
    enabled: !!device,
    refetchInterval: 5000,
    queryFn: async () => {
      const kinds = [
        "device-inbound",
        "device-outbound",
        "device-routing",
        "device-certificate",
      ];
      const all = await Promise.all(kinds.map(client.resources));
      return Object.fromEntries(
        kinds.map((kind, i) => [
          kind,
          all[i].items.filter((r) => r.owner_id === Number(selected)),
        ]),
      ) as Record<string, Resource[]>;
    },
  });
  const grants = useQuery({
    queryKey: ["grants"],
    queryFn: client.grants,
    enabled: !!device,
    refetchInterval: 5000,
  });
  const tasks = useQuery({
    queryKey: ["tasks", selected],
    queryFn: () => client.tasks("?device_id=" + selected),
    enabled: !!device,
    refetchInterval: 5000,
  });
  if (devices.isPending) return <State loading />;
  if (devices.error)
    return <State error={devices.error} onRetry={() => devices.refetch()} />;
  return (
    <div className="stack">
      <div className="page-intro">
        <div>
          <h2>按设备管理流量去向</h2>
          <p>
            先选择 Gate，再配置出站与路由。配置只有收到设备回执后才记为生效。
          </p>
        </div>
      </div>
      <Card>
        <CardContent>
          <Label>选择 Gate</Label>
          <Select
            value={selected}
            onValueChange={(v) => setParams({ device: String(v) })}
          >
            <SelectTrigger aria-label="选择 Gate">
              <SelectValue placeholder="选择要管理的设备">
                {device?.name || (selected ? "设备不存在" : "选择要管理的设备")}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {devices.data?.items.map((d) => (
                <SelectItem key={d.id} value={String(d.id)}>
                  {d.name || "未命名 Gate"} · #{d.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {device && (
            <div className="section-heading">
              <p>当前设备：{device.name}。离线任务将在重新连接后执行。</p>
              <Button
                variant="link"
                onClick={() => navigate(`/devices/${device.id}?tab=topology`)}
              >
                查看配置拓扑
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
      {!selected ? (
        <State empty="选择 Gate 后查看已生效的配置" />
      ) : !device ? (
        <State empty="所选设备不存在或已移除" />
      ) : (
        <>
          <div className="detail-tabs" role="tablist" aria-label="出站与路由">
            {[
              ["routing", "路由规则"],
              ["outbounds", "出站连接"],
            ].map(([key, name]) => (
              <Button
                key={key}
                role="tab"
                aria-selected={tab === key}
                variant="ghost"
                onClick={() => setTab(key)}
              >
                {name}
              </Button>
            ))}
          </div>
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
                  device={device}
                  key={selected + tab}
                  deviceId={device.id}
                  grants={(grants.data?.items ?? []).filter(
                    (g) => g.device_id === device.id,
                  )}
                  tab={tab}
                  resources={resources.data ?? {}}
                  tasks={tasks.data?.items ?? []}
                />
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
