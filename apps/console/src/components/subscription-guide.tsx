import { useId, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { ArrowUpRight, Monitor, Smartphone } from "lucide-react";
import { Badge, Button, Card, CardContent, ChoiceCard, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, IconTile, Notice, SectionHeader, Segmented } from "@weir/ui";
import { importClients } from "../lib/import-clients";

export type GuideSubscription = { title: string; target: string; url: string; expiresAt?: number; unavailableReason?: string };
const devices = [["ios", "iPhone / iPad", "iOS"], ["android", "Android", "Android"], ["desktop", "电脑", "Windows · macOS · Linux"]] as const;
type Device = typeof devices[number][0];

// 导入只包装传入的地址；固定格式快照不能靠追加参数转成其他格式。
export function SubscriptionGuide({ items }: { items: GuideSubscription[] }) {
  const id = useId();
  const [device, setDevice] = useState<Device>("ios");
  const [selected, setSelected] = useState("");
  const [qr, setQr] = useState<GuideSubscription | null>(null);
  const clients = importClients.filter((c) => items.some((i) => i.target === c.target) && (c.platforms === "全平台" || (device === "desktop" ? /Windows|macOS|Linux/.test(c.platforms) : c.platforms.includes(device === "ios" ? "iOS" : "Android"))));
  const client = clients.find((c) => c.name === selected) ?? clients[0];
  const compatible = client ? items.filter((i) => i.target === client.target) : items;
  const activeQR = qr && items.some((i) => i.url === qr.url) ? qr : null;
  return <section className="subscription-guide" aria-labelledby={id} data-tour="subscription-guide">
    <div><p className="subscription-guide-eyebrow">开始使用</p><h2 id={id}>三步连接你的设备</h2><p className="subscription-guide-lead">选择设备和客户端，再添加订阅。配置由发布者提供。</p></div>
    <div className="subscription-guide-devices" role="group" aria-label="选择设备">
      {devices.map(([value, label, caption]) => <ChoiceCard key={value} selected={device === value} render={<button type="button" aria-pressed={device === value} />} onClick={() => { setDevice(value); setSelected(""); }}>
        <IconTile tone={device === value ? "accent" : "neutral"}>{value === "desktop" ? <Monitor /> : <Smartphone />}</IconTile><strong>{label}</strong><small>{caption}</small>
      </ChoiceCard>)}
    </div>
    <div className="subscription-guide-clients">
      <SectionHeader title="选择客户端" description="仅列出支持这些订阅格式的客户端。电脑选项包含不同桌面系统，请核对下方支持范围。" />
      {client ? <Segmented aria-label="选择客户端" value={client.name} onValueChange={setSelected} options={clients.map((c) => [c.name, c.name] as const)} /> : <Notice>当前没有适配此设备的一键导入方式，可下载配置或复制地址后手动添加。</Notice>}
      {client && <small>{client.name} · {client.platforms}</small>}
    </div>
    <div className="subscription-guide-results">
      {compatible.map((item) => {
        const expired = item.expiresAt != null && item.expiresAt * 1000 <= Date.now();
        const url = new URL(item.url, location.origin).href;
        return <Card key={item.url} size="lg"><CardContent className="subscription-guide-result">
          <SectionHeader flush title={item.title} description={item.expiresAt ? `有效期至 ${new Date(item.expiresAt * 1000).toLocaleString()}` : "选择客户端后添加订阅"} actions={<Badge variant="soft" tone={expired ? "warn" : "info"}>{expired ? "已到期" : item.target}</Badge>} />
          {expired ? <Notice tone="warn">此订阅已到期，请联系发布者更新。</Notice> : item.unavailableReason ? <Notice tone="warn">{item.unavailableReason}</Notice> : <>
            <CopyField label="订阅地址" value={url} />
            <div className="subscription-guide-actions">
              {client && <Button render={<a href={client.href(url, item.title)} />}>导入 {client.name}<ArrowUpRight /></Button>}
              <Button variant={client ? "outline" : "default"} render={<a href={url} download />}>下载配置</Button>
              <Button variant="outline" onClick={() => setQr(item)}>二维码</Button>
            </div>
          </>}
        </CardContent></Card>;
      })}
    </div>
    <Notice>先安装所选客户端，再点导入；浏览器没有唤起客户端时，复制订阅地址到客户端手动添加。订阅地址包含访问凭据，请妥善保管。</Notice>
    <Dialog open={!!activeQR} onOpenChange={(open) => !open && setQr(null)}><DialogContent>
      <DialogTitle>订阅二维码</DialogTitle><DialogDescription>{activeQR?.title} · 使用客户端扫描添加</DialogDescription>
      {activeQR && <div className="subscription-guide-qr"><QRCodeSVG value={new URL(activeQR.url, location.origin).href} size={193} title="订阅二维码" /></div>}
      <DialogFooter><Button variant="outline" onClick={() => setQr(null)}>关闭</Button></DialogFooter>
    </DialogContent></Dialog>
  </section>;
}
