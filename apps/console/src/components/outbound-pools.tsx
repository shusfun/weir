import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OptionRow, ReorderButtons, Field, Notice, SectionHeader,
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
  Switch,
  Checkbox,
} from "@migate/ui";
import {
  Plus,
  RefreshCw,
  Layers,
  ArrowUpRight,
  ListOrdered,
} from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { client } from "../lib/client";
import { type Device, date } from "../lib/models";
import { State, Status } from "./feedback";

type Pool = {
  id: number;
  name: string;
  source_id: number;
  version: number;
  status: string;
  enabled: boolean;
  task_id?: number;
  selected: string[];
  member_count: number;
  last_error?: string;
  last_applied?: string;
  auto_refresh: boolean;
  prepend: boolean;
  priority: number;
  sync_error?: string;
  sync_unsupported?: number;
  last_sync_check?: string;
};
type Candidate = {
  identity: string;
  name: string;
  protocol: string;
  server: string;
  port: number;
  supported: boolean;
  reason: string;
};
type Preview = {
  items: Candidate[];
  preview_token: string;
  version: number;
  duplicates: number;
};
const reasons: Record<string, string> = {
  unsupported_protocol: "内核不支持此协议",
  unsupported_protocol_fields: "协议字段尚未适配",
  unsupported_managed_transport: "内核传输字段尚未适配",
  unsupported_profile: "连接参数不完整",
};
export function OutboundPools({ device }: { device: Device }) {
  const cache = useQueryClient(),
    base = `/admin/devices/${device.id}/pools`;
  const pools = useQuery({
    queryKey: ["outbound-pools", device.id],
    queryFn: () => api(base) as Promise<{ items: Pool[] }>,
    refetchInterval: 5000,
  });
  const sources = useQuery({
    queryKey: ["resources", "sources"],
    queryFn: () => client.resources("sources"),
  });
  const [editor, edit] = useState<{
    id: number;
    name: string;
    source_id: number;
    version: number;
    auto_refresh: boolean;
    prepend: boolean;
  } | null>(null);
  const [active, open] = useState<Pool | null>(null),
    [preview, setPreview] = useState<Preview | null>(null),
    [selected, select] = useState<string[]>([]),
    [search, setSearch] = useState("");
  const [action, setAction] = useState<"apply" | "disable" | "delete" | null>(
      null,
    ),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const [poolOrder, setPoolOrder] = useState<Pool[] | null>(null);
  // 调整展示顺序：与相邻项互换位置
  const swap = (i: number, by: number) =>
    setPoolOrder((order) => {
      if (!order) return order;
      const next = [...order];
      [next[i], next[i + by]] = [next[i + by], next[i]];
      return next;
    });
  const sortedPools = [...(pools.data?.items ?? [])].sort(
    (a, b) => (a.priority ?? 0) - (b.priority ?? 0) || a.id - b.id,
  );
  const capable = (
    device.metadata.capabilities as string[] | undefined
  )?.includes("outbound.pool.v1");
  async function perform(f: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await f();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  async function refreshLists() {
    await Promise.all([
      cache.invalidateQueries({ queryKey: ["outbound-pools", device.id] }),
      cache.invalidateQueries({ queryKey: ["tasks"] }),
      cache.invalidateQueries({ queryKey: ["device-resources"] }),
    ]);
  }
  function inspect(p: Pool) {
    open(p);
    setPreview(null);
    select(p.selected ?? []);
    setSearch("");
    setAction(null);
    setError("");
  }
  async function load(refresh: boolean) {
    if (!active) return;
    await perform(async () => {
      const p = (await api(`${base}/${active.id}/preview`, "POST", {
        version: active.version,
        refresh,
      })) as Preview;
      setPreview(p);
      select(
        active.auto_refresh
          ? p.items.filter((n) => n.supported).map((n) => n.identity)
          : (active.selected ?? []).filter((id) =>
              p.items.some((n) => n.identity === id && n.supported),
            ),
      );
      setAction(null);
    });
  }
  async function save() {
    if (!editor) return;
    await perform(async () => {
      await api(base, "POST", editor);
      edit(null);
      setNotice("出站池已保存，请预览节点并选择应用。");
      await refreshLists();
    });
  }
  async function apply() {
    if (!active || !action) return;
    await perform(async () => {
      const t = await api(`${base}/${active.id}/apply`, "POST", {
        version: active.version,
        preview_token: preview?.preview_token ?? "",
        selected: action === "apply" ? selected : [],
        enabled: action === "apply",
        delete: action === "delete",
        confirm: true,
      });
      setNotice(`任务 #${t.id} 已提交，等待 Gate 回执。`);
      open(null);
      setAction(null);
      await refreshLists();
    });
  }
  const visible =
    preview?.items.filter((n) =>
      `${n.name} ${n.protocol} ${n.server}`
        .toLowerCase()
        .includes(search.toLowerCase()),
    ) ?? [];
  return (
    <Card>
      <CardContent>
        <SectionHeader title={<><Layers size={18} /> 订阅出站池</>} description="从订阅源选择出口节点，整批应用后可在路由中引用。" actions={<>
          <Button
            variant="outline"
            disabled={busy || !sortedPools.length}
            onClick={() =>
              void perform(async () => {
                const result = await api(`${base}/refresh`, "POST", {});
                setNotice(
                  result.items
                    .map(
                      (x: { pool_id: number; result: string }) =>
                        `${sortedPools.find((p) => p.id === x.pool_id)?.name ?? "出站池"}：${x.result === "manual_preview_required" ? "来源已刷新，请预览应用" : x.result === "sync_checked" ? "已检查同步，请查看状态与回执" : x.result === "source_refresh_failed_previous_preserved" ? "来源刷新失败，保留原出口" : "仍有任务待确认"}`,
                    )
                    .join("；"),
                );
                await refreshLists();
              })
            }
          >
            <RefreshCw size={15} />
            刷新全部来源
          </Button>
          <Button
            variant="outline"
            disabled={
              busy ||
              sortedPools.length < 2 ||
              sortedPools.some((p) => p.status === "pending")
            }
            onClick={() => {
              setError("");
              setPoolOrder(sortedPools);
            }}
          >
            <ListOrdered size={15} />
            调整池顺序
          </Button>
          <Button
            disabled={!capable}
            onClick={() => {
              setError("");
              edit({
                id: 0,
                name: "",
                source_id: 0,
                version: 0,
                auto_refresh: false,
                prepend: false,
              });
            }}
          >
            <Plus size={16} />
            创建出站池
          </Button></>} />
        {!capable && (
          <Notice tone="warn">
            设备需升级以支持出站池，现有手动出站可继续使用。
          </Notice>
        )}
        {notice && (
          <Notice role="status">
            {notice}
          </Notice>
        )}
        {error && !active && !editor && <State error={error} />}
        {pools.isPending ? (
          <State loading />
        ) : pools.error ? (
          <State error={pools.error} onRetry={() => pools.refetch()} />
        ) : !pools.data.items.length ? (
          <State empty="暂无出站池。先添加订阅源，再选择出口节点。" />
        ) : (
          sortedPools.map((p) => (
            <div className="list-row" key={p.id}>
              <div>
                <strong>{p.name}</strong>
                <small>
                  {String(
                    sources.data?.items.find((s) => s.id === p.source_id)?.data
                      .name ?? "订阅源不可用",
                  )}{" "}
                  · 已生效 {p.member_count ?? 0} 个 ·{" "}
                  {p.enabled ? "启用" : "停用"}
                </small>
                <small>
                  最近应用 {date(p.last_applied)}
                  {p.task_id ? ` · 任务 #${p.task_id}` : ""}
                </small>
                <small>
                  {p.auto_refresh ? "自动同步全部可托管节点" : "手动选择应用"} ·
                  最近检查 {date(p.last_sync_check)}
                </small>
                {p.sync_error && (
                  <small role="alert">
                    自动同步暂停，保留已生效出口：
                    {p.sync_error === "pool_member_has_routes"
                      ? "路由仍引用已移除节点，请先调整路由"
                      : p.sync_error === "fresh_bounded_source_required"
                        ? "等待新鲜且有界的来源缓存"
                        : "请检查来源或设备任务"}
                  </small>
                )}
                {!!p.sync_unsupported && (
                  <small>
                    上次自动检查有 {p.sync_unsupported}{" "}
                    项暂不支持托管，未加入出口。
                  </small>
                )}
                {p.last_error && (
                  <small role="alert">应用失败，保留上次生效配置</small>
                )}
              </div>
              <Status value={p.status} />
              <div className="row-actions">
                <Button
                  variant="outline"
                  disabled={!capable || p.status === "pending"}
                  onClick={() => inspect(p)}
                >
                  节点与应用
                </Button>
                <Button
                  variant="ghost"
                  disabled={p.status === "pending"}
                  onClick={() => {
                    setError("");
                    edit({
                      id: p.id,
                      name: p.name,
                      source_id: p.source_id,
                      version: p.version,
                      auto_refresh: p.auto_refresh,
                      prepend: p.prepend,
                    });
                  }}
                >
                  编辑
                </Button>
              </div>
            </div>
          ))
        )}
        <Button variant="link" size="sm" render={<Link to="/subscriptions/sources" />}>
          管理订阅源 <ArrowUpRight />
        </Button>
        <Dialog
          open={!!poolOrder}
          onOpenChange={(v) => {
            if (!v && !busy) setPoolOrder(null);
          }}
        >
          <DialogContent size="md">
            <DialogTitle>调整订阅出站池顺序</DialogTitle>
            <DialogDescription>
              保存完整池版本快照。仅调整展示顺序，不改变路由匹配、凭据或节点授权。
            </DialogDescription>
            <div className="stack">
              {poolOrder?.map((p, i) => (
                <div className="selection-row" key={p.id}>
                  <span className="break-all">
                    {i + 1}. {p.name}
                  </span>
                  <ReorderButtons
                    label={p.name}
                    upDisabled={busy || i === 0}
                    downDisabled={busy || i === poolOrder.length - 1}
                    onUp={() => swap(i, -1)}
                    onDown={() => swap(i, 1)}
                  />
                </div>
              ))}
            </div>
            {error && (
              <Notice tone="bad">
                {error}
              </Notice>
            )}
            <DialogFooter>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => setPoolOrder(null)}
              >
                取消
              </Button>
              <Button
                disabled={busy}
                onClick={() =>
                  void perform(async () => {
                    await api(`${base}/order`, "POST", {
                      items: poolOrder?.map((p) => ({
                        id: p.id,
                        version: p.version,
                      })),
                    });
                    setPoolOrder(null);
                    setNotice("出站池顺序已保存。");
                    await refreshLists();
                  })
                }
              >
                保存池顺序
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <Dialog
          open={!!editor}
          onOpenChange={(v) => {
            if (!v && !busy) edit(null);
          }}
        >
          <DialogContent>
            <DialogTitle>
              {editor?.id ? "编辑出站池" : "创建出站池"}
            </DialogTitle>
            <DialogDescription>
              默认手动选择。自动同步须显式开启；仍被路由引用的节点不会被自动移除。
            </DialogDescription>
            {editor && (
              <form
                id="pool-editor"
                className="stack"
                onSubmit={(e) => {
                  e.preventDefault();
                  void save();
                }}
              >
                <div>
                  <Field label="名称" htmlFor="pool-name"><Input
                    id="pool-name"
                    required
                    maxLength={100}
                    value={editor.name}
                    onChange={(e) => edit({ ...editor, name: e.target.value })}
                  /></Field>
                </div>
                <div>
                  <Label>订阅源</Label>
                  <Select
                    value={editor.source_id ? String(editor.source_id) : ""}
                    onValueChange={(v) =>
                      edit({ ...editor, source_id: Number(v) })
                    }
                  >
                    <SelectTrigger aria-label="订阅源">
                      <SelectValue placeholder="选择已启用订阅源">
                        {editor.source_id
                          ? String(
                              sources.data?.items.find(
                                (s) => s.id === editor.source_id,
                              )?.data.name ?? "订阅源不可用",
                            )
                          : undefined}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {sources.data?.items
                        .filter((s) => s.data.enabled !== false)
                        .map((s) => (
                          <SelectItem key={s.id} value={String(s.id)}>
                            {String(s.data.name ?? s.key)}
                          </SelectItem>
                        ))}
                    </SelectContent>
                  </Select>
                </div>
                {sources.error && (
                  <State
                    error={sources.error}
                    onRetry={() => sources.refetch()}
                  />
                )}
                <p className="muted">
                  沿用订阅源的刷新周期、请求头和安全校验。修改来源后需重新预览应用。
                </p>
                <div className="pool-toggle-row">
                  <Label htmlFor="pool-prepend" className="min-w-0">在手动出站前展示</Label>
                  <Switch
                    id="pool-prepend"
                    checked={editor.prepend}
                    onCheckedChange={(v) => edit({ ...editor, prepend: v })}
                  />
                </div>
                <div className="pool-toggle-row">
                  <Label htmlFor="pool-auto-refresh" className="min-w-0">自动同步全部可托管节点</Label>
                  <Switch
                    id="pool-auto-refresh"
                    checked={editor.auto_refresh}
                    onCheckedChange={(v) =>
                      edit({ ...editor, auto_refresh: v })
                    }
                  />
                </div>
                {editor.auto_refresh && (
                  <Notice>
                    首次预览并应用后，随来源缓存更新自动增减全部可托管节点，失败保留旧库存；暂不支持的协议不纳入。已有路由引用阻止移除，不会改成直连。
                  </Notice>
                )}
                {error && (
                  <Notice tone="bad">
                    {error}
                  </Notice>
                )}
              </form>
            )}
            <DialogFooter>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => edit(null)}
              >
                取消
              </Button>
              <Button
                type="submit"
                form="pool-editor"
                disabled={busy || !editor?.source_id}
              >
                {busy ? "保存中…" : "保存"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <Dialog
          open={!!active}
          onOpenChange={(v) => {
            if (!v && !busy) open(null);
          }}
        >
          <DialogContent size="lg">
            <DialogTitle>{active?.name} · 出口节点</DialogTitle>
            <DialogDescription>
              凭据保留在服务端。去重使用完整连接参数；不支持的组合会显示原因。
            </DialogDescription>
            <div className="stack">
              <div className="row-actions">
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() => void load(false)}
                >
                  预览缓存
                </Button>
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() => void load(true)}
                >
                  <RefreshCw size={15} />
                  刷新并预览
                </Button>
              </div>
              {preview ? (
                <>
                  <p>
                    去重 {preview.duplicates} 项 · 已选 {selected.length} 个 ·
                    预览 10 分钟有效
                  </p>
                  {active?.auto_refresh && (
                    <Notice>
                      自动模式将同步全部可托管节点；需手动选择部分节点时，请先关闭自动同步。
                    </Notice>
                  )}
                  <Input
                    aria-label="筛选出口节点"
                    placeholder="搜索节点、协议或服务器"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                  <div className="row-actions">
                    <Button
                      variant="ghost"
                      disabled={active?.auto_refresh}
                      onClick={() =>
                        select([
                          ...new Set([
                            ...selected,
                            ...visible
                              .filter((n) => n.supported)
                              .map((n) => n.identity),
                          ]),
                        ])
                      }
                    >
                      选择筛选结果
                    </Button>
                    <Button
                      variant="ghost"
                      disabled={active?.auto_refresh}
                      onClick={() => select([])}
                    >
                      清空选择
                    </Button>
                  </div>
                  <div className="stack">
                    {!visible.length ? (
                      <State
                        empty="没有匹配的节点"
                        detail="请调整搜索条件，已选节点保持不变。"
                      />
                    ) : (
                      visible.map((n) => (
                        <OptionRow key={n.identity}>
                          <Checkbox
                            aria-label={`选择 ${n.name || n.server}`}
                            disabled={
                              !n.supported || busy || active?.auto_refresh
                            }
                            checked={selected.includes(n.identity)}
                            onCheckedChange={(checked) =>
                              select(
                                checked === true
                                  ? [...selected, n.identity]
                                  : selected.filter((x) => x !== n.identity),
                              )
                            }
                          />
                          <span>
                            <strong>{n.name || n.server}</strong>
                            <small>
                              {n.protocol} · {n.server}:{n.port}
                            </small>
                            {!n.supported && (
                              <small>
                                {reasons[n.reason] ?? "此组合尚不支持"}
                              </small>
                            )}
                          </span>
                        </OptionRow>
                      ))
                    )}
                  </div>
                </>
              ) : (
                <State
                  empty="选择预览缓存，或刷新后查看当前上游节点。"
                  detail="查看预览不会更改已生效出口。"
                />
              )}
              {error && (
                <Notice tone="bad">
                  {error}
                </Notice>
              )}
              <Notice tone="warn">
                移除仍被路由引用的节点会被拒绝，请先调整路由。刷新或配置失败保留上次生效配置；停用不会更改用户授权。
              </Notice>
              {action && (
                <Notice tone={action === "apply" ? "info" : "warn"} role="alert">
                  <strong>
                    {action === "apply"
                      ? `确认整批应用 ${selected.length} 个节点？`
                      : action === "delete"
                        ? "确认删除出站池与本池节点？"
                        : "确认停用出站池并移除本池出口？"}
                  </strong>
                  <p>Gate 成功回执前，已生效列表保持原状。</p>
                  <Button
                    variant={action === "apply" ? "default" : "destructive"}
                    disabled={busy}
                    onClick={() => void apply()}
                  >
                    {busy ? "提交中…" : "确认提交"}
                  </Button>
                  <Button
                    variant="ghost"
                    disabled={busy}
                    onClick={() => setAction(null)}
                  >
                    返回选择
                  </Button>
                </Notice>
              )}
            </div>
            <DialogFooter>
              <Button
                variant="destructive"
                disabled={busy}
                onClick={() => setAction("delete")}
              >
                删除池
              </Button>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => setAction("disable")}
              >
                停用
              </Button>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => open(null)}
              >
                关闭
              </Button>
              <Button
                disabled={busy || !preview || !selected.length}
                onClick={() => setAction("apply")}
              >
                应用所选节点
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}
