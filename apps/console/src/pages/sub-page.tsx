// 浏览器打开订阅地址时的一键导入页：不需要登录，只显示本链接的用量、到期与节点名称（不含凭据）。
import { useQuery } from "../lib/query";
import { useParams } from "react-router-dom";
import { Badge, Card, CardContent, CopyField, Meter, PageHeader, Readout } from "@weir/ui";
import { QRCodeSVG } from "qrcode.react";
import { api } from "../api";
import { SubscriptionGuide } from "../components/subscription-guide";
import { importClients, withTarget } from "../lib/import-clients";
import { State } from "../components/feedback";
import { bytes, date } from "../lib/models";
import { protocolNames } from "../lib/core";

interface Info {
  name: string;
  quota: number;
  unlimited: boolean;
  used: number;
  expires_at?: string | null;
  next_reset?: string | null;
  url: string;
  nodes: { name: string; protocol: string; chain: boolean; available: boolean }[];
}

export function SubPage() {
  const { token = "" } = useParams();
  const info = useQuery({ queryKey: ["sub-info", token], queryFn: () => api("/sub-info/" + token) as Promise<Info>, retry: false });
  if (info.isPending || info.error)
    return (
      <main className="auth">
        <State loading={info.isPending} error={info.error} onRetry={() => info.refetch()} />
      </main>
    );
  const d = info.data;
  const title = "Weir" + (d.name ? " · " + d.name : "");
  return (
    <main className="subscription-import-page">
      <PageHeader title={title} description="在下方选择你使用的客户端一键导入；电脑上的客户端也可复制地址手动添加。" />
      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-[1fr_auto]">
          <div className="stack">
            <div className="grid gap-3 sm:grid-cols-3">
              <Readout label="已用" value={bytes(d.used)} caption={d.unlimited ? "不限额度" : `共 ${bytes(d.quota)}`} />
              <Readout label="到期" value={d.expires_at ? date(d.expires_at) : "长期有效"} />
              <Readout label="下次重置" value={d.next_reset ? date(d.next_reset) : "不重置"} />
            </div>
            {!d.unlimited && d.quota > 0 && <Meter label="流量使用" value={d.used} max={d.quota} />}
            <CopyField label="订阅地址" value={d.url} />
          </div>
          <div className="qr-code justify-self-center"><QRCodeSVG value={d.url} size={168} title="订阅二维码" /></div>
        </CardContent>
      </Card>
      <SubscriptionGuide items={[...new Set(importClients.map((c) => c.target))].map((target) => ({ title, target, url: withTarget(d.url, target) }))} />
      <Card>
        <CardContent className="stack">
          <h2 className="text-base font-semibold">节点（{d.nodes.length}）</h2>
          {d.nodes.length === 0 ? (
            <State empty="暂无可用节点" />
          ) : (
            <ul className="grid gap-2 sm:grid-cols-2">
              {d.nodes.map((n, i) => (
                <li key={i} className="flex items-center justify-between gap-2">
                  <span>{n.name}</span>
                  <span className="actions">
                    <Badge variant="soft" tone="neutral">{protocolNames[n.protocol] ?? n.protocol}</Badge>
                    {n.chain && <Badge variant="soft" tone="neutral">链式</Badge>}
                    {!n.available && <Badge variant="soft" tone="warn">暂不可用</Badge>}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </main>
  );
}
