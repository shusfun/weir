// 原型 · 概览：首页就是待处理清单，每条带原因和处理按钮；旁边是浏览器测的「你 → VPS」和后台状态。
import { useState } from "react";
import { Badge, Button, Card, CardContent, CardHeader, InsetGroup, InsetRow, InsetRows, MetricStrip, PageHeader, Readout, StatusLamp, useToast } from "@weir/ui";
import { gates, nodes, todos } from "./data";
import { useNavigate } from "react-router-dom";
import { base, badgeTone, NodeDialog } from "./parts";

export function ProtoOverview() {
  const toast = useToast();
  const navigate = useNavigate();
  const [done, setDone] = useState<string[]>([]);
  const [open, setOpen] = useState<number | null>(null);
  const left = todos.filter((t) => !done.includes(t.id));
  return (
    <div className="stack">
      <PageHeader size="hero" eyebrow="概览" title={left.length ? `${left.length} 件事要处理` : "没有要处理的事"} description="2 台 VPS 在线 · 8 / 12 个节点可用 · 5 个用户本月 31.2 GiB" />
      <MetricStrip items={[{ label: "VPS 在线", value: "2 / 2" }, { label: "可用节点", value: "8 / 12" }, { label: "方微云流量", value: "81", unit: "%", caption: "12 天后重置" }, { label: "国内误判走代理", value: "17.9", unit: "GB", caption: "7 天 · 可省" }]} />
      <div className="grid gap-[var(--weir-space-5)] lg:grid-cols-[minmax(0,1fr)_minmax(0,361px)]">
        <InsetGroup title="待处理" description="按影响排序；处理完自动消失">
          <InsetRows>
            {left.length === 0 && <InsetRow><small className="text-[var(--weir-ink-3)]">都处理完了。</small></InsetRow>}
            {left.map((t) => (
              <InsetRow key={t.id} className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-[var(--weir-space-3)]">
                <StatusLamp tone={t.tone} />
                <div className="stack-tight">
                  <span className="actions"><strong>{t.title}</strong><Badge variant="soft" tone={badgeTone(t.tone)}>{t.tag}</Badge></span>
                  <small className="text-[var(--weir-ink-2)]">{t.why}</small>
                </div>
                <div className="actions">
                  <Button size="sm" onClick={() => { if (t.href) { navigate(base + t.href); return; } setDone([...done, t.id]); toast(`${t.action}（原型）`, { tone: "ok", description: "完成后这一条会自动消失。" }); }}>{t.action}</Button>
                  {t.node && <Button size="sm" variant="ghost" onClick={() => setOpen(t.node!)}>查看</Button>}
                </div>
              </InsetRow>
            ))}
          </InsetRows>
        </InsetGroup>
        <div className="stack">
          <Card size="sm">
            <CardHeader title="你 → VPS" description="浏览器实测 · 16:41" actions={<Button size="sm" variant="ghost" onClick={() => toast("正在从你的浏览器重测（原型）")}>重测</Button>} />
            <CardContent className="grid grid-cols-2 gap-[var(--weir-space-3)]">
              {gates.map((g) => <Readout key={g.id} label={g.name} value={g.browserMs} unit="ms" caption={g.browserUdp} />)}
            </CardContent>
          </Card>
          <Card size="sm">
            <CardHeader title="Laya" description="方微云还没装 Laya，安全事件不会自动收紧。" actions={<Badge variant="soft">未部署</Badge>} />
            <CardContent><Button size="sm" variant="outline" onClick={() => toast("开始部署 Laya 到方微云（原型）")}>部署到方微云</Button></CardContent>
          </Card>
          <Card size="sm">
            <CardHeader title="dsh" description="有新版本 0.9.4，更新时 AI 助手停用约 20 秒。" actions={<Badge variant="soft" tone="ok">运行中</Badge>} />
            <CardContent><Button size="sm" variant="outline" onClick={() => toast("dsh 更新中（原型）")}>更新到 0.9.4</Button></CardContent>
          </Card>
        </div>
      </div>
      <NodeDialog node={nodes.find((n) => n.id === open) ?? null} close={() => setOpen(null)} />
    </div>
  );
}
