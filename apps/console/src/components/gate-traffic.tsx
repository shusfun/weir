import { useState } from "react";
import { Settings2 } from "lucide-react";
import { Button, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Field, Form, Input, Notice, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction } from "../lib/action";
import { bytes } from "../lib/models";
import { field, useConsoleContext } from "../lib/console-context";
import type { ObjectState } from "../lib/core";
import { State } from "./feedback";
import { useStringDialogParam } from "./object-link";

interface Traffic {
  settings: { quota: number; mode: string; reset_day: number; auto_deprioritize: boolean };
  revision: string; meter_revision: string; used: number | null; period_start: string; period_end: string;
  state: ObjectState; alert: string; auto_deprioritized: boolean;
}

export function GateTraffic({ gateID }: { gateID: number }) {
  const q = useQuery({ queryKey: ["gate-traffic", gateID], queryFn: () => api(`/admin/gates/${gateID}/traffic`) as Promise<Traffic>, refetchInterval: 30000 });
  const dialog = useStringDialogParam("traffic");
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />;
  const data = q.data;
  const configured = data.settings.quota > 0;
  return <section className="stack" data-tour="gate-traffic">
    {configured ? <>
    <SectionHeader flush title="供应商月流量" actions={<Button variant="outline" onClick={() => dialog.open(String(gateID))}>额度与校准</Button>} />
    <p>{data.used == null ? "未知" : bytes(data.used)} / {data.settings.quota ? bytes(data.settings.quota) : "未设置额度"} · {new Date(data.period_end).toLocaleString("zh-CN", { timeZone: "UTC", hour12: false })} 重置（UTC）</p>
    <Notice tone={data.alert === "critical" ? "bad" : data.alert === "warning" ? "warn" : "info"}>{data.alert === "critical" ? "已使用至少 95%，临近供应商额度。" : data.alert === "warning" ? "已使用至少 80%，请关注剩余额度。" : ""}{data.state.reason}。{data.auto_deprioritized ? "内置自动候选已后移，客户端仍按实际探测选路。" : data.settings.auto_deprioritize ? "达到 95% 后降低内置自动候选优先级。" : "仅提醒。"}不影响用户额度。</Notice>
    </> : <div><Button variant="outline" onClick={() => dialog.open(String(gateID))}><Settings2 />设置流量</Button></div>}
    {dialog.value === String(gateID) && <TrafficForm gateID={gateID} data={data} close={dialog.close} saved={() => { dialog.close(); void q.refetch(); }} />}
  </section>;
}

function TrafficForm({ gateID, data, close, saved }: { gateID: number; data: Traffic; close: () => void; saved: () => void }) {
  const [quota, setQuota] = useState(String(data.settings.quota / 1073741824));
  const [mode, setMode] = useState(data.settings.mode);
  const [day, setDay] = useState(String(data.settings.reset_day));
  const [deprioritize, setDeprioritize] = useState(data.settings.auto_deprioritize);
  const [calibration, setCalibration] = useState("");
  const save = useFormAction(() => api(`/admin/gates/${gateID}/traffic`, "PUT", { quota: Math.round(Number(quota) * 1073741824), mode, reset_day: Number(day), auto_deprioritize: deprioritize, revision: data.revision, meter_revision: data.meter_revision, ...(calibration.trim() ? { calibration: Math.round(Number(calibration) * 1073741824) } : {}) }), { onSuccess: saved });
  useConsoleContext({ object: { type: "gate", id: gateID, name: `VPS ${gateID}` }, dialog: { title: "供应商额度与校准" }, form: [field("quota", "月额度 GiB", quota), field("reset_day", "重置日", day), field("calibration", "本周期已用 GiB", calibration)] });
  return <Dialog open onOpenChange={open => !open && !save.busy && close()}><DialogContent size="md">
    <DialogTitle>供应商额度与校准</DialogTitle><DialogDescription>网卡用量独立于用户账单。重置日按 UTC，短月份使用最后一天；0 额度表示仅记录。</DialogDescription>
    <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
      <Field name="quota" label="月额度（GiB）" required><Input type="number" min="0" max="8388607" step="any" required value={quota} onChange={e => setQuota(e.target.value)} /></Field>
      <Field name="mode" label="供应商计费方式"><Select value={mode} onValueChange={v => v && setMode(v)}><SelectTrigger><SelectValue>{({ sum: "双向合计", outbound: "只算出站", max: "取较大一方" } as Record<string, string>)[mode] ?? "未知"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="sum">双向合计</SelectItem><SelectItem value="outbound">只算出站</SelectItem><SelectItem value="max">取较大一方</SelectItem></SelectContent></Select></Field>
      <Field name="reset_day" label="每月重置日" required><Input type="number" min="1" max="31" step="1" required value={day} onChange={e => setDay(e.target.value)} /></Field>
      <SwitchField checked={deprioritize} onCheckedChange={setDeprioritize} label="临近额度时降低自动候选优先级" description="达到 95% 后，在下一次订阅更新时将该 VPS 的节点后移。手选顺序和自定义分组不变；不会停用节点，客户端仍按探测结果选择。" />
      <Field name="calibration" label="本周期已用校准（GiB）" hint="留空保留当前累计；填写后从供应商账单的当前用量继续累加。观测已更新时需重新打开再确认。"><Input type="number" min="0" max="8388607" step="any" value={calibration} disabled={data.used == null} onChange={e => setCalibration(e.target.value)} /></Field>
      <DialogFooter error={save.error}><Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter>
    </Form>
  </DialogContent></Dialog>;
}
