// 用户：一个窗口建用户与套餐；详情中查看 / 管理其订阅链接（含凭据原文）与自建链式代理。
import { Fragment, useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { Avatar, Badge, BulkBar, Button, Card, Checkbox, DotCrowd, TrafficUsage, Input, PageHeader, SectionHeader, Segmented, Sheet, SheetContent, SkeletonTable, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, CopyButton, Notice, useToast, useConfirm } from "@weir/ui";
import { Plus, RotateCcw, Trash2 } from "lucide-react";
import { api, clause } from "../api";
import { useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { UserForm } from "../components/user-form";
import { HistoryCharts } from "../components/history-charts";
import { ChainPanel, LinkPanel, type NodeOption } from "../components/link-panel";
import { bytes, date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import { dataObject, useDialogParam } from "../components/object-link";
import { objectHref, type ObjectRef } from "../lib/objects";
import type { NodeItem, UserItem, Withheld } from "../lib/core";

// 状态标签：需要处理时写最要紧的原因（稿子「3 天后到期」「流量 95%」）
function careLabel(u: UserItem): string | null {
  if (!u.enabled) return "已停用";
  const left = u.expires_at ? Math.ceil((new Date(u.expires_at).getTime() - Date.now()) / 86400e3) : null;
  if (left != null && left < 0) return "已到期";
  if (left != null && left < 7) return `${left} 天后到期`;
  if (!u.unlimited && u.quota > 0 && u.used / u.quota >= 0.9) return `流量 ${Math.round((u.used / u.quota) * 100)}%`;
  return null;
}

function needsCare(u: UserItem) {
  const soon = u.expires_at ? new Date(u.expires_at).getTime() - Date.now() < 7 * 86400e3 : false;
  const heavy = !u.unlimited && u.quota > 0 && u.used / u.quota >= 0.9;
  return !u.enabled || soon || heavy;
}

async function bulkExtend(ids: number[], users: UserItem[], act: (key: string, run: () => Promise<unknown>, ok: string) => Promise<unknown>) {
  const dated = users.filter((u) => ids.includes(u.id) && u.expires_at);
  if (!dated.length) return;
  await act("bulk-extend", async () => {
    for (const u of dated) {
      const next = new Date(new Date(u.expires_at!).getTime() + 30 * 86400e3).toISOString();
      await api(`/admin/users/${u.id}`, "PATCH", { expires_at: next });
    }
  }, `已为 ${dated.length} 人续期 30 天`);
}

async function bulkDisable(ids: number[], act: (key: string, run: () => Promise<unknown>, ok: string) => Promise<unknown>) {
  await act("bulk-disable", async () => {
    for (const id of ids) await api(`/admin/users/${id}`, "PATCH", { enabled: false });
  }, "已停用所选用户");
}

export function UsersPage() {
  const confirm = useConfirm();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const [form, setForm] = useState<UserItem | "new" | null>(null);
  const [query, setQuery] = useState("");
  const [groupBy, setGroupBy] = useState("status");
  const [picked, setPicked] = useState<number[]>([]);
  const [showNormal, setShowNormal] = useState(false);
  const [created, setCreated] = useState<UserItem | null>(null);
  const { run: act, isBusy } = useRowAction<string>({ refetch: users.refetch });
  // 地址栏：?user=编号 是这个用户的详情（对象链接、dsh 的界面导航）；?link= / ?chain= 只知道编号时，
  // 先向中心查它属于哪个用户，再打开那个用户并高亮这一项。
  const detailParam = useDialogParam("user", users.data, !users.isFetching);
  const detail = detailParam.current;
  const [params] = useSearchParams();
  const navigate = useNavigate();
  useEffect(() => {
    const id = params.get("user");
    if (id) navigate(`/users/${id}`, { replace: true });
  }, [params, navigate]);
  const toast = useToast();
  useEffect(() => {
    for (const type of ["link", "chain"] as const) {
      const id = params.get(type);
      if (!id) continue;
      void api(`/admin/objects/${type}/${encodeURIComponent(id)}`)
        .then((r) => navigate(objectHref(r.object as ObjectRef), { replace: true }))
        .catch(() => {
          toast("没有找到要打开的对象", { tone: "warn", description: "它可能已被删除。" });
          navigate("/users", { replace: true });
        });
    }
  }, [params, navigate, toast]);
  useConsoleContext({ page: "用户", list: { total: users.data?.length ?? 0, items: (users.data ?? []).slice(0, 20).map((u) => ({ type: "user", name: u.name || u.email, id: u.id })) } });
  useConsoleContext(detail ? { object: { type: "user", name: detail.name || detail.email, id: detail.id }, dialog: { title: "用户详情" } } : null);
  const list = users.data ?? [];
  const soon = list.filter((u) => u.enabled && u.expires_at && new Date(u.expires_at).getTime() - Date.now() < 7 * 86400e3).length;
  const heavy = list.filter((u) => !u.unlimited && u.quota > 0 && u.used / u.quota >= 0.9).length;
  const total = list.reduce((sum, u) => sum + u.used, 0);
  const header = (
    <PageHeader
      title="用户"
      description={users.data ? (
        <span className="weir-sum">
          <span><b>{list.length}</b> 个用户</span>
          <span><b>{list.filter((u) => u.enabled).length}</b> 个启用</span>
          {soon > 0 && <span className="text-[var(--weir-warn-ink)]">{soon} 个 7 天内到期</span>}
          {heavy > 0 && <span className="text-[var(--weir-warn-ink)]">{heavy} 个流量超过 90%</span>}
          <span>本月共 {bytes(total)}</span>
        </span>
      ) : "正在读取用户"}
      actions={<Button onClick={() => setForm("new")}>新建用户</Button>}
    />
  );
  const optionsFor = (u: UserItem): NodeOption[] =>
    (nodes.data ?? []).filter((n) => n.enabled && (u.all_nodes || (u.node_ids ?? []).includes(n.id)));
  return (
    <div className="stack">
      {header}
      {users.data && users.data.length > 0 && (
        <Card className="px-[25px] py-[21px]">
          <div className="mb-[13px] flex flex-wrap items-center justify-between gap-3">
            <span className="text-meta text-[var(--weir-ink-2)]">全部用户 · 点越大本月流量越多，黄色是需要处理的</span>
            <span className="weir-legend"><span><i data-tone="ok" />正常</span><span><i data-tone="warn" />需要处理</span><span><i data-tone="off" />已停用</span></span>
          </div>
          <DotCrowd label="全部用户 · 点越大本月流量越多，黄色是需要处理的" onSelect={(id) => navigate(`/users/${id}`)} members={users.data.map((u) => ({ id: String(u.id), name: u.name || u.email, weight: Number.isFinite(u.used) ? u.used / Math.max(1, ...list.map((v) => Number.isFinite(v.used) ? v.used : 0)) : null, tone: !u.enabled ? "off" : needsCare(u) ? "warn" : "ok" }))} />
        </Card>
      )}
      {users.data && users.data.length > 0 && (
        <div className="flex flex-wrap items-center gap-[9px]">
          <Input className="w-[301px] max-w-full" aria-label="搜索邮箱、名称或备注" placeholder="搜索邮箱、名称或备注" value={query} onChange={(e) => setQuery(e.target.value)} />
          <Segmented aria-label="分组" value={groupBy} onValueChange={setGroupBy} options={[["status", "按状态分组"], ["seen", "最近在线"], ["traffic", "流量"]] as const} />
          {groupBy === "seen" && <small className="text-[var(--weir-ink-3)]">最近在线未知，仍按状态排列</small>}
          <span className="flex-1" />
          <BulkBar count={picked.length}>
            <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void bulkExtend(picked, users.data, act)}>续期 30 天</Button>
            <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void bulkDisable(picked, act)}>停用</Button>
          </BulkBar>
        </div>
      )}
      {users.isPending || users.error ? (
        <State
          loading={users.isPending}
          error={users.error}
          onRetry={() => users.refetch()}
          skeleton={<SkeletonTable columns={["用户", "用量", "到期", "节点", "链接 / 链式", <span className="sr-only">操作</span>]} lines={2} />}
        />
      ) : users.data.length === 0 ? (
        <State empty="还没有用户" detail="给朋友开账号时在这里建：一次设好额度、到期和节点，保存后自动生成订阅链接。" />
      ) : (
        <Card className="overflow-hidden p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[41px]"><span className="sr-only">选择</span></TableHead><TableHead>用户</TableHead>
                  <TableHead>本月流量</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>链接</TableHead>
                  <TableHead>到期 / 重置</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead><span className="sr-only">操作</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {[...users.data].filter((u) => `${u.email} ${u.name ?? ""}`.toLowerCase().includes(query.trim().toLowerCase())).sort((a, b) => groupBy === "traffic" ? b.used - a.used : Number(needsCare(b)) - Number(needsCare(a))).map((u, i, list) => (
                  <Fragment key={u.id}>
                  {groupBy === "status" && (i === 0 || needsCare(list[i - 1]) !== needsCare(u)) && <TableRow data-group-row=""><TableCell colSpan={8}>{needsCare(u) ? `需要处理 · ${list.filter(needsCare).length}` : <>正常 · {list.filter((item) => !needsCare(item)).length} <button type="button" className="ml-[9px] font-semibold text-[var(--weir-accent-text)] active:opacity-60 touch:min-h-11 touch:min-w-11" onClick={() => setShowNormal((v) => !v)}>{showNormal ? "收起" : "展开"}</button></>}</TableCell></TableRow>}
                  {(!needsCare(u) && !showNormal) ? null : <TableRow data-state={picked.includes(u.id) ? "selected" : undefined} {...dataObject({ type: "user", id: u.id })}>
                    <TableCell><Checkbox aria-label={"选择 " + u.email} checked={picked.includes(u.id)} onCheckedChange={(on) => setPicked((ids) => on ? [...ids, u.id] : ids.filter((id) => id !== u.id))} /></TableCell>
                    <TableCell>
                      <span className="flex items-center gap-[9px]">
                        <Avatar name={u.name || u.email} />
                        <span className="min-w-0">
                          <button type="button" className="block max-w-full truncate text-left font-bold text-[var(--weir-ink)] active:opacity-60 touch:min-h-11" onClick={() => navigate(`/users/${u.id}`)}>{u.name || u.email.split("@")[0]}</button>
                          <small className="block truncate text-fine text-[var(--weir-ink-3)]">{u.email}{u.role === "admin" ? " · 管理员" : ""}</small>
                        </span>
                      </span>
                    </TableCell>
                    <TableCell className="min-w-40">
                      <TrafficUsage used={u.used} quota={u.quota} unlimited={u.unlimited} />
                    </TableCell>
                    <TableCell className="font-[family-name:var(--weir-mono)]">{u.all_nodes ? "全部" : u.node_ids?.length ?? 0}</TableCell>
                    <TableCell className="font-[family-name:var(--weir-mono)]">{u.links}</TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className={careLabel(u)?.includes("到期") ? "font-[family-name:var(--weir-mono)] text-[var(--weir-warn-ink)]" : "font-[family-name:var(--weir-mono)]"}>{u.expires_at ? date(u.expires_at) : "长期"}</span>
                      <span className="ml-[5px] text-meta text-[var(--weir-ink-3)]">{u.expires_at ? "到期" : u.monthly_reset ? "每月重置" : ""}</span>
                    </TableCell>
                    <TableCell>{careLabel(u) ? <Badge variant="soft" tone={u.enabled ? "warn" : "neutral"}>{careLabel(u)}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">正常</span>}</TableCell>
                    <TableCell>
                      <span className="flex flex-nowrap justify-end gap-1.5">
                        <Button size="sm" variant="ghost" onClick={() => setForm(u)}>编辑</Button>
                        <Button size="icon-sm" variant="outline" aria-label={"清零用量 " + u.email} disabled={isBusy()} busy={isBusy("reset:" + u.id)} busyText="正在清零" onClick={async () => (await confirm({ title: `清零 ${u.email} 本周期用量？`, confirmText: "清零", tone: "danger" })) && act("reset:" + u.id, () => api(`/admin/users/${u.id}/reset-usage`, "POST"), "用量已清零")}><RotateCcw /></Button>
                        <Button size="icon-sm" variant="destructive" aria-label={"删除 " + u.email} disabled={isBusy()} busy={isBusy("del:" + u.id)} busyText="正在删除" onClick={async () => (await confirm({ title: `删除用户 ${u.email}？`, description: "其所有链接立即失效。", confirmText: "删除", tone: "danger" })) && act("del:" + u.id, () => api("/admin/users/" + u.id, "DELETE"), "用户已删除")}><Trash2 /></Button>
                      </span>
                    </TableCell>
                  </TableRow>}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
        </Card>
      )}
      <UserForm
        value={form}
        nodes={nodes.data ?? []}
        close={() => setForm(null)}
        saved={(u) => {
          // 新建后直接打开详情：订阅链接与二维码就在那里，一次性密码也显示在顶部。
          void users.refetch();
          if (form === "new") {
            setCreated(u);
            navigate(`/users/${u.id}?tab=links`);
          }
          setForm(null);
          toast("已保存", { tone: "ok" });
        }}
      />
      <Sheet
        open={!!detail}
        onOpenChange={(o) => {
          if (!o) {
            detailParam.close();
            setCreated(null);
          }
        }}
      >
        {detail && (
          <SheetContent side="right" title={detail.name || detail.email} description={`${detail.email} · ${bytes(detail.used)} / ${detail.unlimited ? "不限" : bytes(detail.quota)}`} className="w-full sm:max-w-[640px]">
            <div className="stack p-4">
              {created?.id === detail.id && (
                <Notice role="status" tone="ok">
                  已创建 {created.email}，默认链接已生成，导入下面的地址或扫描二维码即可使用。
                  {created.password && (
                    <>
                      {" "}初始密码只显示这一次：<code className="font-mono">{created.password}</code> <CopyButton value={created.password}>复制密码</CopyButton>
                    </>
                  )}
                </Notice>
              )}
              <WithheldNotice userId={detail.id} />
              <SectionHeader title="流量" description="最近 1 / 6 / 24 小时，按 VPS 上报的计数；缺失的时段显示为未知。" />
              <HistoryCharts path={`/admin/users/${detail.id}/history`} />
              <LinkPanel base={`/admin/users/${detail.id}`} planEnds={detail.expires_at} showCredentials inlineQr={created?.id === detail.id} />
              <ChainPanel base={`/admin/users/${detail.id}`} admin />
            </div>
          </SheetContent>
        )}
      </Sheet>
    </div>
  );
}

// WithheldNotice：因额度没发给这个用户的节点（WireGuard 无法按用户计量，只发给不限额度的用户）。
function WithheldNotice({ userId }: { userId: number }) {
  const q = useQuery({ queryKey: ["user", userId], queryFn: () => api(`/admin/users/${userId}`) as Promise<{ withheld?: Withheld[] }> });
  // 读取失败不能当成「都发了」：说明原因并可重新读取
  if (q.error) return <Notice tone="bad">读不到这个用户有哪些节点没有发给他：{clause(q.error)}。<Button size="xs" variant="ghost" disabled={q.isFetching} busy={q.isFetching} busyText="正在读取…" onClick={() => void q.refetch()}>重新加载</Button></Notice>;
  const list = q.data?.withheld ?? [];
  if (list.length === 0) return null;
  return <Notice tone="warn">这些节点没有发给该用户：{list.map((w) => w.name).join("、")}。{list[0].reason}。</Notice>;
}
