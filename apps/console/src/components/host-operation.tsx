import { CodeBlock, Collapsible, Notice } from "@weir/ui";
import { errorMessage } from "../api";
export type RootOperation = { id:string; state:string; error?:string; trace_id?:string; logs?:string[] };
export function HostOperation({operation:o}:{operation:RootOperation}) {
 const status=({queued:"等待宿主执行",downloading:"准备中",installing:"正在执行",done:"宿主操作已完成",failed:"宿主操作失败"} as Record<string,string>)[o.state]??"状态未知";
 const failure=o.error?errorMessage(o.error):"";
 return <div className="stack"><Notice tone={o.state==="failed"?"bad":o.state==="done"?"ok":"info"}>{status} · {o.id}{failure?` · ${failure}`:""}</Notice><Collapsible title="操作记录"><small className="break-all">关联 {o.trace_id||"未知"}</small><CodeBlock>{o.logs?.join("\n")||"等待执行回执"}</CodeBlock></Collapsible></div>;
}
