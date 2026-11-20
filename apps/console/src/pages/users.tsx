// 用户：一个窗口建用户与套餐；详情中查看 / 管理其订阅链接（含凭据原文）与自建链式代理。
import { useQueryClient } from "@tanstack/react-query";
import { Fragment, useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { ActionGroup, Avatar, Badge, BulkBar, Button, Card, Checkbox, DotCrowd, TrafficUsage, Input, PageHeader, Segmented, Dialog, DialogContent, DialogTitle, DialogDescription, SkeletonTable, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, CopyButton, Notice, useToast, useConfirm } from "@weir/ui";
import { ChevronRight, Pencil, Plus, RotateCcw, Trash2 } from "lucide-react";
import { api, clause } from "../api";
import { useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { UserForm } from "../components/user-form";
import { UserDetailView } from "../components/user-detail-view";
import { bytes, date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import { ObjectLink, dataObject, useDialogParam } from "../components/object-link";
import { objectHref, type ObjectRef } from "../lib/objects";
import { objectStatus, type NodeItem, type UserItem, type Withheld } from "../lib/core";

// 只有中心给出了非正常结论的用户才算「需要处理」；没有状态的用户归在正常组，状态列写「未知」
function needsCare(u: UserItem) {
  return !!u.object_state && u.object_state.status !== "ok";
}

function careLabel(u: UserItem): string | null {
  return needsCare(u) ? objectStatus(u.object_state)[0] : null;
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

async function bulkDisable(ids: number[], users: UserItem[], confirm: ReturnType<typeof useConfirm>, act: (key: string, run: () => Promise<unknown>, ok: string) => Promise<unknown>) {
  const names = users.filter((u) => ids.includes(u.id)).map((u) => u.name || u.email);
  if (!(await confirm({ title: `停用 ${names.length} 个用户？`, description: `${names.slice(0, 5).join("、")}${names.length > 5 ? ` 等 ${names.length} 人` : ""}。停用后他们的所有链接立即不能使用，可以随时重新启用。`, confirmText: "停用", tone: "danger" }))) return false;
  await act("bulk-disable", async () => {
    for (const id of ids) await api(`/admin/users/${id}`, "PATCH", { enabled: false });
  }, `已停用 ${names.length} 个用户`);
  return true;
}

// 链接列显示链接名称：列表接口只给数量，名称按需从用户详情读取（与详情窗共用缓存）
function LinkNames({ user }: { user: UserItem }) {
  const q = useQuery({ queryKey: ["user", user.id], enabled: user.links > 0, queryFn: () => api(`/admin/users/${user.id}`) as Promise<{ link_items?: { id: number; name: string }[] }> });
  if (!user.links) return <span className="text-meta text-[var(--weir-ink-3)]">没有链接</span>;
  if (q.error) return <span className="text-meta text-[var(--weir-bad-ink)]" title={clause(q.error)}>读取失败</span>;
  if (q.isPending) return <span className="text-meta text-[var(--weir-ink-3)]">正在读取</span>;
  const names = (q.data.link_items ?? []).map((l) => l.name || "未命名链接");
  return <span className="block max-w-[181px] truncate" title={names.join("、")}>{names.slice(0, 2).join("、")}{names.length > 2 ? ` 等 ${names.length} 条` : ""}</span>;
}

export function UsersPage() {
  const client = useQueryClient();
  const confirm = useConfirm();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const [form, setForm] = useState<UserItem | "new" | null>(null);
  const [query, setQuery] = useState("");
  const [groupBy, setGroupBy] = useState("status");
  const [picked, setPicked] = useState<number[]>([]);
  const [showNormal, setShowNormal] = useState(true);
  const [created, setCreated] = useState<UserItem | null>(null);
  const { run: act, isBusy } = useRowAction<string>({ refetch: users.refetch });
  // 地址栏：?user=编号 是这个用户的详情（对象链接、dsh 的界面导航）；?link= / ?chain= 只知道编号时，
  // 先向中心查它属于哪个用户，再打开那个用户并高亮这一项。
  const detailParam = useDialogParam("user", users.data, users.isSuccess);
  const detail = detailParam.current;
  const [params] = useSearchParams();
  const navigate = useNavigate();
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
  const careCount = list.filter(needsCare).length;
  const total = list.reduce((sum, u) => sum + u.used, 0);
  const header = (
    <PageHeader
      title="用户"
      description={users.data ? (
        <span className="weir-sum">
          <span><b>{list.length}</b> 个用户</span>
          <span><b>{list.filter((u) => u.enabled).length}</b> 个启用</span>
          {careCount > 0 && <span className="text-[var(--weir-warn-ink)]">{careCount} 个需要处理</span>}
          <span>本月共 {bytes(total)}</span>
        </span>
      ) : "正在读取用户"}
      actions={<Button onClick={() => setForm("new")}><Plus />新建用户</Button>}
    />
  );
  return (
    <div className="stack">
      {header}
      {users.data && users.data.length > 0 && (
        <Card className="px-[var(--weir-gap-25)] py-[var(--weir-gap-21)]">
          <div className="mb-[var(--weir-gap-13)] flex flex-wrap items-center justify-between gap-3">
            <span className="text-meta text-[var(--weir-ink-2)]">全部用户 · 圆点大小对应本月流量</span>
            <span className="weir-legend"><span><i data-tone="ok" />可使用</span><span><i data-tone="warn" />需要处理</span><span><i data-tone="off" />已停用</span></span>
          </div>
          <DotCrowd label="全部用户 · 点越大本月流量越多" onSelect={(id) => detailParam.open(Number(id))} members={users.data.map((u) => {
            const maxUsed = Math.max(1, ...list.map((v) => Number.isFinite(v.used) ? v.used : 0));
            return { id: String(u.id), name: u.name || u.email, weight: Number.isFinite(u.used) ? Math.max(0.08, u.used / maxUsed) : null, tone: objectStatus(u.object_state)[1] };
          })} />
        </Card>
      )}
      {users.data && users.data.length > 0 && (
        <div className="flex flex-wrap items-center gap-[var(--weir-gap-9)]">
          <Input className="w-[281px] max-w-full" aria-label="搜索邮箱、名称或备注" placeholder="搜索邮箱、名称或备注" value={query} onChange={(e) => setQuery(e.target.value)} />
          <Segmented aria-label="分组与排序" value={groupBy} onValueChange={setGroupBy} options={[["status", "按状态分组"], ["traffic", "按流量排序"], ["all", "全部平铺"]] as const} />
          <span className="flex-1" />
          <BulkBar count={picked.length}>
            <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void bulkExtend(picked, users.data, act)}>续期 30 天</Button>
            <Button size="sm" variant="outline" disabled={isBusy()} busy={isBusy("bulk-disable")} busyText="正在停用…" onClick={() => void bulkDisable(picked, users.data, confirm, act).then((done) => { if (done) setPicked([]); })}>停用</Button>
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
                {(() => {
                  const hideNormal = groupBy === "status" && query.trim() === "" && !showNormal;
                  const filtered = [...users.data].filter((u) => `${u.email} ${u.name ?? ""}`.toLowerCase().includes(query.trim().toLowerCase())).sort((a, b) => groupBy === "traffic" ? b.used - a.used : groupBy === "all" ? a.id - b.id : Number(needsCare(b)) - Number(needsCare(a)));
                  if (filtered.length === 0) return <TableRow><TableCell colSpan={8} className="text-[var(--weir-ink-3)]">没有匹配的用户</TableCell></TableRow>;
                  return filtered.map((u, i, list) => (
                    <Fragment key={u.id}>
                    {groupBy === "status" && (i === 0 || needsCare(list[i - 1]) !== needsCare(u)) && <TableRow data-group-row=""><TableCell colSpan={8}>{needsCare(u) ? `需要处理 · ${list.filter(needsCare).length}` : <button type="button" className="weir-press inline-flex min-h-8 cursor-pointer items-center gap-[var(--weir-gap-9)] border-0 bg-transparent p-0 text-left font-semibold text-inherit touch:min-h-11" aria-expanded={!hideNormal} aria-label={!hideNormal ? "收起正常用户" : "展开正常用户"} onClick={() => setShowNormal((v) => !v)}><span>正常 · {list.filter((item) => !needsCare(item)).length}</span><ChevronRight className={`size-4 transition-[rotate] duration-[var(--weir-spring-dur)] ease-[var(--weir-spring)] ${!hideNormal ? "rotate-90" : "rotate-0"}`} /></button>}</TableCell></TableRow>}
                    {(!needsCare(u) && hideNormal) ? null : <TableRow data-state={picked.includes(u.id) ? "selected" : undefined} {...dataObject({ type: "user", id: u.id })}>
                    <TableCell><Checkbox aria-label={"选择 " + u.email} checked={picked.includes(u.id)} onCheckedChange={(on) => setPicked((ids) => on ? [...ids, u.id] : ids.filter((id) => id !== u.id))} /></TableCell>
                    <TableCell>
                      <span className="flex items-center gap-[var(--weir-gap-9)]">
                        <Avatar name={u.name || u.email} />
                        <span className="min-w-0">
                          <ObjectLink className="inline-block max-w-full text-left font-bold" to={{ type: "user", id: u.id, name: u.name || u.email }}><span className="block truncate">{u.name || u.email.split("@")[0]}</span></ObjectLink>
                          <small className="block truncate text-fine text-[var(--weir-ink-3)]">{u.email}{u.role === "admin" ? " · 管理员" : ""}</small>
                        </span>
                      </span>
                    </TableCell>
                    <TableCell className="min-w-40">
                      <TrafficUsage used={u.used} quota={u.quota} unlimited={u.unlimited} />
                    </TableCell>
                    <TableCell className="font-[family-name:var(--weir-mono)]">{u.all_nodes ? "全部" : u.node_ids?.length ?? 0}</TableCell>
                    <TableCell><LinkNames user={u} /></TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className="font-[family-name:var(--weir-mono)]">{u.expires_at ? date(u.expires_at).slice(0, 10) : "长期"}</span>
                      <span className="ml-[var(--weir-gap-5)] text-meta text-[var(--weir-ink-3)]">{u.expires_at ? "到期" : u.monthly_reset ? "每月重置" : ""}</span>
                    </TableCell>
                    <TableCell>{careLabel(u) ? <Badge variant="status" tone={objectStatus(u.object_state)[1] === "bad" ? "bad" : objectStatus(u.object_state)[1] === "warn" ? "warn" : "neutral"}>{careLabel(u)}</Badge> : <span className="text-meta text-[var(--weir-ink-3)]">{u.object_state ? "可使用" : "未知"}</span>}</TableCell>
                    <TableCell>
                      <ActionGroup align="end" wrap={false}>
                        <Button size="sm" variant="ghost" onClick={() => detailParam.open(u.id)}>详情</Button>
                        <Button size="icon-sm" variant="ghost" title="编辑用户" aria-label={"编辑 " + u.email} onClick={() => setForm(u)}><Pencil /></Button>
                        <Button size="icon-sm" variant="ghost" title="清零本周期用量" aria-label={"清零用量 " + u.email} disabled={isBusy()} busy={isBusy("reset:" + u.id)} onClick={async () => (await confirm({ title: `清零 ${u.email} 本周期用量？`, confirmText: "清零", tone: "danger" })) && act("reset:" + u.id, () => api(`/admin/users/${u.id}/reset-usage`, "POST"), "用量已清零")}><RotateCcw /></Button>
                        <Button size="icon-sm" variant="ghost" title="删除用户" aria-label={"删除 " + u.email} disabled={isBusy()} busy={isBusy("del:" + u.id)} onClick={async () => (await confirm({ title: `删除用户 ${u.email}？`, description: "其所有链接立即失效。", confirmText: "删除", tone: "danger" })) && act("del:" + u.id, () => api("/admin/users/" + u.id, "DELETE"), "用户已删除")}><Trash2 /></Button>
                      </ActionGroup>
                    </TableCell>
                  </TableRow>}
                  </Fragment>
                  ));
                })()}
              </TableBody>
            </Table>
        </Card>
      )}
      <UserForm
        value={form}
        nodes={nodes.data ?? []}
        nodesState={{ loading: nodes.isPending, error: nodes.error }}
        close={() => setForm(null)}
        saved={(u) => {
          // 新建后直接打开详情：订阅链接与二维码就在那里，一次性密码也显示在顶部。
          client.setQueryData<UserItem[]>(["users"], old => old ? [...old.filter(item => item.id !== u.id), u] : [u]);
          void users.refetch();
          if (form === "new") {
            setCreated(u);
            navigate(`/users?user=${u.id}&detail_tab=links`);
          }
          setForm(null);
          toast("已保存", { tone: "ok" });
        }}
      />
      <Dialog
        open={!!detail}
        onOpenChange={(o) => {
          if (!o) {
            detailParam.close();
            setCreated(null);
          }
        }}
      >
        {detail && (
          <DialogContent size="xl">
            <DialogTitle>{detail.name || detail.email}</DialogTitle>
            <DialogDescription className="sr-only">账户、链接、链式代理与流量</DialogDescription>
            <div className="stack">
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
              <UserDetailView key={detail.id} user={detail} users={users.data ?? []} inlineQr={created?.id === detail.id} />
            </div>
          </DialogContent>
        )}
      </Dialog>
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
