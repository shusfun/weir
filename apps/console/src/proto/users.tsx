// 原型 · 用户模块：用户（点阵 + 默认展开的列表 + 弹窗，详情即编辑）/ 套餐与用码。订阅链接归订阅模块。
import { useState } from "react";
import { Plus } from "lucide-react";
import {
  Avatar, Badge, BulkBar, Button, Card, CardContent, Checkbox, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DotCrowd, HoldButton,
  InsetGroup, InsetRow, InsetRows, Meter, Notice, PageHeader, Readout, Segmented, SwitchField, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast,
} from "@weir/ui";
import { myLinks, nodes, proxies, userOps, users } from "./data";
import { lampTone, nodeMeta, nodeTitle } from "./parts";

const usage = [{ name: "方微云 VLESS :43512", gb: 5.1, p90: "12.6 MB/s" }, { name: "方微云 VLESS → 家宽", gb: 2.2, p90: "4.6 MB/s" }, { name: "香港 01", gb: 1.0, p90: "6.4 MB/s" }, { name: "香港 01 → 公司代理", gb: 0.3, p90: "3.0 MB/s" }];


type User = (typeof users)[number];

function UserDialog({ user, close }: { user: User | null; close: () => void }) {
  const toast = useToast();
  const [enabled, setEnabled] = useState(true);
  const [all, setAll] = useState(false);
  const [picked, setPicked] = useState<number[]>([1, 20]);
  const toggle = (id: number) => setPicked(picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id]);
  return (
    <Dialog open={!!user} onOpenChange={(o) => !o && close()}>
      {user && (
        <DialogContent size="lg">
          <DialogHeader>
            <DialogTitle>{user.name}</DialogTitle>
            <DialogDescription>{`${user.email} · 最近在线 16:02 · 2 个会话`}</DialogDescription>
          </DialogHeader>
          <div className="stack">
            <InsetGroup title="账户">
              <InsetRows>
                <InsetRow><SwitchField placement="end" label="启用" checked={enabled} onCheckedChange={(c) => { setEnabled(c); toast(c ? "已启用（原型）" : "已停用（原型）"); }} /></InsetRow>
                <InsetRow className="actions justify-between"><Readout label="额度" value={user.quota == null ? "不限" : `${user.used} / ${user.quota}`} unit={user.quota == null ? undefined : "GiB"} caption="每月 1 日重置" /><Button size="sm" variant="outline" onClick={() => toast("修改额度（原型）")}>修改</Button></InsetRow>
                <InsetRow className="actions justify-between"><Readout label="到期" value="2026-12-31" caption="47 天后 · 续期会顺延没单独设期限的链接" /><Button size="sm" variant="outline" onClick={() => toast("已续期 30 天（原型）", { tone: "ok" })}>续期 30 天</Button></InsetRow>
              </InsetRows>
            </InsetGroup>
            <InsetGroup title={`能用的节点 · ${all ? "全部" : picked.length + " 个"}`} description="点一下选中或取消，用户下次更新订阅时拿到" actions={<SwitchField label="全部节点" checked={all} onCheckedChange={setAll} />}>
              <div className="actions">
                {nodes.filter((n) => n.source.kind !== "chain").map((n) => (
                  <Chip key={n.id} tone={lampTone(n.verdict.tone)} meta={nodeMeta(n)} selected={all || picked.includes(n.id)} disabled={all} onClick={() => toggle(n.id)}>{nodeTitle(n)}</Chip>
                ))}
              </div>
            </InsetGroup>
            <InsetGroup title="节点用量 · 本月" description="他用得最多的节点；按凭据统计，链式节点单独算">
              <InsetRows>
                {usage.map((u) => (
                  <InsetRow key={u.name} className="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto] items-center gap-[var(--weir-space-3)]">
                    <small>{u.name}</small><Meter value={u.gb} max={usage[0].gb} label={`${u.gb} GB`} /><small className="text-[var(--weir-ink-2)]">P90 {u.p90}</small>
                  </InsetRow>
                ))}
              </InsetRows>
            </InsetGroup>
            <InsetGroup title="订阅与代理" description="他自己建的，只读查看">
              <InsetRows>
                {myLinks.filter((l) => l.owner === user.name).map((l) => <InsetRow key={l.id} className="actions justify-between"><span>订阅「{l.name}」· {l.nodes.length} 个节点</span><small className="text-[var(--weir-ink-2)]">最近拉取 {l.last} · 24 小时 {l.pulls} 次</small></InsetRow>)}
                {proxies.filter((p) => p.owner === user.name).map((p) => <InsetRow key={p.id} className="actions justify-between"><span>代理「{p.name}」· {p.kind} {p.address}</span><small className="text-[var(--weir-ink-2)]">{p.probe}</small></InsetRow>)}
              </InsetRows>
            </InsetGroup>
            <InsetGroup title="操作记录" description="来自操作审计，按这个用户筛出" actions={<Button size="sm" variant="ghost" onClick={() => toast("打开操作审计并按用户筛选（原型）")}>全部记录</Button>}>
              <InsetRows>
                {userOps.map((o) => <InsetRow key={o.at + o.what} className="grid grid-cols-[81px_minmax(0,1fr)_auto] gap-[var(--weir-space-3)]"><small className="text-[var(--weir-ink-3)]">{o.at}</small><span>{o.what}</span><small className="text-[var(--weir-ink-3)]">{o.where}</small></InsetRow>)}
              </InsetRows>
            </InsetGroup>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => toast("已复制订阅地址（原型）")}>复制订阅地址</Button>
            <Button variant="outline" onClick={() => toast("已踢下线（原型）")}>踢下线</Button>
            <HoldButton variant="destructive" onHold={() => { toast("用户已删除（原型）", { tone: "warn" }); close(); }}>按住删除</HoldButton>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}

export function ProtoUsers() {
  const toast = useToast();
  const [sel, setSel] = useState<number[]>([]);
  const [open, setOpen] = useState<User | null>(null);
  const [filter, setFilter] = useState("all");
  return (
    <div className="stack">
      <PageHeader eyebrow="用户" title="用户" description="5 个用户 · 5 个启用 · 本月共 23.06 GiB" actions={<Button onClick={() => toast("新建用户：流量额度必填，没有默认 0（原型）")}><Plus />新建用户</Button>} />
      <Card size="sm">
        <CardContent>
          <DotCrowd label="全部用户 · 点越大本月流量越多" members={users.map((u) => ({ id: String(u.id), name: u.name, weight: u.quota ? Math.min(1, u.used / u.quota) : 0.8, tone: "info" as const }))} onSelect={(id) => setOpen(users.find((u) => String(u.id) === id) ?? null)} />
        </CardContent>
      </Card>
      <div className="actions">
        <Segmented aria-label="筛选" value={filter} onValueChange={setFilter} options={[["all", "全部 5"], ["attention", "需要处理 0"], ["off", "已停用 0"]]} />
        <BulkBar count={sel.length}><Button size="sm" variant="outline" onClick={() => toast(`${sel.length} 人续期 30 天；没有到期日的会说明跳过（原型）`)}>续期 30 天</Button><Button size="sm" variant="ghost" onClick={() => setSel([])}>清除选择</Button></BulkBar>
      </div>
      <Card className="overflow-hidden p-0">
        <Table>
          <TableHeader><TableRow><TableHead className="w-11"><span className="sr-only">选择</span></TableHead><TableHead>用户</TableHead><TableHead>本月流量</TableHead><TableHead>节点</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>
            {users.map((u) => (
              <TableRow key={u.id}>
                <TableCell><Checkbox aria-label={`选择 ${u.name}`} checked={sel.includes(u.id)} onCheckedChange={(c) => setSel(c ? [...sel, u.id] : sel.filter((x) => x !== u.id))} /></TableCell>
                <TableCell><span className="actions"><Avatar name={u.name} /><span className="stack-tight"><strong>{u.name}</strong><small className="text-[var(--weir-ink-3)]">{u.email}</small></span></span></TableCell>
                <TableCell>{u.used} {u.quota == null ? "GiB · 不限" : `/ ${u.quota} GiB`}</TableCell>
                <TableCell>{u.nodes}</TableCell>
                <TableCell><Badge variant="status" tone="ok">正常</Badge></TableCell>
                <TableCell><div className="actions justify-end"><Button size="sm" variant="ghost" onClick={() => toast("已复制订阅地址（原型）")}>复制订阅</Button><Button size="sm" variant="outline" onClick={() => setOpen(u)}>打开</Button></div></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
      <UserDialog user={open} close={() => setOpen(null)} />
    </div>
  );
}

export function ProtoPlans() {
  return (
    <div className="stack">
      <PageHeader eyebrow="用户" title="套餐与用码" description="沿用现有页面，只挪到用户模块下；测试残留（QA-DEEP-1113）在概览里提示清理。" />
      <Notice tone="info">原型不重画这一页。</Notice>
    </div>
  );
}
