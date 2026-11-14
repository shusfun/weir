// 浏览器打开订阅地址时的一键导入页：不需要登录，只显示本链接的用量、到期与节点名称（不含凭据）。
import { useQuery } from "../lib/query";
import { useParams } from "react-router-dom";
import { Badge, Card, CardContent, CopyField, Meter, PageHeader, Readout, SectionHeader } from "@weir/ui";
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
  compatibility: {
    target: string;
    included_nodes: number;
    excluded_nodes: { name: string; protocol?: string; reason: string }[];
    affected_groups: { name: string; removed: boolean; reason: string }[];
    warnings?: { name?: string; reason: string }[];
    affected_rules: { name?: string; reason: string }[];
  }[];
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
  const compatibility = (d.compatibility ?? []).map((report) => ({ ...report,
    excluded_nodes: report.excluded_nodes ?? [], affected_groups: report.affected_groups ?? [], affected_rules: report.affected_rules ?? [], warnings: report.warnings ?? [],
  }));
  const affected = compatibility.filter((report) => report.included_nodes === 0 || report.excluded_nodes.length || report.affected_groups.length || report.affected_rules.length || report.warnings.length);
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
      {affected.length > 0 && (
        <Card>
          <CardContent className="stack">
            <SectionHeader title="客户端兼容情况" description="仅导入目标客户端支持的节点；没有兼容节点的格式暂不可导入。" />
            <ul className="grid gap-3">
              {affected.map((report) => (
                <li key={report.target} className="stack">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="soft" tone="warn">{report.target}</Badge>
                    <span className="text-body">可导入 {report.included_nodes} 个节点，遗漏 {report.excluded_nodes.length} 个</span>
                  </div>
                  <ul className="grid gap-1 text-body text-[var(--weir-ink-2)]">
                    {report.excluded_nodes.map((node, i) => <li key={`${node.name}-${i}`}>{node.name}：{node.reason}</li>)}
                    {report.affected_groups.map((group, i) => <li key={`${group.name}-${i}`}>分组「{group.name}」：{group.reason}</li>)}
                    {report.warnings.map((warning, i) => <li key={`warning-${i}`}>{warning.name}：{warning.reason}</li>)}
                    {report.affected_rules.map((rule, i) => <li key={`${rule.name ?? "rule"}-${i}`}>规则{rule.name ? `「${rule.name}」` : ""}：{rule.reason}</li>)}
                  </ul>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
      <SubscriptionGuide items={[...new Set(importClients.map((c) => c.target))].map((target) => ({ title, target, url: withTarget(d.url, target), unavailableReason: compatibility.find((report) => report.target === target)?.included_nodes === 0 ? "此格式没有兼容节点，请选择其他客户端或联系管理员。" : undefined }))} />
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
