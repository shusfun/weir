// 用户：一个窗口建用户与套餐；详情中查看 / 管理其订阅链接（含凭据原文）与自建链式代理。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, Meter, PageHeader, Sheet, SheetContent, SkeletonTable, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, CopyButton, Notice, useToast } from "@weir/ui";
import { Plus, RotateCcw, Trash2 } from "lucide-react";
import { api } from "../api";
import { State } from "../components/feedback";
import { UserForm } from "../components/user-form";
import { ChainPanel, LinkPanel, type NodeOption } from "../components/link-panel";
import { bytes, date } from "../lib/models";
import type { NodeItem, UserItem } from "../lib/core";

export function UsersPage() {
  const toast = useToast();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[] });
  const [form, setForm] = useState<UserItem | "new" | null>(null);
  const [detail, setDetail] = useState<UserItem | null>(null);
  const [created, setCreated] = useState<UserItem | null>(null);
  const [busy, setBusy] = useState(0);
  async function act(id: number, fn: () => Promise<unknown>, ok: string) {
    setBusy(id);
    try {
      await fn();
      toast(ok, { tone: "ok" });
      await users.refetch();
    } catch (e) {
      toast("操作未完成", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
  }
  const header = <PageHeader eyebrow="工作区" title="用户" description="建用户时一次设好额度、周期与节点；用户可自行生成多条链接与链式代理。" actions={<Button onClick={() => setForm("new")}><Plus />创建用户</Button>} />;
  const optionsFor = (u: UserItem): NodeOption[] =>
    (nodes.data ?? []).filter((n) => n.enabled && (u.all_nodes || (u.node_ids ?? []).includes(n.id))).map((n) => ({ id: n.id, name: n.name, gate_name: n.gate_name }));
  return (
    <div className="stack">
      {header}
      {created?.password && (
        <Notice role="status">
          已创建 {created.email}，初始密码只显示这一次：<code className="font-mono">{created.password}</code> <CopyButton value={created.password}>复制密码</CopyButton>
        </Notice>
      )}
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
                      <button type="button" className="text-left underline-offset-4 hover:underline" onClick={() => setDetail(u)}>{u.name || u.email}</button>
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
                        <Button size="icon-sm" variant="outline" aria-label={"清零用量 " + u.email} disabled={busy === u.id} onClick={() => window.confirm(`清零 ${u.email} 本周期用量？`) && act(u.id, () => api(`/admin/users/${u.id}/reset-usage`, "POST"), "用量已清零")}><RotateCcw /></Button>
                        <Button size="icon-sm" variant="destructive" aria-label={"删除 " + u.email} disabled={busy === u.id} onClick={() => window.confirm(`删除用户 ${u.email}？其所有链接立即失效。`) && act(u.id, () => api("/admin/users/" + u.id, "DELETE"), "用户已删除")}><Trash2 /></Button>
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
          if (form === "new") setCreated(u);
          setForm(null);
          toast("已保存", { tone: "ok" });
          void users.refetch();
        }}
      />
      <Sheet open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        {detail && (
          <SheetContent side="right" title={detail.name || detail.email} description={`${detail.email} · ${bytes(detail.used)} / ${detail.quota ? bytes(detail.quota) : "不限"}`} className="w-full sm:max-w-[640px]">
            <div className="stack overflow-y-auto p-4">
              <LinkPanel base={`/admin/users/${detail.id}`} nodes={optionsFor(detail)} showCredentials />
              <ChainPanel base={`/admin/users/${detail.id}`} nodes={optionsFor(detail)} admin />
            </div>
          </SheetContent>
        )}
      </Sheet>
    </div>
  );
}
