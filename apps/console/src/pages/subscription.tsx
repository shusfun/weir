import { useQuery } from "@tanstack/react-query";
import { client } from "../lib/client";
import {ClientRelease} from "../components/client-release";
import { ClientIcon } from "../components/client-icon";
import { useState } from "react";
import {
  Button,
  Card,
  CardContent,
  Input,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import {
  Copy,
  Download,
  ShieldCheck,
  Link2,
  Check,
  ArrowUpRight,
} from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { bytes, date } from "../lib/models";
import { Metric } from "../components/metric";
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
export function SubscriptionPage({ data }: { data: Subscription }) {
  const [format, F] = useState("clash");
  const [platform,PL]=useState("all"),[chosen,CH]=useState<string|null>(null);
  const catalog=useQuery({queryKey:["client-catalog"],queryFn:()=>client.catalog()});
  const [expanded,EX]=useState(false);
  const [copied, C] = useState(false);
  const [error, E] = useState("");
  const url = data.url ? data.url + "?target=" + format : "";
  const used = data.used ?? 0;
  const quota = data.quota ?? 0;
  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      C(true);
      E("");
      setTimeout(() => C(false), 2000);
    } catch {
      E("复制失败，请手动选择订阅地址复制。");
    }
  }
  return (
    <div className="stack">
      <div className="welcome-banner">
        <div>
          <span className="eyebrow">YOUR CONNECTIONS</span>
          <h2>连接你的世界</h2>
          <p>选择客户端格式，获取自己的节点订阅。</p>
        </div>
        <span className="welcome-icon">
          <Link2 size={36} />
        </span>
      </div>
      <div className="metric-grid">
        <Metric
          title="可用节点"
          value={String(data.node_count ?? "未知")}
          caption="已生效且有效的授权"
        />
        <Metric
          title="剩余流量"
          value={
            data.used === undefined || data.quota === undefined
              ? "未知"
              : bytes(Math.max(0, quota - used))
          }
          caption={"总额度 " + bytes(data.quota)}
        />
        <Metric
          title="有效期"
          value={data.expires ? date(data.expires) : "长期有效"}
        />
      </div>
      <div className="subscription-grid" id="subscription-connection">
        <Card>
          <CardContent>
            <div className="section-heading">
              <div>
                <h2>订阅连接</h2>
                <p>链接含有个人凭据，请妥善保管。</p>
              </div>
              <ShieldCheck size={22} />
            </div>
            {url ? (
              <div className="stack">
                <div className="field">
                  <label>客户端格式</label>
                  <Select
                    value={format}
                    onValueChange={(v) => {
                      F(v ?? "clash");
                      CH(null);
                      C(false);
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
                </div>
                <div className="subscription-url">
                  <Input
                    readOnly
                    aria-label={format + "订阅地址"}
                    value={url}
                  />
                  <Button onClick={copy}>
                    {copied ? <Check size={16} /> : <Copy size={16} />}{" "}
                    {copied ? "已复制" : "复制链接"}
                  </Button>
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
                  <div className="usage-track">
                    <span
                      style={{
                        width:
                          Math.min(100, quota ? (used / quota) * 100 : 0) + "%",
                      }}
                    />
                  </div>
                </div>
                {error && <p role="alert">{error}</p>}
              </div>
            ) : (
              <div className="empty">暂无可用订阅，请联系管理员分配节点。</div>
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
        <div className="section-heading"><div><h2>选择客户端</h2><p>下载入口由管理员维护；选择后切换本人订阅格式。</p></div>
        <Select value={platform} onValueChange={v=>{PL(v??"all");EX(false)}}><SelectTrigger aria-label="客户端适用平台"><SelectValue>{platformNames[platform]??platform}</SelectValue></SelectTrigger><SelectContent>{[["all","全部平台"],["windows","Windows"],["macos","macOS"],["linux","Linux"],["android","Android"],["ios","iOS"],["harmonyos","HarmonyOS"]].map(([v,n])=><SelectItem key={v} value={v}>{n}</SelectItem>)}</SelectContent></Select></div>
        {catalog.isPending?<p role="status">正在读取客户端目录…</p>:catalog.isError?<p role="alert">客户端目录读取失败。<Button variant="outline" onClick={()=>void catalog.refetch()}>重试目录</Button></p>:<div className="client-choice-grid">
          {catalog.data?.clients.filter(v=>platform==="all"||v.platforms.includes(platform)).slice(0,expanded?50:6).map(v=><article className="client-choice" key={v.id} data-selected={chosen===v.id}><div className="catalog-identity"><ClientIcon icon={v.icon} data={v.icon_data}/><div><h3>{v.name}</h3><small>{v.platforms.map(p=>platformNames[p]??p).join(" / ")}</small>{v.repo&&<ClientRelease id={v.id} repo={v.repo}/>}</div></div><p>{v.description}</p><div className="actions"><Button size="sm" variant={chosen===v.id?"secondary":"outline"} disabled={!data.formats?.includes(v.format)} onClick={()=>{CH(v.id);F(v.format);C(false);document.getElementById("subscription-connection")?.scrollIntoView({behavior:"smooth",block:"start"})}}>{chosen===v.id?"已选择":"使用此客户端"}</Button><Button variant="ghost" size="sm" onClick={()=>window.open(v.url,"_blank","noopener,noreferrer")}>下载客户端</Button></div></article>)}
          {!catalog.data?.clients.some(v=>platform==="all"||v.platforms.includes(platform))&&<p>此平台暂无客户端，请使用下方通用订阅。</p>}
        </div>}
        {!!catalog.data&&catalog.data.clients.filter(v=>platform==="all"||v.platforms.includes(platform)).length>6&&<Button variant="outline" onClick={()=>EX(!expanded)}>{expanded?"收起客户端目录":"查看全部客户端（"+catalog.data.clients.filter(v=>platform==="all"||v.platforms.includes(platform)).length+"）"}</Button>}
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
