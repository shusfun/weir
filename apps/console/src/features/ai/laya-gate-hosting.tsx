import { lazy, Suspense, useState } from "react";
const LayaCard = lazy(() => import("../../components/defense").then(module => ({ default: module.LayaCard })));
import { Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { ActionGroup, Badge, Button, Collapsible, Dialog, DialogContent, DialogDescription, DialogTitle, Notice, TextLink, useConfirm } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { useRowAction } from "../../lib/action";
import type { Job } from "../../lib/jobs";
import { State } from "../../components/feedback";
import { JobByID } from "../../components/job";
import { ActionsWait } from "../../components/wait";
import type { ObjectState } from "../../lib/core";
import { hasUnsavedChanges } from "../../lib/dirty";
import { useConsoleContext } from "../../lib/console-context";

type Hosting = { gate_name: string; online: boolean; colocated: boolean; prepared: boolean; can_prepare: boolean; object_state: ObjectState; component: { state: string; code?: string; version?: string; model?: string }; job?: Job; operation?: { status: string; task_id?: string; code?: string; stage?: string } };
type LayaGlobal = { mode?: "center" | "gate"; gate_id?: number; enabled: boolean; verified?: boolean; model: string; warmup_seconds?: number };

export function GateLayaHosting({ gateId, onUse, defaultOpen = false }: { gateId: number; onUse?: () => void; defaultOpen?: boolean }) {
  const client = useQueryClient();
  const q = useQuery({ queryKey: ["gate-laya", gateId], queryFn: () => api(`/admin/gates/${gateId}/laya`) as Promise<Hosting>, enabled: gateId > 0, refetchInterval: 5000 });
  const laya = useQuery({ queryKey: ["laya"], queryFn: () => api("/admin/defense/laya") as Promise<LayaGlobal>, enabled: gateId > 0 });
  const [settings, setSettings] = useState(false);
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { run, isBusy, waitFor } = useRowAction();
  useConsoleContext({ object: { type: "gate", id: gateId, name: q.data?.gate_name || "VPS" } });

  async function operate(operation: "prepare" | "install" | "remove") {
    setBusy(true);
    setError("");
    try {
      const p = await api(`/admin/gates/${gateId}/laya/preview`, "POST", { operation });
      if (!await confirm({
        title: `${operation === "prepare" ? "准备部署权限" : operation === "install" ? "安装或重试" : "移除"}「${q.data?.gate_name}」的 Laya？`,
        description: operation === "prepare" ? "通过已安装的固定宿主服务准备官方 Ollaya 与固定模型批准档案；不开放远程命令，也不启用后台分析。" : operation === "install" ? `使用官方批准版本 ${p.version}，下载固定模型并验证。可以保持零节点；安装完成后可一键设为推理位置并实测启用。` : "移除容器并保留模型数据。",
        confirmText: "确认执行",
      })) return;
      await api(`/admin/gates/${gateId}/laya/${p.id}/confirm`, "POST");
      void q.refetch();
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作未完成");
    } finally {
      setBusy(false);
    }
  }

  async function activateOnGate() {
    const gateName = q.data?.gate_name || `VPS #${gateId}`;
    const model = q.data?.component.model || laya.data?.model || "laya:multilingual";
    const warmup = laya.data?.warmup_seconds ?? 300;
    if (!await confirm({
      title: `将全局 Laya 切换到「${gateName}」并实测启用？`,
      description: `中心会先把推理位置保存到「${gateName}」（模型 ${model}），随即派发一条合成安全事件做预热实测（时限 ${warmup} 秒），实测通过后自动开启全局 Laya 判断。`,
      confirmText: "保存并实测启用",
    })) return;
    setError("");
    await run("enable", async (track) => {
      await api("/admin/defense/laya", "PUT", { mode: "gate", gate_id: gateId, enabled: false, url: "", model, warmup_seconds: warmup, key: "" });
      const res = (await api("/admin/defense/laya/test-async", "POST", undefined, track)) as { verdict: { category: string; confidence: number } };
      if (res.verdict.category === "unknown") {
        await client.invalidateQueries({ queryKey: ["laya"] });
        throw new Error("实测返回「未知」，已保存推理位置但未自动启用判断，请查看运行观测或重试");
      }
      await api("/admin/defense/laya", "PUT", { mode: "gate", gate_id: gateId, enabled: true, url: "", model, warmup_seconds: warmup, key: "" });
      await Promise.all([client.invalidateQueries({ queryKey: ["laya"] }), client.invalidateQueries({ queryKey: ["observe-system-health"] })]);
      onUse?.();
      return res;
    }, `已在「${gateName}」启用 Laya 判断`, {
      detail: (r) => `合成事件置信度 ${Math.round((r as { verdict: { confidence: number } }).verdict.confidence * 100)}%`,
      fail: "Laya 实测或启用未完成",
      wait: { label: `正在让「${gateName}」预热模型并实测推理`, max: warmup },
    });
  }

  if (!gateId) return null;
  const activeHere = laya.data?.mode === "gate" && laya.data?.gate_id === gateId && laya.data?.enabled;
  const selectedHere = laya.data?.mode === "gate" && laya.data?.gate_id === gateId && !laya.data?.enabled;
  const summary = q.isPending ? "读取中…" : q.error ? "读取失败" : activeHere ? "正在承担全局判断" : q.data?.object_state.label ?? "状态未知";
  const allowed = (action: string) => q.data?.object_state.next.includes(action) ?? false;
  const canUse = allowed("use") && !activeHere;
  const allBusy = busy || isBusy();

  return <Collapsible variant="section" title="Laya 组件" summary={summary} defaultOpen={defaultOpen}>
    <p className="text-meta text-[var(--weir-ink-3)]">可独立部署 Laya，无须先创建节点；部署完成后可直接设为全局推理 VPS 并实测启用。</p>
    {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <div className="stack">
      {q.data.colocated ? <Notice>本机执行端随中心运行。<TextLink render={<Link to="/system?tab=ai&setting=laya" />}>在 AI 设置中部署与接入中心 Laya</TextLink>。</Notice> : <>
        <div className="flex flex-wrap items-center gap-2">
          <span>{q.data.gate_name}</span>
          <Badge variant="status" tone={q.data.object_state.status === "ok" ? "ok" : "neutral"}>{q.data.object_state.label}</Badge>
          {activeHere && <Badge variant="soft" tone="ok">全局推理使用中</Badge>}
          {selectedHere && <Badge variant="soft" tone="warn">{laya.data?.verified ? "已实测 · 待开启" : "已指向此 VPS · 待实测"}</Badge>}
        </div>
        <small>{q.data.object_state.reason}</small>
        {activeHere && <Notice tone="ok">当前全局 Laya 判断正由这台 VPS 执行（模型 {laya.data?.model || "未知"}）。在 <TextLink render={<Link to="/ops/observe" />}>运行观测</TextLink> 查看判断记录与自动起草。</Notice>}
        <ActionGroup>
          {canUse && <Button disabled={allBusy} busy={isBusy("enable")} busyText="正在实测启用…" onClick={() => void activateOnGate()}>设为推理 VPS 并实测启用</Button>}
          <Button variant={!canUse && !allowed("install") && allowed("prepare") ? "default" : "outline"} disabled={allBusy || !allowed("prepare")} onClick={() => void operate("prepare")}>准备部署权限</Button>
          <Button variant={!canUse && (allowed("install") || !allowed("prepare")) ? "default" : "outline"} disabled={allBusy || !allowed("install")} busy={busy} busyText="正在提交…" onClick={() => void operate("install")}>安装或重试 Laya</Button>
          <Button variant="outline" disabled={allBusy || !allowed("remove")} onClick={() => void operate("remove")}>移除容器</Button>
          <Button variant="ghost" onClick={() => setSettings(true)}><Settings2 />全局接入设置</Button>
        </ActionGroup>
        <ActionsWait wait={waitFor("enable")} />
        <Notice>准备 → 安装与模型下载 → 设为推理 VPS 并实测启用。端口只监听回环；移除容器会保留模型卷。</Notice>
        {q.data.job && <JobByID key={q.data.job.id} id={q.data.job.id} initial={q.data.job} openLogs onFinish={() => void q.refetch()} />}
        {!q.data.job && q.data.operation && <Notice tone={q.data.operation.status === "succeeded" ? "ok" : ["failed", "expired"].includes(q.data.operation.status) ? "bad" : "info"}>{({ pending: "等待执行端领取", leased: "安装与验证进行中", cancelling: "正在取消", succeeded: "操作完成", failed: "操作失败，可重试", expired: "操作超时，请核对后重试", cancelled: "已取消", unknown: "结果未知，请刷新核对" } as Record<string, string>)[q.data.operation.status] || "状态未知"} · {q.data.operation.stage || q.data.operation.code || q.data.operation.task_id}</Notice>}
      </>}
    </div>}
    {error && <Notice tone="bad">{error}</Notice>}
    <Dialog open={settings} onOpenChange={async v => { if (v || !hasUnsavedChanges() || await confirm({ title: "放弃未保存的 Laya 配置？", confirmText: "放弃修改" })) setSettings(v); }}>
      <DialogContent size="lg">
        <DialogTitle>Laya 接入设置与测试</DialogTitle>
        <DialogDescription>修改当前推理位置及允许的参数。保存并测试通过后才能启用后台分析。</DialogDescription>
        {settings && <Suspense fallback={<State loading />}><LayaCard hosting={false} /></Suspense>}
      </DialogContent>
    </Dialog>
  </Collapsible>;
}
