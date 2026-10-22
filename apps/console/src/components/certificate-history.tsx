import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Notice, SectionHeader, Button, Card, CardContent, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Badge } from "@migate/ui";
import type { Device, Resource, Task } from "../lib/models";
import { date } from "../lib/models";
import { api } from "../api";
import { Status } from "./feedback";

type History = { validity: string; observed_at: string; total: number; removable: number; preview_digest: string; items: { version: string; certificate_id: number; created_at: string; protected: boolean }[] };
export function CertificateHistoryPanel({ device, tasks, certificates }: { device: Device; tasks: Task[]; certificates: Resource[] }) {
  const cache = useQueryClient(), [pending, setPending] = useState<History | null>(null), [busy, setBusy] = useState(false), [error, setError] = useState(""), [ticket, setTicket] = useState<number | null>(null);
  const core = device.metadata.core_status as { observed_at?: string; validity?: string; certificate_history?: History } | undefined;
  const history = core?.certificate_history;
  const supported = (device.metadata.capabilities as string[] | undefined)?.includes("certificate.history.v1");
  const age = Date.now() - Date.parse(history?.observed_at ?? "");
  const fresh = device.status === "approved" && core?.validity !== "unknown" && history?.validity === "fresh" && Number.isFinite(age) && age >= -5000 && age < 60000;
  const task = tasks.find(t => t.id === ticket), running = !!ticket && (!task || ["pending", "running"].includes(task.status));
  async function submit() {
    if (!pending) return;
    setBusy(true); setError("");
    try {
      const result = await api(`/admin/devices/${device.id}/tasks`, "POST", { operation: "certificate.clean", payload: { preview_digest: pending.preview_digest }, confirm: true });
      setTicket(result.id); setPending(null); await cache.invalidateQueries({ queryKey: ["tasks"] });
    } catch (e) { setError(e instanceof Error ? e.message : "清理任务未能创建，请重试。"); }
    finally { setBusy(false); }
  }
  return <Card><CardContent className="stack">
    <SectionHeader level="h3" title="证书历史版本" description="仅处理本项目登记的证书文件；保留每张证书最近两个版本及当前配置仍引用的版本。未登记文件保留。" actions={<Button variant="outline" onClick={() => cache.invalidateQueries({ queryKey: ["devices"] })}>刷新历史观测</Button>} />
    {!supported ? <p role="status">此 Agent 尚不支持历史管理，请先升级。</p> : !fresh ? <p role="status">历史观测未知或已过期，暂不能清理。请等待设备恢复上报。</p> : <>
      <p className="muted">实际观测 {date(history!.observed_at)} · 来源：Gate 文件登记</p>
      {history!.total > history!.items.length && <p>登记共 {history!.total} 个版本，此处展开 {history!.items.length} 个；清理数量按完整登记核算。</p>}
      {history!.items.length ? <div className="stack" aria-label="证书历史清单">{history!.items.map(row => <div className="list-row" key={row.version}><div><strong>{String(certificates.find(c => Number(c.data.id) === row.certificate_id)?.data.name || "证书 #" + row.certificate_id)} · {row.version.split("-").at(-1)?.slice(0, 8)}</strong><small>登记 {date(row.created_at)}</small></div><Badge variant="secondary">{row.protected ? "保留" : "可清理"}</Badge></div>)}</div> : <p role="status">尚无登记版本。旧版遗留文件不会自动纳入清理；下次导入或更新证书后开始登记。</p>}
      <Button variant="outline" disabled={!history!.removable || running} onClick={() => { setPending(history!); setError(""); }}>清理历史版本（{history!.removable}）</Button>
    </>}
    {ticket && <Notice role="status">任务 #{ticket} · <Status value={task?.status ?? "pending"} />{task?.status === "applied" ? " 已完成，请等待下一次历史观测。" : task?.status === "failed" ? " 未完成清理，请在执行任务中查看原因并重新核对历史。" : " 等待设备执行，不代表文件已删除。"}</Notice>}
    {error && !pending && <Notice tone="bad">{error}</Notice>}
    <Dialog open={!!pending} onOpenChange={open => { if (!open && !busy) setPending(null); }}><DialogContent size="md"><DialogTitle>确认清理证书历史</DialogTitle><DialogDescription>将清理 {pending?.removable} 个登记版本的证书与私钥文件。保留当前引用和最近两个版本，不重启内核。清单变化时设备会拒绝本次任务。</DialogDescription>{error && <Notice tone="bad">{error}</Notice>}<DialogFooter><Button variant="outline" disabled={busy} onClick={() => setPending(null)}>取消</Button><Button disabled={busy} onClick={submit}>{busy ? "提交中…" : "确认清理历史"}</Button></DialogFooter></DialogContent></Dialog>
  </CardContent></Card>;
}
