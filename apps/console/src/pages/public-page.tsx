import {PublicGuestbook} from "../components/guestbook";
import {PublicSubscriptions,type PublicSubscriptionCard} from "../components/public-subscriptions";
import {GlobalAnnouncement} from "../components/global-announcement";
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowUpRight,
  Globe2,
  LogIn,
  QrCode,
} from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { IconTile, CopyButton, Notice,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@migate/ui";
import { api } from "../api";
import { State } from "../components/feedback";
import "./public-page.css";

import {PublishedAnnouncement,type PublishedNotice} from "../components/published-announcement";

type PublishedPage = PublishedNotice & {
  title: string;
  description: string;
  notice: string;
  show_login: boolean;
  theme?: string;
  show_share?: boolean;
  guestbook?: {enabled:boolean;allow_anonymous:boolean};
 subscriptions?: PublicSubscriptionCard[];
  cards?: { title: string; description: string; slug: string }[];
};

export function PublicPage() {
  const slug = location.pathname.slice(3);
  const query = useQuery({
    queryKey: ["public-page", slug],
    queryFn: () =>
      api("/pages/" + encodeURIComponent(slug)) as Promise<PublishedPage>,
    retry: false,
    refetchInterval: 30000,
  });
  const [qr, setQR] = useState(false),
    [copied, setCopied] = useState(false),
    [error, setError] = useState("");
  const page = !query.isError ? query.data : undefined;
  const pageURL = location.origin + "/p/" + encodeURIComponent(slug);
  useEffect(() => {
    const previous = document.title;
    document.title = page?.title ?? "公开页面 · MiGate";
    return () => {
      document.title = previous;
    };
  }, [page?.title]);
  useEffect(() => {
    if (!copied) return;
    const timeout = setTimeout(() => setCopied(false), 2500);
    return () => clearTimeout(timeout);
  }, [copied]);
  // 复制反馈由 CopyButton 负责；页面另在下方显示一次“已复制”状态
  const copyProps = {
    value: pageURL,
    onCopied: () => {
      setCopied(true);
      setError("");
    },
    onError: () => setError("无法访问剪贴板，请通过二维码或浏览器地址栏分享。"),
  };
  return (
    <main className="published-page" data-theme={page?.theme || "violet"}>
      <header className="published-top">
        <a className="published-brand" href={pageURL}>
          <Globe2 size={21} />
          <span>公开目录</span>
        </a>
        {page?.show_login && (
          <Button variant="outline" onClick={() => location.assign("/")}>
            <LogIn size={16} />
            登录
          </Button>
        )}
      </header>
      {query.isPending ? (
        <Card>
          <CardContent>
            <State loading />
          </CardContent>
        </Card>
      ) : query.isError ? (
        <Card>
          <CardContent>
            <div className="stack">
              <h1>页面不可用</h1>
              <Notice tone="bad">{query.error.message}</Notice>
              <State error={query.error} onRetry={() => query.refetch()} />
            </div>
          </CardContent>
        </Card>
      ) : (
        page && (
          <>
            <section className="published-hero">
                            <h1>{page.title}</h1>
              {page.description && (
                <p className="published-description">{page.description}</p>
              )}
              {page.show_share && (
                <div className="published-actions">
                  <CopyButton {...copyProps}>复制页面地址</CopyButton>
                  <Button variant="outline" onClick={() => setQR(true)}>
                    <QrCode size={16} />
                    页面二维码
                  </Button>
                </div>
              )}
              {copied && <p role="status">页面地址已复制。</p>}
              {error && <Notice tone="bad">{error}</Notice>}
            </section>
            <GlobalAnnouncement audience="public"/>
            <PublishedAnnouncement notice={page} scope={slug}/>
            {!!page.cards?.length && (
              <section
                className="published-directory"
                aria-labelledby="directory-title"
              >
                <div className="published-directory-heading">
                  <h2 id="directory-title">探索目录</h2>
                  <span>{page.cards.length} 个公开页面</span>
                </div>
                <div className="published-grid">
                  {page.cards.map((card) => (
                    <a
                      className="published-card-link"
                      key={card.slug}
                      href={"/p/" + encodeURIComponent(card.slug)}
                    >
                      <Card size="lg" className="published-card">
                        <CardContent className="published-card-body">
                          <IconTile size="lg" tone="soft">
                            <Globe2 />
                          </IconTile>
                          <h3>{card.title}</h3>
                          {card.description && <p>{card.description}</p>}
                          <span className="published-card-action">
                            查看页面
                            <ArrowUpRight size={16} />
                          </span>
                        </CardContent>
                      </Card>
                    </a>
                  ))}
                </div>
              </section>
            )}
            {!!page.subscriptions?.length&&<PublicSubscriptions items={page.subscriptions}/>}
            {page.guestbook?.enabled&&<PublicGuestbook slug={slug}/>}
<footer className="published-footer">MiGate · 公开内容</footer>
          </>
        )
      )}
      <Dialog open={qr && page?.show_share === true} onOpenChange={setQR}>
        <DialogContent className="published-qr">
          <DialogTitle>页面二维码</DialogTitle>
          <DialogDescription>仅分享当前公开页面地址。</DialogDescription>
          <QRCodeSVG value={pageURL} size={192} title="公开页面二维码" />
          <p className="break-all">{pageURL}</p>
          <CopyButton {...copyProps} icon={false}>复制页面地址</CopyButton>
        </DialogContent>
      </Dialog>
    </main>
  );
}
