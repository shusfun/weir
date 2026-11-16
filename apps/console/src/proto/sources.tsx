// 原型 · 订阅源：来源只管来源（刷新、余量、异常时保留旧节点），挑节点后直接进统一节点库并分给用户。
import { useState } from "react";
import { Plus } from "lucide-react";
import {
  Badge, Button, Card, CardContent, CardHeader, Chip, PageHeader, PipelineBoard, Segmented,
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast,
} from "@weir/ui";
import { sourceNodes, sources } from "./data";
import { badgeTone, ServiceCard } from "./parts";


export function ProtoSources() {
  const toast = useToast();
  const [picked, setPicked] = useState(sourceNodes.filter((n) => n.picked).map((n) => n.id));
  const [region, setRegion] = useState("all");
  const toggle = (id: string) => setPicked(picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id]);
  return (
    <div className="stack">
      <PageHeader eyebrow="订阅" title="订阅源" description="粘贴机场订阅地址，拉取后在这里挑节点；选用的节点进「节点 › 全部节点」，可以分给用户或作为落地。"
        actions={<Button onClick={() => toast("粘贴订阅地址（原型）")}><Plus />添加来源</Button>} />
      <ServiceCard items={[{ tone: "ok", label: "每 6 小时自动刷新", meta: "上次 14:00" }, { tone: "warn", label: "1 个来源异常", meta: "保留旧节点" }]}
        action={<Button size="sm" variant="outline" onClick={() => toast("全部刷新中（原型）")}>全部刷新</Button>} />
      <PipelineBoard label="订阅流水线" steps={[{ name: "来源", value: 2 }, { name: "拉到节点", value: 50 }, { name: "选用", value: 2 }, { name: "订阅组", value: 1 }, { name: "分给用户", value: 2 }]} />
      <Card className="overflow-hidden p-0">
        <Table>
          <TableHeader><TableRow><TableHead>来源</TableHead><TableHead>节点</TableHead><TableHead>剩余 / 到期</TableHead><TableHead>上次刷新</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>
            {sources.map((s) => (
              <TableRow key={s.id}>
                <TableCell><div className="stack-tight"><strong>{s.name}</strong><small className="text-[var(--weir-ink-3)]">{s.url}</small></div></TableCell>
                <TableCell>{s.total} 个 · 选用 {s.picked}</TableCell>
                <TableCell><small>{s.left}</small></TableCell>
                <TableCell><small>{s.at}</small></TableCell>
                <TableCell><div className="stack-tight"><Badge variant="soft" tone={badgeTone(s.verdict.tone)}>{s.verdict.text}</Badge>{s.verdict.why && <small className="text-[var(--weir-ink-3)]">{s.verdict.why}</small>}</div></TableCell>
                <TableCell><div className="actions justify-end">
                  {s.verdict.tone === "warn" && <Button size="sm" onClick={() => toast("已接受空结果（原型）")}>接受空结果</Button>}
                  <Button size="sm" variant="ghost" onClick={() => toast("刷新中（原型）")}>刷新</Button>
                  <Button size="sm" variant="ghost" onClick={() => toast(s.verdict.tone === "warn" ? "上游返回的原文（原型）" : "定位到下方挑节点（原型）")}>{s.verdict.tone === "warn" ? "看上游返回" : "挑节点"}</Button>
                </div></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
      <Card>
        <CardHeader title="示例机场 · 挑节点" description="点一下选中或取消；选用后进统一节点库" actions={<Segmented aria-label="地区" value={region} onValueChange={setRegion} options={[["all", "全部 38"], ["hk", "香港 9"], ["jp", "日本 8"], ["us", "美国 7"]]} />} />
        <CardContent className="stack">
          <div className="actions">
            {sourceNodes.map((n) => <Chip key={n.id} tone={n.tone} meta={`${n.proto} · ${n.ms}`} selected={picked.includes(n.id)} onClick={() => toggle(n.id)}>{n.name}</Chip>)}
          </div>
          <div className="actions">
            <small className="text-[var(--weir-ink-2)]">已选 {picked.length} 个</small>
            <span className="ml-auto" />
            <Button variant="outline" onClick={() => toast("实测这一页（原型）")}>实测这一页</Button>
            <Button onClick={() => toast(`选用 ${picked.length} 个，接着选给哪些用户（原型）`, { tone: "ok" })}>选用并分给用户…</Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

