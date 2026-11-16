// 原型 · 我的订阅：每个人只管自己的订阅。从「我的节点」（分到的 + 自己用代理建的链式节点）里挑，组装成订阅链接。
// 管理员看别人的订阅在「用户」里打开那个人；代理在「代理」；链式节点在「节点」。
import { useState } from "react";
import { Plus } from "lucide-react";
import { Button, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Field, InsetGroup, Input, Notice, PageHeader, Switch, Ticket, useToast } from "@weir/ui";
import { myLinks, nodes } from "./data";
import { lampTone, nodeMeta, nodeTitle, useRole } from "./parts";

const days = Array.from({ length: 21 }, (_, i) => (i % 9 === 4 ? "warn" : i < 3 ? "none" : "ok")) as ("ok" | "warn" | "none")[];

function BuildDialog({ open, close, owner }: { open: boolean; close: () => void; owner: string }) {
  const toast = useToast();
  const mineNodes = owner === "Tulip" ? nodes.filter((n) => [1, 2, 3, 20].includes(n.id) || (n.source.kind === "chain" && n.source.owner === "Tulip")) : nodes.filter((n) => n.source.kind !== "chain");
  const [picked, setPicked] = useState<number[]>(mineNodes.slice(0, 2).map((n) => n.id));
  const toggle = (id: number) => setPicked(picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id]);
  const groups: [string, typeof mineNodes][] = [["分给我的", mineNodes.filter((n) => n.source.kind !== "chain")], ["我的链式节点", mineNodes.filter((n) => n.source.kind === "chain")]];
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogHeader><DialogTitle>新建订阅</DialogTitle><DialogDescription>挑要放进这条订阅的节点，例如给某台设备单独一条。</DialogDescription></DialogHeader>
        <div className="stack">
          <Field label="名称" name="name"><Input defaultValue="平板" /></Field>
          {groups.filter(([, list]) => list.length).map(([title, list]) => (
            <InsetGroup key={title} title={title} description="状态、端口与 UDP 能力都在标签上">
              <div className="actions">{list.map((n) => <Chip key={n.id} tone={lampTone(n.verdict.tone)} meta={nodeMeta(n)} selected={picked.includes(n.id)} onClick={() => toggle(n.id)}>{nodeTitle(n)}</Chip>)}</div>
            </InsetGroup>
          ))}
          <Field label="有效期" name="window" hint="不填即一直可用；不能晚于你的套餐到期日"><Input placeholder="一直可用" /></Field>
        </div>
        <DialogFooter><Button variant="ghost" onClick={close}>取消</Button><Button onClick={() => { toast(`已生成订阅，含 ${picked.length} 个节点（原型）`, { tone: "ok", description: "地址与二维码在票据上，可以直接导入客户端。" }); close(); }}>生成订阅（{picked.length} 个节点）</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ProtoSubs() {
  const toast = useToast();
  const owner = useRole() === "admin" ? "shus808" : "Tulip";
  const links = myLinks.filter((l) => l.owner === owner);
  const [on, setOn] = useState<Record<string, boolean>>(Object.fromEntries(myLinks.map((l) => [l.id, l.on])));
  const [build, setBuild] = useState(false);
  return (
    <div className="stack">
      <PageHeader title="我的订阅" description="从「我的节点」里挑，组装成订阅链接；导入客户端就能用。" actions={<Button onClick={() => setBuild(true)}><Plus />新建订阅</Button>} />
      <div className="weir-ticket-grid">
        {links.map((l) => (
          <Ticket key={l.id} name={l.name} meta={`${l.window} · ${l.nodes.length} 个节点`} url={`https://example.invalid/sub/${l.id}••••`} last={l.last} tone={on[l.id] ? "ok" : "unknown"} days={days} note={`最近 21 天的拉取 · 24 小时 ${l.pulls} 次`}
            actions={<><Switch aria-label={`${l.name} 启用`} checked={on[l.id]} onCheckedChange={(c) => { setOn({ ...on, [l.id]: c }); toast(c ? "已启用（原型）" : "已停用，客户端下次更新拿不到节点（原型）"); }} /><Button size="sm" variant="ghost" onClick={() => toast("打开一键导入页（原型）")}>导入</Button><Button size="sm" variant="ghost" onClick={() => setBuild(true)}>改节点</Button><Button size="sm" variant="ghost" onClick={() => toast("拉取记录（原型）")}>记录</Button></>} />
        ))}
      </div>
      <BuildDialog open={build} close={() => setBuild(false)} owner={owner} />
    </div>
  );
}

export function ProtoStub({ eyebrow, title, note }: { eyebrow?: string; title: string; note?: string }) {
  return (
    <div className="stack">
      <PageHeader eyebrow={eyebrow} title={title} description={note ?? "沿用现有页面，只挪到新的导航分组下。"} />
      <Notice tone="info">原型不重画这一页。</Notice>
    </div>
  );
}
