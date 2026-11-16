// 原型 · 代理：代理归代理。系统的（管理员给节点和规则用）与各用户的（只给他的链式节点用）在一处，按归属分组；
// 同一地址合并成一行并写明挂在哪几台 VPS。凭据加密存储：用户只看到地址与用户名；管理员可切明文 / 密文，切明文记审计。
import { useState } from "react";
import { Plus } from "lucide-react";
import { Badge, Button, Card, PageHeader, Segmented, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast } from "@weir/ui";
import { proxies } from "./data";
import { AddProxy, badgeTone, UdpBadge, useRole } from "./parts";

export function ProtoProxies() {
  const toast = useToast();
  const admin = useRole() === "admin";
  const [plain, setPlain] = useState("cipher");
  const [owner, setOwner] = useState("all");
  const [add, setAdd] = useState(false);
  const list = admin ? proxies : proxies.filter((p) => p.owner === "Tulip");
  const owners = [...new Set(list.map((p) => p.owner ?? "系统"))];
  const shown = owners.filter((o) => owner === "all" || o === owner);
  return (
    <div className="stack">
      <PageHeader eyebrow={admin ? "线路" : undefined} title={admin ? "代理" : "我的代理"} description={admin ? "系统代理给节点和规则用；用户的代理只给他自己的链式节点用。凭据加密存储。" : "你自己的代理。保存前由 VPS 实测，凭据加密存储；用它在「我的节点」里新建节点。"}
        actions={<Button onClick={() => setAdd(true)}><Plus />添加代理</Button>} />
      {admin && (
        <div className="actions">
          <Segmented aria-label="归属" value={owner} onValueChange={setOwner} options={[["all", `全部 ${proxies.length}`], ...owners.map((o) => [o, o] as [string, string])]} />
          <span className="ml-auto" />
          <Segmented aria-label="凭据显示" value={plain} onValueChange={(v) => { setPlain(v); if (v === "plain") toast("已切到明文，已记入操作审计（原型）", { tone: "warn" }); }} options={[["cipher", "密文"], ["plain", "明文"]]} />
        </div>
      )}
      {shown.map((o) => (
        <Card key={o} className="overflow-hidden p-0">
          <div className="px-[var(--weir-space-4)] pt-[var(--weir-space-3)]"><small className="text-[var(--weir-ink-3)]">{o === "系统" ? "系统代理" : `${o} 的代理`} · {list.filter((p) => (p.owner ?? "系统") === o).length}</small></div>
          <Table>
            <TableHeader><TableRow><TableHead>代理</TableHead><TableHead>地址</TableHead><TableHead>凭据</TableHead><TableHead>UDP</TableHead><TableHead>VPS 实测</TableHead><TableHead>挂在</TableHead><TableHead>被谁用</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
            <TableBody>
              {list.filter((p) => (p.owner ?? "系统") === o).map((p) => (
                <TableRow key={p.id}>
                  <TableCell className="whitespace-nowrap"><span className="flex flex-nowrap items-center gap-[var(--weir-space-2)]"><strong>{p.name}</strong><Badge variant="outline">{p.kind}</Badge></span>{p.dupOf && <small className="block text-[var(--weir-warn-ink)]">与「{p.dupOf}」是同一个代理</small>}</TableCell>
                  <TableCell><small>{p.address}</small></TableCell>
                  <TableCell><small className="font-[family-name:var(--weir-mono)]">{p.username ? `${p.username} / ` : ""}{admin ? (plain === "plain" ? p.secret : p.cipher) : "已加密"}</small></TableCell>
                  <TableCell className="whitespace-nowrap"><UdpBadge udp={p.udp} /></TableCell>
                  <TableCell className="whitespace-nowrap"><Badge variant="soft" tone={badgeTone(p.probeTone)}>{p.probe}</Badge></TableCell>
                  <TableCell><small>{p.gates.join("、")}</small></TableCell>
                  <TableCell><small>{p.usedBy.join("、") || "没有在用"}</small></TableCell>
                  <TableCell><div className="flex flex-nowrap items-center justify-end gap-[var(--weir-space-2)]">
                    {p.dupOf && <Button size="sm" onClick={() => toast(`合并到「${p.dupOf}」，引用一起改（原型）`)}>合并</Button>}
                    <Button size="sm" variant="ghost" onClick={() => toast("VPS 实测中（原型）")}>实测</Button>
                    <Button size="sm" variant="ghost" onClick={() => toast("编辑代理（原型）")}>编辑</Button>
                  </div></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      ))}
      <AddProxy open={add} close={() => setAdd(false)} admin={admin} />
    </div>
  );
}
