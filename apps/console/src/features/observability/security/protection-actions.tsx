import { ActionGroup, Badge, Button, Notice, SectionHeader, useConfirm } from "@weir/ui";
import type { ReactNode } from "react";
import { api } from "../../../api";
import { useQuery, queryClient } from "../../../lib/query";
import { useRowAction } from "../../../lib/action";
import { date } from "../../../lib/models";
import { reopenDrafts } from "../../../components/draft-confirm";
import { State } from "../../../components/feedback";

type Action = {
  id: string;
  automatic: boolean;
  permanent: boolean;
  event_id: number;
  trace_id: string;
  kind: string;
  ip?: string;
  field?: string;
  created_at: string;
  until: string;
  revoked_at?: string;
  notification: string;
  notification_attempts?: number;
  effective_state: string;
  suspended_reason?: string;
};

export function ProtectionActions({ pick }: { pick?: (id:string,name:string)=>ReactNode }) {
  const confirm = useConfirm();
  const q = useQuery({ queryKey: ["protection-actions"], queryFn: async () =>
    (await api("/admin/defense/automatic")).items as Action[], refetchInterval: 10000 });
  const guard = useQuery({ queryKey: ["protection-policy"], queryFn: () => api("/admin/defense/policy") });
  const action = useRowAction<string>({ refetch: () => { void q.refetch(); void guard.refetch(); } });
  const revoke = async (item: Action) => {
    if (!(await confirm({ title: "撤销这条安全处置？", description: "立即停止这条处置，恢复底层规则；其他仍有效的限制继续执行。", confirmText: "撤销", tone: "danger" }))) return;
    await action.run(item.id, () => api(`/admin/defense/automatic/${item.id}/revoke`, "POST"), "已撤销");
  };
  return <section className="stack" data-tour="automatic-protection">
    <SectionHeader title="安全处置与自动操作" description="显示最近 24 小时临时处置及永久策略。滚动一小时次数包含已撤销的自动操作。" />
    {guard.isPending || guard.error ? <State loading={guard.isPending} error={guard.error} onRetry={() => guard.refetch()} /> : (
      <Notice>自动收紧{guard.data.policy.effective.enabled ? "已开启" : "已关闭"} ·
        滚动一小时 {guard.data.hourly_used} / {guard.data.hourly_limit} 次 ·
        最长 {guard.data.policy.effective.max_minutes} 分钟。配置在系统设置的安全防御分区。</Notice>
    )}
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : q.data.length === 0 ? (
      <State empty="尚无临时或永久安全处置" />
    ) : q.data.map((item) => {
      const active = !item.revoked_at && (item.permanent || new Date(item.until).getTime() > Date.now());
      const paused = active && item.automatic && guard.data && !guard.data.policy.effective.enabled;
      return <div key={item.id} className="stack border-b border-[var(--weir-line)] py-2">
        <ActionGroup>{pick?.(item.id,`安全处置 ${item.id}`)}<strong>{item.kind === "block" ? `封禁来源 ${item.ip || "未知"}` : `收紧 ${item.field || "未知"}`}</strong>
          <Badge variant="status">{item.effective_state === "suspended" ? "已暂停" : item.revoked_at ? "已撤销" : paused ? "总开关已暂停" : active ? "生效中" : "已到期"}</Badge>
          <span>{item.automatic ? "自动安全收紧" : "管理员确认"}</span>
        </ActionGroup>
        <small>{date(item.created_at)} · {item.permanent ? "永久，直至撤销" : `到期 ${date(item.until)}`} · 安全事件 #{item.event_id}</small>
        <small className="break-all">trace {item.trace_id || "未知"} · 通知 {({ pending: "等待发送", sent: "已发送", failed: "发送失败，面板保留记录", confirmed: "管理员已确认" } as Record<string, string>)[item.notification] ?? "未知"}</small>
        {item.suspended_reason && <Notice>{item.suspended_reason}</Notice>}
        {item.notification === "failed" && <small>已尝试 {item.notification_attempts ?? "未知"} 次；最多自动重试 5 次，失败不会撤销已生效的防线。</small>}
        {active && <ActionGroup>
          <Button variant="outline" size="sm" disabled={action.isBusy()} busy={action.isBusy(item.id)}
            onClick={() => void revoke(item)}>撤销</Button>
          {!item.permanent && <Button variant="outline" size="sm" disabled={action.isBusy()}
            onClick={() => void action.run(item.id, async () => {
              await api(`/admin/defense/automatic/${item.id}/promote`, "POST");
              void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] }); reopenDrafts();
            }, "永久处置草稿已生成，需确认")}>预演转为永久</Button>}
        </ActionGroup>}
      </div>;
    })}
  </section>;
}
