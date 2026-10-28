// 公开页上的已发布订阅快照：下载、复制地址与二维码。
import { useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
  CopyButton,
  Notice,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@weir/ui";
export interface PublicSubscriptionCard {
  title: string;
  target: string;
  expires_at: number;
  path: string;
}
const names: Record<string, string> = {
  links: "节点链接",
  base64: "Base64",
  clash: "Clash / Mihomo",
  singbox: "sing-box",
  surge: "Surge",
  loon: "Loon",
  quanx: "Quantumult X",
  egern: "Egern",
};
export function PublicSubscriptions({ items }: { items: PublicSubscriptionCard[] }) {
  const [qr, setQr] = useState<PublicSubscriptionCard | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  return (
    <section className="published-directory" aria-labelledby="public-subscriptions-title">
      <div className="published-directory-heading">
        <h2 id="public-subscriptions-title">公开订阅</h2>
        <span>{items.length} 个已发布快照</span>
      </div>
      <p className="muted">由管理员明确发布；下载内容包含节点凭据，请自行保管。到期或撤下后无法继续下载。</p>
      {notice && <Notice role="status">{notice}</Notice>}
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="published-grid">
        {items.map((card) => (
          <Card key={card.path}>
            <CardContent className="stack">
              <h3>{card.title}</h3>
              <p>
                {names[card.target] ?? card.target} · 到期 {new Date(card.expires_at * 1000).toLocaleString()}
              </p>
              <div className="actions">
                <Button render={<a href={card.path} download />}>下载订阅</Button>
                <CopyButton
                  variant="outline"
                  value={() => location.origin + card.path}
                  onCopied={() => {
                    setError("");
                    setNotice("已复制订阅地址：" + card.title);
                  }}
                  onError={() => {
                    setNotice("");
                    setError("浏览器未允许复制，请使用二维码或下载入口。");
                  }}
                >
                  复制订阅地址
                </CopyButton>
                <Button variant="outline" onClick={() => setQr(card)}>
                  订阅二维码
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      <Dialog open={!!qr && items.some((v) => v.path === qr.path)} onOpenChange={(v) => !v && setQr(null)}>
        <DialogContent className="published-qr">
          <DialogTitle>公开订阅二维码</DialogTitle>
          <DialogDescription>{qr?.title} · 仅编码该次明确发布的快照下载地址。</DialogDescription>
          {qr && <QRCodeSVG value={location.origin + qr.path} size={192} title="公开订阅二维码" />}
          <DialogFooter>
            <Button variant="outline" onClick={() => setQr(null)}>
              关闭订阅二维码
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
