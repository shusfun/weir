import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@weir/ui";
import { FlaskConical } from "lucide-react";
import { api } from "../api";
import { queryClient } from "../lib/query";
import { fetchJob, isJobGone, jobKey, jobRefetch, jobRetry, jobGoneText, type Job } from "../lib/jobs";
import { date } from "../lib/models";
import type { Gate, NodeItem, NodeTest, NodeTestCheck } from "../lib/core";

type RequestState = { submitting?: boolean; jobId?: string; error?: string };

function useNodeTest(n: NodeItem) {
  const key = ["node-test-ui", n.id];
  const local = useQuery<RequestState>({ queryKey: key, queryFn: () => ({}), enabled: false, initialData: {} }).data;
  const id = local.jobId ?? (n.test?.status === "running" ? n.test.job_id : undefined);
  const job = useQuery<Job>({ queryKey: jobKey(id ?? null), queryFn: () => fetchJob(id!), enabled: !!id, refetchInterval: jobRefetch, retry: jobRetry });
  const status = job.data?.status;
  useEffect(() => {
    if (status && status !== "running" || isJobGone(job.error)) {
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["node"] });
    }
  }, [id, status, job.error]);
  const response = job.data?.result as { body?: { test?: NodeTest } } | undefined;
  const completed = response?.body?.test;
  const result = n.test && n.test.status !== "running" && (!completed || n.test.job_id === id || n.test.at >= completed.at) ? n.test : completed ?? n.test;
  const lost = isJobGone(job.error);
  const busy = !!local.submitting || (!lost && !!id && (!job.data || status === "running"));
  const error = local.error || (lost ? jobGoneText : job.error instanceof Error ? job.error.message : "") || (status === "cancelled" ? "测试已取消" : "");
  const run = async () => {
    if (busy || queryClient.getQueryData<RequestState>(key)?.submitting) return;
    queryClient.setQueryData(key, { submitting: true });
    try {
      await api(`/admin/nodes/${n.id}/test`, "POST", {}, { onJob: (j) => queryClient.setQueryData(key, { submitting: false, jobId: j.id }) });
    } catch (e) {
      queryClient.setQueryData<RequestState>(key, (old) => ({ ...old, error: e instanceof Error ? e.message : String(e) }));
    } finally {
      queryClient.setQueryData<RequestState>(key, (old) => ({ ...old, submitting: false }));
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["node"] });
    }
  };
  return { busy, error, result, step: job.data?.step, run };
}

const checkText = (c: NodeTestCheck) => ({ passed: "通过", failed: "失败", unknown: "未知", not_applicable: "不适用" })[c.status];

export function NodeTestControl({ node, gate, detail = false }: { node: NodeItem; gate?: Gate; detail?: boolean }) {
  const t = useNodeTest(node);
  const disabledReason = !node.enabled ? "节点已停用" : !gate?.online ? "VPS 离线" : gate.status !== "active" ? "VPS 已撤销" : "";
  const r = t.result;
  const title = t.error || (t.busy ? t.step || "正在提交节点测试" : r?.why) || "使用有效用户凭据实测，流量计入该用户用量";
  const tone = t.error || r?.status === "failed" ? "text-[var(--weir-bad-ink)]" : r?.status === "passed" ? "text-[var(--weir-ok-ink)]" : "text-[var(--weir-ink-3)]";
  return (
    <div className={"flex min-w-0 items-start gap-2 " + (detail ? "w-full" : "")}>
      <Button className="order-last" size="icon-sm" variant="ghost" busy={t.busy} busyText={`正在测试 ${node.name}`} disabled={!!disabledReason}
        title={disabledReason || "实测节点"} aria-label={`实测节点 ${node.name}`} onClick={() => void t.run()}><FlaskConical /></Button>
      <div className={"grid h-16 min-w-0 flex-1 content-center text-meta " + tone} role="status" aria-live="polite" aria-atomic="true" title={title}>
        <span className="block truncate">{t.error ? "实测未完成" : t.busy ? "正在实测…" : !r ? "尚未实测" : r.status === "passed" ? "实测通过" : r.status === "failed" ? "实测失败" : "实测未知"}
          {!t.busy && r && ` · TCP ${checkText(r.tcp)} · UDP ${checkText(r.udp)}`}</span>
        <span className="block truncate text-[var(--weir-ink-3)]">{t.busy ? t.step || "正在提交，等候中心任务" : r ? `${date(r.at)}${r.location === "colocated" ? " · 同机实测，外部可达性未知" : ""}` : "点击图标测试真实连接"}</span>
        <span className="block truncate text-[var(--weir-ink-3)]">{t.busy ? "\u00a0" : t.error || r?.why || disabledReason || "\u00a0"}</span>
      </div>
    </div>
  );
}
