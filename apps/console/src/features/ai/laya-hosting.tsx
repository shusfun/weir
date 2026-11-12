import { useState } from "react";
import { ActionGroup, Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, InsetRow, InsetRows, Notice, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { useConsoleContext } from "../../lib/console-context";
import type { Job } from "../../lib/jobs";
import { date } from "../../lib/models";
import { State } from "../../components/feedback";
import { JobLine } from "../../components/job";
import { DiagnosticExport } from "../../components/diagnostic-export";

type Operation = "ensure" | "stop" | "remove";
type Approved = { version: string; model: string; spec: { repository: string; digest: string; host_port: number; uid: number; gid: number; memory_bytes: number; cpu_millis: number; model_volume: string } };
type Activity = { id: string; operation: Operation; status: string; stage: string; code?: string; trace_id: string; created_at: string; stages: { name: string; code?: string; started_at: string; finished_at: string }[] };
type Hosting = { available: boolean; state: string; code?: string; approved?: Approved; operations?: Activity[] };
type Preview = { id: string; operation: Operation; before: string; deployment_revision: string; approved: Approved; expires_at: string };
const names: Record<Operation, string> = { ensure: "安装或启动", stop: "停止", remove: "移除容器" };
const states: Record<string, string> = { running: "运行中", stopped: "已停止", starting: "启动中", failed: "失败", unknown: "未知", succeeded: "已完成" };

export function LayaHosting({ onUse }: { onUse: (url: string, model: string) => void }) {
  const q = useQuery({ queryKey: ["laya-hosting"], queryFn: ({ signal }) => api("/admin/defense/laya/hosting", "GET", undefined, { signal }) as Promise<Hosting> });
  const [preview, setPreview] = useState<Preview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [job, setJob] = useState<Job>();
  useConsoleContext({ notices: ["中心托管官方 Laya；部署参数由 root 持有的批准档案限定，操作需预演确认。"], ...(preview ? { dialog: { title: "确认中心 Laya 托管操作" } } : {}) });

  async function plan(operation: Operation) {
    setBusy(true); setError(""); setNotice("");
    try { setPreview(await api("/admin/defense/laya/hosting/preview", "POST", { operation }) as Preview); }
    catch (e) { setError(e instanceof Error ? e.message : "无法预演托管操作"); }
    finally { setBusy(false); }
  }

  async function confirm() {
    if (!preview) return;
    setBusy(true); setError("");
    try {
      const result = await api(`/admin/defense/laya/hosting/${preview.id}/confirm`, "POST");
      if (result.job) setJob(result.job as Job);
      setNotice(result.duplicate ? "这份确认已提交，请查看活动记录。" : "已提交托管操作；完成后核对状态，再确认接入设置。");
      setPreview(null); void q.refetch();
    } catch (e) { setError(e instanceof Error ? e.message : "提交托管操作失败"); }
    finally { setBusy(false); }
  }

  return <Collapsible title="中心托管官方 Laya" data-tour="laya-hosting">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <div className="stack">
      {!q.data.available ? <Notice>{q.data.code === "laya_hosting_unsupported" ? "此平台支持连接已有实例；托管需要 Linux amd64 或 arm64。" : "尚未取得 root 批准的部署档案。先由服务器管理员配置固定版本、官方镜像摘要、模型卷、非 root 身份及资源上限，再刷新。"}</Notice> : <>
        <InsetRows>
          <InsetRow><span>容器状态</span><Badge variant="status" tone={q.data.state === "running" ? "ok" : "neutral"}>{states[q.data.state] ?? "未知"}</Badge></InsetRow>
          <InsetRow><span>批准版本</span><span>{q.data.approved?.version ?? "未知"}</span></InsetRow>
          <InsetRow><span>官方模型</span><span>{q.data.approved?.model ?? "未知"}</span></InsetRow>
        </InsetRows>
        {q.data.code && <Notice>{q.data.code === "laya_container_missing" ? "尚未创建托管容器。" : `未取得可用容器状态（${q.data.code}）。`}</Notice>}
        <Notice>默认使用官方 CPU Docker 镜像，不编译源码。只发布回环端口；移除保留模型卷。安装成功后，仍需在接入表单保存并启用。</Notice>
        <ActionGroup>
          <Button disabled={busy || job?.status === "running"} busy={busy && !preview} busyText="正在预演…" onClick={() => void plan("ensure")}>预演安装或启动</Button>
          <Button variant="outline" disabled={busy || job?.status === "running"} onClick={() => void plan("stop")}>预演停止</Button>
          <Button variant="outline" disabled={busy || job?.status === "running"} onClick={() => void plan("remove")}>预演移除</Button>
          <Button variant="ghost" disabled={busy || q.data.state !== "running" || !q.data.approved} onClick={() => { if (q.data.approved) onUse(`http://127.0.0.1:${q.data.approved.spec.host_port}`, q.data.approved.model); }}>填入接入表单</Button>
        </ActionGroup>
      </>}
      <ActionGroup><Button variant="ghost" disabled={q.isFetching} onClick={() => void q.refetch()}>刷新托管状态</Button></ActionGroup>
      <JobLine job={job} idle="尚未提交托管操作" detailDescription="取消后容器状态可能已改变。请刷新核对后再操作；安装失败仅尝试停止本次新建容器，保留模型卷。" onFinish={(finished) => { setJob(finished); void q.refetch(); }} />
      {notice && <Notice>{notice}</Notice>}
      {error && !preview && <Notice tone="bad">{error}</Notice>}
      {(q.data.operations?.length ?? 0) > 0 && <Collapsible title="托管活动记录">
        <Table><TableHeader><TableRow><TableHead>时间与操作</TableHead><TableHead>结果</TableHead><TableHead>诊断</TableHead></TableRow></TableHeader><TableBody>
          {q.data.operations?.map((op) => <TableRow key={op.id}>
            <TableCell>{date(op.created_at)} · {names[op.operation]}<small className="block break-all text-meta">关联 {op.trace_id || "未知"}</small></TableCell>
            <TableCell>{states[op.status] ?? "未知"}{op.code && <small className="block text-meta">{op.code}</small>}<Collapsible title="已取得的执行环节"><CodeBlock>{JSON.stringify(op.stages ?? [], null, 2)}</CodeBlock></Collapsible></TableCell>
            <TableCell><DiagnosticExport items={[{ kind: "laya-operation", id: op.id }]} /></TableCell>
          </TableRow>)}
        </TableBody></Table>
      </Collapsible>}
    </div>}
    <Dialog open={preview !== null} onOpenChange={(open) => { if (!open && !busy) { setPreview(null); setError(""); } }}>
      <DialogContent size="lg"><DialogHeader><DialogTitle>确认中心 Laya {preview ? names[preview.operation] : "托管操作"}</DialogTitle><DialogDescription>只作用于当前中心的已批准容器。参数变化、预览过期或账号不一致时拒绝执行。</DialogDescription></DialogHeader>
        {preview && <div className="stack">
          <p>当前：{states[preview.before] ?? "未知"} → {names[preview.operation]}。{preview.operation === "ensure" ? "需要下载官方镜像与固定模型，并执行合成推理检查。" : "Laya 判断可能暂时不可用，基础规则继续工作。"}</p>
          <p>移除不会删除模型卷；失败或取消后先核对实际状态，再预演重试。部署不会自动启用判断。</p>
          <Collapsible title="完整批准参数"><CodeBlock>{JSON.stringify(preview.approved, null, 2)}</CodeBlock></Collapsible>
        </div>}
        <DialogFooter error={error}><Button variant="ghost" disabled={busy} onClick={() => { setPreview(null); setError(""); }}>取消</Button><Button disabled={busy || !preview} busy={busy} busyText="正在提交…" onClick={() => void confirm()}>确认执行</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </Collapsible>;
}
