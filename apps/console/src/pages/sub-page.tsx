// 浏览器打开订阅地址时的一键导入页：不需要登录，只显示本链接的用量、到期与节点名称（不含凭据）。
import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { Badge, Button, Card, CardContent, CopyButton, Input, Meter, PageHeader, Readout } from "@weir/ui";
import { ArrowUpRight } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { api } from "../api";
import { State } from "../components/feedback";
import { bytes, date } from "../lib/models";
import { protocolNames } from "../lib/core";

interface Info {
  name: string;
  quota: number;
  used: number;
  expires_at?: string | null;
  next_reset?: string | null;
  url: string;
  nodes: { name: string; protocol: string; chain: boolean; available: boolean }[];
}

const enc = encodeURIComponent;
const b64 = (s: string) => btoa(unescape(enc(s)));
const withTarget = (url: string, target: string) => `${url}?target=${target}`;

// 每个客户端的导入地址；target 显式指定格式，不依赖客户端 UA。
export const importClients: { name: string; platforms: string; href: (url: string, name: string) => string }[] = [
  { name: "Clash Verge / Mihomo Party", platforms: "Windows · macOS · Linux", href: (u, n) => `clash://install-config?url=${enc(withTarget(u, "clash"))}&name=${enc(n)}` },
  { name: "FlClash", platforms: "全平台", href: (u, n) => `flclash://install-config?url=${enc(withTarget(u, "clash"))}&name=${enc(n)}` },
  { name: "Clash Meta for Android", platforms: "Android", href: (u, n) => `clashmeta://install-config?url=${enc(withTarget(u, "clash"))}&name=${enc(n)}` },
  { name: "Stash", platforms: "iOS · macOS", href: (u, n) => `stash://install-config?url=${enc(withTarget(u, "clash"))}&name=${enc(n)}` },
  { name: "Surfboard", platforms: "Android", href: (u) => `surfboard:///install-config?url=${enc(withTarget(u, "clash"))}` },
  { name: "sing-box", platforms: "全平台", href: (u, n) => `sing-box://import-remote-profile?url=${enc(withTarget(u, "singbox"))}#${enc(n)}` },
  { name: "Hiddify", platforms: "全平台", href: (u, n) => `hiddify://import/${withTarget(u, "singbox")}#${enc(n)}` },
  { name: "Karing", platforms: "全平台", href: (u, n) => `karing://install-config?url=${enc(withTarget(u, "singbox"))}&name=${enc(n)}` },
  { name: "Shadowrocket", platforms: "iOS", href: (u, n) => `shadowrocket://add/sub://${b64(withTarget(u, "base64"))}?remark=${enc(n)}` },
  { name: "Quantumult X", platforms: "iOS", href: (u, n) => `quantumult-x:///add-resource?remote-resource=${enc(JSON.stringify({ server_remote: [`${withTarget(u, "quanx")}, tag=${n}`] }))}` },
  { name: "Surge", platforms: "iOS · macOS", href: (u, n) => `surge:///install-config?url=${enc(withTarget(u, "surge"))}&name=${enc(n)}` },
  { name: "Loon", platforms: "iOS", href: (u) => `loon://import?sub=${enc(withTarget(u, "loon"))}` },
  { name: "Egern", platforms: "iOS", href: (u, n) => `egern:///policy_groups/new?name=${enc(n)}&url=${enc(withTarget(u, "egern"))}` },
  { name: "Streisand", platforms: "iOS", href: (u, n) => `streisand://import/${withTarget(u, "base64")}#${enc(n)}` },
  { name: "V2Box", platforms: "iOS · Android", href: (u, n) => `v2box://install-sub?url=${enc(withTarget(u, "base64"))}&name=${enc(n)}` },
  { name: "v2rayNG", platforms: "Android", href: (u, n) => `v2rayng://install-config?url=${enc(withTarget(u, "base64"))}&name=${enc(n)}` },
  { name: "NekoBox", platforms: "Android", href: (u, n) => `sn://subscription?url=${enc(withTarget(u, "base64"))}&name=${enc(n)}` },
];

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
    <main className="mx-auto grid w-full max-w-[960px] gap-4 p-4">
      <PageHeader eyebrow="订阅" title={title} description="在下方选择你使用的客户端一键导入；电脑上的客户端也可复制地址手动添加。" />
      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-[1fr_auto]">
          <div className="stack">
            <div className="grid gap-3 sm:grid-cols-3">
              <Readout label="已用" value={bytes(d.used)} caption={d.quota ? `共 ${bytes(d.quota)}` : "不限额度"} />
              <Readout label="到期" value={d.expires_at ? date(d.expires_at) : "长期有效"} />
              <Readout label="下次重置" value={d.next_reset ? date(d.next_reset) : "不重置"} />
            </div>
            {d.quota > 0 && <Meter label="流量使用" value={d.used} max={d.quota} />}
            <div className="subscription-url">
              <Input readOnly className="font-mono text-xs" aria-label="订阅地址" value={d.url} />
              <CopyButton value={d.url}>复制地址</CopyButton>
            </div>
          </div>
          <div className="qr-code justify-self-center"><QRCodeSVG value={d.url} size={168} title="订阅二维码" /></div>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="stack">
          <h2 className="text-base font-semibold">一键导入</h2>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {importClients.map((c) => (
              <Button key={c.name} variant="outline" className="justify-between" render={<a href={c.href(d.url, title)} />}>
                <span className="text-left">{c.name}<small className="block text-[var(--mg-ink-3)]">{c.platforms}</small></span>
                <ArrowUpRight />
              </Button>
            ))}
          </div>
          <small className="text-[var(--mg-ink-3)]">没有反应时说明本机未安装该客户端；v2rayN 等请复制地址后在客户端里添加订阅。</small>
        </CardContent>
      </Card>
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
