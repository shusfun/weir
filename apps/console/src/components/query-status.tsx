import { Button, Notice } from "@weir/ui";
import { isTransient } from "../api";
import { useQueryFailures, visibleBackgroundFailures } from "../lib/query";

export function QueryStatus() {
  const failures=useQueryFailures();
  const disconnected=failures.some(f=>isTransient(f.error));
  const background=visibleBackgroundFailures(failures);
  const reasons=[...new Set(background.map(f=>String(f.error)))];
  return <>
    {(disconnected||background.length>0)&&<Notice tone={background.length?"bad":"warn"} role={background.length?"alert":"status"}>
      <div className="stack gap-2">
        {disconnected&&<span>中心暂时连不上（更新或重启时属正常），恢复后自动继续</span>}
        {reasons.map(reason=><span key={reason}>后台读取失败：{reason}</span>)}
        {background.length>0&&<Button size="sm" variant="outline" onClick={()=>{for(const f of background)void f.query.fetch().catch(()=>{});}}>重试</Button>}
      </div>
    </Notice>}
  </>;
}
