import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Button,
  Card,
  CardContent,
  Input,
  Label,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@migate/ui";
import { Plus, Search, ShieldCheck } from "lucide-react";
import { api } from "../api";
import { client } from "../lib/client";
import { bytes, type Grant } from "../lib/models";
import { State, Status } from "../components/feedback";

export function GrantsPage() {
  const cache = useQueryClient();
  const [params] = useSearchParams();
  const [open, O] = useState(false);
  const [search, S] = useState("");
  const [user, U] = useState(params.get("user") ?? "");
  const [device, D] = useState(params.get("device") ?? "");
  const [inbound, I] = useState("");
  const [userSearch, US] = useState("");
  const [busy, B] = useState(false);
  const [error, E] = useState("");
  const [notice, N] = useState("");
  const [revoke, R] = useState<Grant | null>(null);
  const grants = useQuery({
    queryKey: ["grants"],
    queryFn: client.grants,
    refetchInterval: 5000,
  });
  const users = useQuery({
    queryKey: ["grant-users", userSearch],
    queryFn: () =>
      client.users("?page=1&page_size=100&q=" + encodeURIComponent(userSearch)),
  });
  const devices = useQuery({ queryKey: ["devices"], queryFn: client.devices });
  const inbounds = useQuery({
    queryKey: ["inbounds"],
    queryFn: () => client.resources("device-inbound"),
  });
  const userName = (id: number) =>
    users.data?.items.find((u) => u.id === id)?.email ?? "用户 #" + id;
  const deviceName = (id: number) =>
    devices.data?.items.find((d) => d.id === id)?.name ?? "Gate #" + id;
  const inboundName = (did: number, id: number) => {
    const r = inbounds.data?.items.find(
      (r) => r.owner_id === did && Number(r.data.id) === id,
    );
    return String(r?.data.remark ?? r?.data.tag ?? "入站 #" + id);
  };
  const loading =
    grants.isPending ||
    users.isPending ||
    devices.isPending ||
    inbounds.isPending;
  const failure =
    grants.error ?? users.error ?? devices.error ?? inbounds.error;
  async function submit() {
    B(true);
    E("");
    try {
      if (revoke) {
        await api("/admin/grants/" + revoke.id, "DELETE");
        R(null);
        N("撤销任务已提交，等待 Gate 回执。设备离线时会保持待执行。");
      } else {
        await api("/admin/grants", "POST", {
          user_id: Number(user),
          device_id: Number(device),
          inbound_id: Number(inbound),
        });
        O(false);
        N("授权任务已提交。收到 Gate 成功回执后，节点才会出现在该用户订阅中。");
      }
      await cache.invalidateQueries({ queryKey: ["grants"] });
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  const rows = (grants.data?.items ?? []).filter(
    (g) =>
      (!params.get("user") || g.user_id === Number(params.get("user"))) &&
      (!params.get("device") || g.device_id === Number(params.get("device"))) &&
      [
        userName(g.user_id),
        deviceName(g.device_id),
        inboundName(g.device_id, g.inbound_id),
      ]
        .join(" ")
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  const eligible = (inbounds.data?.items ?? []).filter(
    (r) =>
      r.owner_id === Number(device) &&
      ["vless", "vmess", "trojan", "hysteria2", "tuic"].includes(
        String(r.data.protocol),
      ),
  );
  return (
    <div className="stack">
      <div className="page-intro">
        <div>
          <h2>节点访问授权</h2>
          <p>共用入站，独立客户端凭证。所有变更以设备回执为准。</p>
        </div>
        <Button
          onClick={() => {
            E("");
            O(true);
          }}
        >
          <Plus size={16} />
          分配节点
        </Button>
      </div>
      {notice && (
        <div className="notice" role="status">
          {notice}
        </div>
      )}
      <Card className="table-card">
        <div className="table-toolbar">
          <div className="search-field">
            <Search size={16} />
            <Input
              aria-label="搜索授权"
              placeholder="搜索用户、设备或入站…"
              value={search}
              onChange={(e) => S(e.target.value)}
            />
          </div>
          <span className="result-count">{rows.length} 条授权</span>
        </div>
        <CardContent>
          {loading ? (
            <State loading />
          ) : failure ? (
            <State
              error={failure}
              onRetry={() => {
                void grants.refetch();
                void users.refetch();
                void devices.refetch();
                void inbounds.refetch();
              }}
            />
          ) : rows.length ? (
            rows.map((g) => (
              <div className="list-row grant-row" key={g.id}>
                <div>
                  <strong>{userName(g.user_id)}</strong>
                  <small>
                    {deviceName(g.device_id)} ·{" "}
                    {inboundName(g.device_id, g.inbound_id)}
                  </small>
                  <small>
                    已用 {bytes(g.used)} · 已分配预算 {bytes(g.allocated)} · #
                    {g.id}
                  </small>
                </div>
                <div className="actions">
                  <Status value={g.status} />
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!g.enabled}
                    onClick={() => {
                      E("");
                      R(g);
                    }}
                  >
                    {g.enabled ? "撤销授权" : "已撤销授权"}
                  </Button>
                </div>
              </div>
            ))
          ) : (
            <State empty="暂无匹配的授权" />
          )}
        </CardContent>
      </Card>
      <div className="help-note">
        <ShieldCheck size={18} />
        <p>
          生成订阅不会新建入站或授权。失效、撤销和未生效的授权不会发布给用户。
        </p>
      </div>
      <Dialog
        open={open || !!revoke}
        onOpenChange={(v) => {
          if (!v && !busy) {
            O(false);
            R(null);
          }
        }}
      >
        <DialogContent className="business-dialog">
          <DialogTitle>
            {revoke ? "撤销节点访问" : "分配独立客户端"}
          </DialogTitle>
          <DialogDescription>
            {revoke
              ? `${userName(revoke.user_id)} · ${deviceName(revoke.device_id)}。撤销会立即停止订阅发布，代理访问需等待设备执行。`
              : "选择用户、Gate 和已生效入站，不会为用户新增监听端口。"}
          </DialogDescription>
          <form
            className="stack"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            {!revoke && (
              <>
                <div className="field">
                  <Label htmlFor="grant-user-search">查找用户</Label>
                  <Input
                    id="grant-user-search"
                    placeholder="输入邮箱筛选…"
                    value={userSearch}
                    onChange={(e) => US(e.target.value)}
                  />
                </div>
                <Choice
                  label="授权用户"
                  value={user}
                  onChange={U}
                  options={(users.data?.items ?? [])
                    .filter((u) => u.enabled && u.quota > 0)
                    .map((u) => [String(u.id), u.email])}
                />
                <Choice
                  label="Gate 设备"
                  value={device}
                  onChange={(v) => {
                    D(v);
                    I("");
                  }}
                  options={(devices.data?.items ?? [])
                    .filter((d) => d.status === "approved")
                    .map((d) => [String(d.id), d.name])}
                />
                <Choice
                  label="已生效入站"
                  value={inbound}
                  onChange={I}
                  options={eligible.map((r) => [
                    String(r.data.id),
                    String(r.data.remark ?? r.data.tag ?? r.data.protocol) +
                      " · " +
                      r.data.protocol +
                      " :" +
                      r.data.port,
                  ])}
                />
                {device && !eligible.length && (
                  <p className="notice">
                    此 Gate 没有可授权入站，请先完成入站配置。
                  </p>
                )}
                {failure && <p role="alert">资源加载失败，请关闭后重试。</p>}
              </>
            )}
            {error && (
              <p className="notice error" role="alert">
                {error}
              </p>
            )}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  O(false);
                  R(null);
                }}
              >
                取消
              </Button>
              <Button
                type="submit"
                variant={revoke ? "destructive" : "default"}
                disabled={
                  busy ||
                  (!revoke && (!user || !device || !inbound || !!failure))
                }
              >
                {busy ? "正在提交…" : revoke ? "确认撤销" : "提交授权任务"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
function Choice({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: string[][];
}) {
  return (
    <div className="field">
      <Label>{label}</Label>
      <Select value={value || null} onValueChange={(v) => onChange(v ?? "")}>
        <SelectTrigger aria-label={label}>
          <SelectValue>
            {options.find((o) => o[0] === value)?.[1] ?? "请选择" + label}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          {options.map(([id, name]) => (
            <SelectItem value={id} key={id}>
              {name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
