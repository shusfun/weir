import { useState } from "react";
import { ChoiceCard, Field, Radio, RadioGroup, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import type { ExitSpec } from "../lib/core";
import { exitTitle, useExitCatalog } from "../lib/exits";
import { ExitFields } from "./chain-exit";
import { State } from "./feedback";

export function ExitSelector(props: Parameters<typeof ExitFields>[0] & {gateId:number; exitId?:number; onExitId:(id:number|undefined)=>void; ownerUserId?:number}) {
  const { gateId, exitId, onExitId, ownerUserId, ...fields } = props;
  const [mode, setMode] = useState(exitId ? "existing" : fields.value.kind === "freedom" ? "direct" : "new");
  const exits = useExitCatalog(fields.admin || !!ownerUserId, gateId);
  const items = (exits.data?.items ?? []).filter(e=>e.enabled && (ownerUserId ? e.owner_user_id===ownerUserId : fields.admin ? e.owner_user_id == null : true));
  return <div className="stack">
    <RadioGroup aria-label="出口选择" value={mode} onValueChange={v=>{setMode(String(v));onExitId(undefined);fields.onChange({kind:v==="direct"?"freedom":"socks5"});}} className="grid gap-2 sm:grid-cols-3">
      {(fields.admin ? [["direct","直接出去"],["existing","已有出口"],["new","新建出口"]] : [["existing","已有出口"],["new","新建出口"]]).map(([v,label])=><ChoiceCard key={v} selected={mode===v}><Radio value={v}/>{label}</ChoiceCard>)}
    </RadioGroup>
    {mode==="existing" && <><Field label="出口" name="exit_id" required><Select required value={exitId?String(exitId):""} onValueChange={v=>onExitId(Number(v))}><SelectTrigger><SelectValue>{items.find(e=>e.id===exitId) ? exitTitle(items.find(e=>e.id===exitId)!) : "请选择出口"}</SelectValue></SelectTrigger><SelectContent>{items.map(e=><SelectItem key={e.id} value={String(e.id)}>{exitTitle(e)} · {e.tag}</SelectItem>)}</SelectContent></Select></Field>{!exits.data && <State loading={exits.isPending} error={exits.error} onRetry={()=>exits.refetch()}/>}</>}
    {mode==="new" && <ExitFields {...fields}/>}
  </div>;
}
