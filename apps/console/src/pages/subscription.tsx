import { useQuery } from "@tanstack/react-query";
import { client } from "../lib/client";
import {ClientRelease} from "../components/client-release";
import { ClientDownload } from "../components/client-download";
import { ClientIcon } from "../components/client-icon";
import { useState } from "react";
import { PageState, ChoiceCard, CopyButton, Field, Notice, SectionHeader,
  Button,
  Card,
  CardContent,
  Badge,
  Input,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import {
  Download,
  ShieldCheck,
  Link2,
  ArrowUpRight,
} from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { bytes, date } from "../lib/models";
import { PageHeader, Readout, Meter } from "@migate/ui";
import { api } from "../api";
import { State } from "../components/feedback";
interface Subscription {
  url?: string;
  formats?: string[];
  quota?: number;
  used?: number;
  expires?: number;
  node_count?: number;
}
const platformNames:Record<string,string>={all:"全部平台",windows:"Windows",macos:"macOS",linux:"Linux",android:"Android",ios:"iOS",harmonyos:"HarmonyOS"};
const names: Record<string, string> = {
  base64: "通用订阅",
  clash: "Clash / Mihomo",
  singbox: "sing-box",
  surge: "Surge",
  loon: "Loon",
  quanx: "Quantumult X",
  egern: "Egern",
};
export function SubscriptionPage() {
  const query = useQuery({ queryKey: ["my-subscription"], queryFn: () => api("/subscription") as Promise<Subscription> });
  if (query.isPending) return <State loading />;
  if (query.error) return <State error={query.error} onRetry={() => query.refetch()} />;
  return <SubscriptionView data={query.data} />;
}

function SubscriptionView({ data }: { data: Subscription }) {
  const [format, F] = useState("clash");
  const [platform,PL]=useState("all"),[chosen,CH]=useState<string|null>(null);
  const catalog=useQuery({queryKey:["client-catalog"],queryFn:()=>client.catalog()});
  const [expanded,EX]=useState(false);
  const [error, E] = useState("");
  const url = data.url ? data.url + "?target=" + format : "";
  const used = data.used ?? 0;
  const quota = data.quota ?? 0;
  // 主推 FlClash：单独置顶，其余客户端排在后面
  const visible = catalog.data?.clients.filter((v) => platform === "all" || v.platforms.includes(platform)) ?? [];
  const featured = visible.find((v) => v.id === "flclash" || /flclash/i.test(v.name));
  const others = visible.filter((v) => v !== featured);
  return (
    <div className="stack">
      <PageHeader title="连接你的世界" description="选择客户端格式，获取自己的节点订阅。订阅只包含已生效、仍有效的授权。" />
      <div className="instrument-row" data-count="3">
        <Readout label="可用节点" value={data.node_count === undefined ? undefined : String(data.node_count)} caption="已生效且有效的授权" />
        <Readout
          label="剩余流量"
          value={data.used === undefined || data.quota === undefined ? undefined : bytes(Math.max(0, quota - used))}
          caption={"总额度 " + bytes(data.quota)}
        >
          <Meter label="流量使用" value={data.used} max={data.quota} />
        </Readout>
        <Readout label="有效期" value={data.expires ? date(data.expires) : "长期有效"} />
      </div>
      <div className="subscription-grid" id="subscription-connection">
        <Card>
          <CardContent>
            <SectionHeader title="订阅连接" description="链接含有个人凭据，请妥善保管。" actions={<ShieldCheck size={22} />} />
            {url ? (
              <div className="stack">
                <Field label="客户端格式">
                  <Select
                    value={format}
                    onValueChange={(v) => {
                      F(v ?? "clash");
                      CH(null);
                      
                    }}
                  >
                    <SelectTrigger aria-label="客户端格式">
                      <SelectValue>{names[format] ?? format}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {(data.formats ?? []).map((f) => (
                        <SelectItem value={f} key={f}>
                          {names[f] ?? f}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <div className="subscription-url">
                  <Input
                    className="font-mono text-xs"
                    readOnly
                    aria-label={format + "订阅地址"}
                    value={url}
                  />
                  <CopyButton value={url} onCopied={() => E("")} onError={() => E("复制失败，请手动选择订阅地址复制。")}>
                    复制链接
                  </CopyButton>
                </div>
                <div className="actions">
                  <Button
                    variant="outline"
                    onClick={() =>
                      window.open(url, "_blank", "noopener,noreferrer")
                    }
                  >
                    <Download size={16} />
                    下载配置
                  </Button>
                  {format === "clash" && (!chosen || catalog.data?.clients.find(v=>v.id===chosen)?.import==="clash") && (
                    <Button
                      variant="ghost"
                      onClick={() =>
                        location.assign(
                          "clash://install-config?url=" +
                            encodeURIComponent(url) +
                            "&name=MiGate",
                        )
                      }
                    >
                      导入 Clash <ArrowUpRight size={15} />
                    </Button>
                  )}
                </div>
                <div className="usage-summary">
                  <div>
                    <span>流量使用</span>
                    <strong>
                      {bytes(data.used)} / {bytes(data.quota)}
                    </strong>
                  </div>
                  <Meter label="流量使用" value={data.used} max={data.quota} />
                </div>
                {error && <Notice tone="bad">{error}</Notice>}
              </div>
            ) : (
              <PageState kind="empty" title="暂无可用订阅" detail="请联系管理员分配节点。" />
            )}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="qr-panel">
            <h2>扫码导入</h2>
            <p>使用客户端扫描当前格式订阅。</p>
            {url && (
              <div className="qr-code">
                <QRCodeSVG value={url} size={168} title="我的订阅二维码" />
              </div>
            )}
            <small>{names[format] ?? format}</small>
          </CardContent>
        </Card>
      </div>
      <Card><CardContent className="stack">
        <SectionHeader title="选择客户端" description="下载入口由管理员维护；选择后切换本人订阅格式。" actions={<Select value={platform} onValueChange={v=>{PL(v??"all");EX(false)}}><SelectTrigger aria-label="客户端适用平台"><SelectValue>{platformNames[platform]??platform}</SelectValue></SelectTrigger><SelectContent>{[["all","全部平台"],["windows","Windows"],["macos","macOS"],["linux","Linux"],["android","Android"],["ios","iOS"],["harmonyos","HarmonyOS"]].map(([v,n])=><SelectItem key={v} value={v}>{n}</SelectItem>)}</SelectContent></Select>} />
        {featured && (
          <ChoiceCard featured className="client-featured" selected={chosen===featured.id} render={<article/>}>
            <div className="catalog-identity">
              <ClientIcon icon={featured.icon} data={featured.icon_data}/>
              <div>
                <h3>{featured.name} <Badge variant="secondary">推荐</Badge></h3>
                <small>{featured.platforms.map(p=>platformNames[p]??p).join(" / ")}</small>
                {featured.repo&&<ClientRelease id={featured.id} repo={featured.repo}/>}
              </div>
            </div>
            <p>{featured.description}</p>
            <div className="actions">
              <Button disabled={!data.formats?.includes(featured.format)} onClick={()=>{CH(featured.id);F(featured.format);document.getElementById("subscription-connection")?.scrollIntoView({behavior:"smooth",block:"start"})}}>{chosen===featured.id?"已选择":"使用 FlClash"}</Button>
              <ClientDownload client={featured} />
            </div>
          </ChoiceCard>
        )}
        {catalog.isPending?<p role="status">正在读取客户端目录…</p>:catalog.isError?<Notice tone="bad">客户端目录读取失败。<Button variant="outline" onClick={()=>void catalog.refetch()}>重试目录</Button></Notice>:<>{others.length>0&&<h3 className="mg-engraved">其他客户端</h3>}<div className="client-choice-grid">
          {others.slice(0,expanded?50:6).map(v=><ChoiceCard className="client-choice" key={v.id} selected={chosen===v.id} render={<article/>}><div className="catalog-identity"><ClientIcon icon={v.icon} data={v.icon_data}/><div><h3>{v.name}</h3><small>{v.platforms.map(p=>platformNames[p]??p).join(" / ")}</small>{v.repo&&<ClientRelease id={v.id} repo={v.repo}/>}</div></div><p>{v.description}</p><div className="actions"><Button size="sm" variant={chosen===v.id?"secondary":"outline"} disabled={!data.formats?.includes(v.format)} onClick={()=>{CH(v.id);F(v.format);document.getElementById("subscription-connection")?.scrollIntoView({behavior:"smooth",block:"start"})}}>{chosen===v.id?"已选择":"使用此客户端"}</Button><ClientDownload client={v} /></div></ChoiceCard>)}
          {!visible.length&&<p>此平台暂无客户端，请使用上方通用订阅。</p>}
        </div></>}
        {others.length>6&&<Button variant="outline" onClick={()=>EX(!expanded)}>{expanded?"收起其他客户端":"查看全部其他客户端（"+others.length+"）"}</Button>}
      </CardContent></Card>
      <div className="help-note">
        <ShieldCheck size={18} />
        <p>
          订阅仅包含你的有效节点。实际代理流量直接连接
          Gate；如节点不可用，请联系管理员检查授权状态。
        </p>
      </div>
    </div>
  );
}
