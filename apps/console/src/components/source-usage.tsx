import { useQuery } from "@tanstack/react-query";
import { Button, Card, CardContent } from "@migate/ui";
import { client } from "../lib/client";
import { date } from "../lib/models";

// Format with BigInt: upstream counter strings must never pass through Number.
function capacity(value: string | null | undefined): string {
 if (value == null || !/^\d+$/.test(value)) return "未知";
 const n=BigInt(value);const units=["B","KiB","MiB","GiB","TiB","PiB"];
 let divisor=1n,index=0;while(n/divisor>=1024n&&index<units.length-1){divisor*=1024n;index++;}
 const tenths=n*10n/divisor;
 return `${tenths/10n}${tenths%10n ? "."+tenths%10n : ""} ${units[index]}`;
}
export function SourceUsagePanel() {
 const q=useQuery({queryKey:["source-usage"],queryFn:client.sourceUsage,refetchInterval:30000});
 const d=q.data;
 return <Card className="source-usage-panel" aria-label="上游用量合计"><CardContent>
  <div className="section-heading"><div><h3>上游用量合计</h3><p className="muted">全部启用订阅源 · 上游自报，不是用户强制额度</p></div><Button variant="outline" size="sm" disabled={q.isFetching} onClick={()=>void q.refetch()}>刷新用量</Button></div>
  {q.isPending ? <p role="status">正在读取上游观测…</p> : q.isError ? <p role="alert">上游用量读取失败，当前数值未知，请重试。</p> : d && <>
   <div className="source-usage-grid">
    {([["剩余流量",d.remaining],["总流量",d.total],["已上传",d.upload],["已下载",d.download]] as const).map(([label,value])=><div key={label}><span>{label}</span><strong title={value==null ? "缺失或过期数据" : value+" B"}>{capacity(value)}</strong></div>)}
   </div>
   <p>{d.status==="empty" ? "暂无参与合计的订阅源。" : `计入 ${d.included} 个 · 未知 ${d.unknown_sources} 个` } · 排除流量 {d.excluded} 个 · 停用 {d.disabled} 个</p>
   <p className="muted">最早到期：{d.expire==null ? "未知" : d.expire==="0" ? "未设到期" : Number(d.expire)<8640000000000 ? date(Number(d.expire)) : "未知"} · 查询时间：{date(d.observed_at)}</p>
   <p className="muted">缺失或过期观测对应合计显示未知，不补零；排除流量仍参与最早到期。</p>
  </>}
 </CardContent></Card>;
}
