// 用户：一个窗口建用户与套餐；详情中查看 / 管理其订阅链接（含凭据原文）与自建链式代理。
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, Meter, PageHeader, SectionHeader, Sheet, SheetContent, SkeletonTable, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, CopyButton, Notice, useToast, useConfirm } from "@weir/ui";
import { Plus, RotateCcw, Trash2 } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { UserForm } from "../components/user-form";
import { HistoryCharts } from "../components/history-charts";
import { ChainPanel, LinkPanel, type NodeOption } from "../components/link-panel";
import { bytes, date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import type { NodeItem, UserItem, Withheld } from "../lib/core";

export function UsersPage() {
  const confirm = useConfirm();
  const toast = useToast();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const [form, setForm] = useState<UserItem | "new" | null>(null);
  const [detail, setDetail] = useState<UserItem | null>(null);
  const [created, setCreated] = useState<UserItem | null>(null);
  const { run: act, isBusy } = useRowAction<number>({ refetch: users.refetch });
  // dsh 的界面导航（?user=编号）：打开这个用户的详情。
  const [params, setParams] = useSearchParams();
  useEffect(() => {
    const id = Number(params.get("user"));
    const u = id ? users.data?.find((x) => x.id === id) : undefined;
    if (u) {
      setDetail(u);
      params.delete("user");
      setParams(params, { replace: true });
    }
  }, [params, users.data, setParams]);
  useConsoleContext({ page: "用户", list: { selected: users.data?.length ?? 0, items: (users.data ?? []).slice(0, 20).map((u) => ({ type: "user", name: u.name || u.email, id: u.id })) } });
  useConsoleContext(detail ? { object: { type: "user", name: detail.name || detail.email, id: detail.id }, dialog: { title: "用户详情" } } : null);
  const header = <PageHeader eyebrow="工作区" title="用户" description="建用户时一次设好额度、周期与节点；用户可自行生成多条链接与链式代理。" actions={<Button onClick={() => setForm("new")}><Plus />创建用户</Button>} />;
  const optionsFor = (u: UserItem): NodeOption[] =>
    (nodes.data ?? []).filter((n) => n.enabled && (u.all_nodes || (u.node_ids ?? []).includes(n.id))).map((n) => ({ id: n.id, name: n.name, gate_name: n.gate_name }));
  return (
    <div className="stack">
      {header}
      {users.isPending || users.error ? (
        <State
          loading={users.isPending}
          error={users.error}
          onRetry={() => users.refetch()}
          skeleton={<SkeletonTable columns={["用户", "用量", "到期", "节点", "链接 / 链式", <span className="sr-only">操作</span>]} lines={2} />}
        />
      ) : users.data.length === 0 ? (
        <State empty="还没有用户" />
      ) : (
        <Card>
          <CardContent className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>用量</TableHead>
                  <TableHead>到期</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>链接 / 链式</TableHead>
                  <TableHead><span className="sr-only">操作</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.data.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell>
                      <Button variant="link" className="h-auto min-h-0 p-0 text-left text-[var(--mg-ink)]" onClick={() => setDetail(u)}>{u.name || u.email}</Button>
                      <small className="block text-[var(--mg-ink-3)]">{u.email}</small>
                      {u.role === "admin" && <Badge variant="soft" tone="neutral">管理员</Badge>} {!u.enabled && <Badge variant="soft" tone="warn">已停用</Badge>}
                    </TableCell>
                    <TableCell className="min-w-40">
                      <small>{bytes(u.used)} / {u.quota ? bytes(u.quota) : "不限"}{u.monthly_reset ? " · 每月重置" : ""}</small>
                      {u.quota > 0 && <Meter label={u.email + " 流量"} value={u.used} max={u.quota} />}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">{u.expires_at ? date(u.expires_at) : "长期"}</TableCell>
                    <TableCell>{u.all_nodes ? "全部" : `${u.node_ids?.length ?? 0} 个`}</TableCell>
                    <TableCell>{u.links} / {u.chains}{!u.allow_chain && "（链式关闭）"}</TableCell>
                    <TableCell>
                      <span className="flex flex-nowrap gap-1.5">
                        <Button size="sm" variant="outline" onClick={() => setDetail(u)}>详情</Button>
                        <Button size="sm" variant="outline" onClick={() => setForm(u)}>编辑</Button>
                        <Button size="icon-sm" variant="outline" aria-label={"清零用量 " + u.email} disabled={isBusy(u.id)} onClick={async () => (await confirm({ title: `清零 ${u.email} 本周期用量？`, confirmText: "清零", tone: "danger" })) && act(u.id, () => api(`/admin/users/${u.id}/reset-usage`, "POST"), "用量已清零")}><RotateCcw /></Button>
                        <Button size="icon-sm" variant="destructive" aria-label={"删除 " + u.email} disabled={isBusy(u.id)} onClick={async () => (await confirm({ title: `删除用户 ${u.email}？`, description: "其所有链接立即失效。", confirmText: "删除", tone: "danger" })) && act(u.id, () => api("/admin/users/" + u.id, "DELETE"), "用户已删除")}><Trash2 /></Button>
                      </span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      <UserForm
        value={form}
        nodes={nodes.data ?? []}
        close={() => setForm(null)}
        saved={(u) => {
          // 新建后直接打开详情：订阅链接与二维码就在那里，一次性密码也显示在顶部。
          if (form === "new") {
            setCreated(u);
            setDetail(u);
          }
          setForm(null);
          toast("已保存", { tone: "ok" });
          void users.refetch();
        }}
      />
      <Sheet
        open={!!detail}
        onOpenChange={(o) => {
          if (!o) {
            setDetail(null);
            setCreated(null);
          }
        }}
      >
        {detail && (
          <SheetContent side="right" title={detail.name || detail.email} description={`${detail.email} · ${bytes(detail.used)} / ${detail.quota ? bytes(detail.quota) : "不限"}`} className="w-full sm:max-w-[640px]">
            <div className="stack overflow-y-auto p-4">
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
              <LinkPanel base={`/admin/users/${detail.id}`} nodes={optionsFor(detail)} showCredentials inlineQr={created?.id === detail.id} />
              <ChainPanel base={`/admin/users/${detail.id}`} nodes={optionsFor(detail)} admin />
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
  const list = q.data?.withheld ?? [];
  if (list.length === 0) return null;
  return <Notice tone="warn">这些节点没有发给该用户：{list.map((w) => w.name).join("、")}。{list[0].reason}。</Notice>;
}
