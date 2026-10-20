import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent } from "@migate/ui";
import { api } from "../api";
import { State } from "./feedback";

type GateRollout = { id?: number; name: string; version: string; rollout: string };
type ReleaseState = {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_url: string;
  status: string;
  message?: string;
  gates: GateRollout[];
};

const rolloutText: Record<string, string> = {
  pending: "待执行",
  running: "执行中",
  applied: "已生效",
  failed: "失败",
  unknown: "未知",
};

export function ReleaseUpdateCard() {
  const cache = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [mode, setMode] = useState("retain");
  const [scopeConfirmed, setScopeConfirmed] = useState(false);
  const query = useQuery({
    queryKey: ["release-status"],
    queryFn: () => api("/admin/release") as Promise<ReleaseState>,
    refetchOnWindowFocus: false,
  });
  if (query.isPending) return <State loading />;
  if (!query.data) return <State error={query.error} onRetry={() => query.refetch()} />;
  const data = query.data;
  const confirm = async () => {
    setBusy(true);
    setError("");
    try {
      await api("/admin/release", "POST", { confirm: true });
      await cache.invalidateQueries({ queryKey: ["release-status"] });
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "发布请求失败");
    } finally {
      setBusy(false);
    }
  };
  const uninstall = async (id: number) => {
    setBusy(true);
    setError("");
    try {
      await api(`/admin/devices/${id}/uninstall`, "POST", { confirm: true, mode, scope_confirmed: scopeConfirmed });
      await cache.invalidateQueries({ queryKey: ["release-status"] });
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "卸载请求失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card>
      <CardContent className="stack">
        <div className="section-heading">
          <div>
            <h3>GitHub Release</h3>
            <p>中心校验安装包后替换自己，再把同一份 Gate 程序推送替换。Gate 不访问 GitHub。</p>
          </div>
          <Badge>{data.status === "ok" && !data.update_available ? "已是最新" : data.status === "unknown" ? "不可用" : "可检查"}</Badge>
        </div>
        {data.status === "unknown" && <State error={data.message || "发布信息不可用"} detail={data.message || "发布信息不可用"} onRetry={() => query.refetch()} />}
        <p>当前版本 {data.current_version || "未知"}。最新正式版本 {data.latest_version || "未知"}。</p>
        {data.release_url && <p><a href={data.release_url}>查看 Release</a></p>}
        <ul>
          {data.gates.map((gate) => (
            <li key={gate.id ?? gate.name}>
              {gate.name || "Gate"}：心跳 {gate.version || "未知"}，{rolloutText[gate.rollout] || "未知"}
              {gate.id ? <Button type="button" disabled={busy || (mode !== "retain" && !scopeConfirmed)} onClick={() => uninstall(gate.id!)}>卸载此 Gate</Button> : null}
            </li>
          ))}
        </ul>
        <label>卸载范围
          <select value={mode} onChange={(event) => { setMode(event.target.value); setScopeConfirmed(false); }}>
            <option value="retain">保留数据和内核</option>
            <option value="cores">移除托管内核</option>
            <option value="purge">备份后清除本项目数据</option>
          </select>
        </label>
        {mode !== "retain" && <label><input type="checkbox" checked={scopeConfirmed} onChange={(event) => setScopeConfirmed(event.target.checked)} />再次确认这个清除范围</label>}
        {error && <p className="notice error-message" role="alert">{error}</p>}
        <Button type="button" disabled={busy || !data.update_available} onClick={confirm}>确认拉取并推送 Gate</Button>
      </CardContent>
    </Card>
  );
}
