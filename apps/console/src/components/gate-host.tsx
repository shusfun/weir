import {HostHistory} from "./host-history";
import {useEffect,useState} from "react";
import { SectionHeader,Card,CardContent} from "@migate/ui";
import {Server} from "lucide-react";
import {bytes,type Device} from "../lib/models";
import {Metric} from "./metric";
interface Host {source:string;observed_at:string;validity:string;cpu_percent:number|null;cpu_interval_seconds:number|null;logical_cpus:number|null;load:number[]|null;uptime_seconds:number|null;memory_total:number|null;memory_available:number|null;disk_total:number|null;disk_available:number|null;network_rx:string|null;network_tx:string|null}
function counter(value:string|null|undefined){if(!value||!/^\d+$/.test(value))return "未知";let n=BigInt(value),unit=0;const units=["B","KiB","MiB","GiB","TiB","PiB"];let scale=1n;while(n/scale>=1024n&&unit<units.length-1){scale*=1024n;unit++}return String(n/scale)+(unit?"."+String((n%scale)*10n/scale):"")+" "+units[unit]}
export function GateHost({device}:{device:Device}){
 const [now,N]=useState(Date.now());useEffect(()=>{const timer=setInterval(()=>N(Date.now()),5000);return()=>clearInterval(timer)},[]);
 const raw=device.metadata.host_status as Host|undefined;const at=Date.parse(raw?.observed_at??"");const fresh=device.status==="approved"&&Number.isFinite(at)&&now-at<=60000&&at-now<=60000&&raw?.source==="linux-procfs-statfs"&&raw.validity!=="unknown";const h=fresh?raw:undefined;
 const memory=h?.memory_total!=null&&h.memory_available!=null?`${bytes(h.memory_total-h.memory_available)} / ${bytes(h.memory_total)}`:"未知";
 const disk=h?.disk_total!=null&&h.disk_available!=null?`${bytes(h.disk_available)} / ${bytes(h.disk_total)}`:"未知";
 return <><Card aria-label="主机观测"><CardContent className="stack">
  <SectionHeader title="主机观测" description={<>只读系统采样 · {fresh?(h?.validity==="available"?"已更新":"部分未知"):"未知 / 观测已过期"}</>} actions={<Server size={22}/>} />
  <div className="host-metrics"><Metric title="CPU 使用率" value={h?.cpu_percent==null?"未知":h.cpu_percent.toFixed(1)+"%"} caption={h?.cpu_interval_seconds==null?"等待两次有效采样；负载不等于使用率":`最近 ${h.cpu_interval_seconds.toFixed(0)} 秒平均；全部 CPU 合计归一到 100%`}/>
   <Metric title="系统负载" value={h?.load?.length===3?h.load.map(v=>v.toFixed(2)).join(" / "):"未知"} caption="1 / 5 / 15 分钟；不是 CPU 使用率"/>
   <Metric title="逻辑 CPU" value={h?.logical_cpus==null?"未知":String(h.logical_cpus)} caption="Agent 可见逻辑处理器"/>
   <Metric title="内存已用 / 总量" value={memory} caption="系统 MemTotal − MemAvailable"/>
   <Metric title="数据盘可用 / 总量" value={disk} caption="Agent 数据目录所在文件系统"/>
   <Metric title="主机累计接收" value={counter(h?.network_rx)} caption="非回环网卡之和；不等于代理用量"/>
   <Metric title="主机累计发送" value={counter(h?.network_tx)} caption="虚拟网卡可能重复统计；不用于计费"/>
   <Metric title="系统运行时间" value={h?.uptime_seconds==null?"未知":`${Math.floor(h.uptime_seconds/86400)} 天 ${Math.floor(h.uptime_seconds%86400/3600)} 小时`} caption="系统启动后时长"/>
  </div>
  <p className="muted">来源：Linux procfs / statfs；采样：{Number.isFinite(at)?new Date(at).toLocaleString():"未知"}。60 秒无新采样显示未知；缺失字段不补零。累计量会在重启、网卡重建时重置。</p>
 </CardContent></Card><HostHistory deviceId={device.id}/></>
}
