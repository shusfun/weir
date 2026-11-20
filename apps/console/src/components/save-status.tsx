// 设置弹窗底部的操作状态：弹窗里的各张设置卡片自己发请求，这一行统一回答「做成了没有」。
// 只看弹窗打开之后发起的请求；成功后刷新页面上的查询，弹窗外那一行的状态随之更新。
import { useEffect, useRef, useState } from "react";
import { useIsMutating, useQueryClient } from "@tanstack/react-query";
import { Button, DialogFooter } from "@weir/ui";
import { date } from "../lib/models";

type Status = { kind: "ok"; at: number; text: string } | { kind: "bad"; message: string } | null;

// 成功文案：行操作在提交参数里带 ok，表单操作放在 meta.ok；按结果生成的文案或没有文案时写「已完成」。
function okText(variables: unknown, meta: Record<string, unknown> | undefined) {
  const fromRow = variables && typeof variables === "object" && "ok" in variables ? (variables as { ok?: unknown }).ok : undefined;
  const text = typeof fromRow === "string" ? fromRow : typeof meta?.ok === "string" ? meta.ok : "";
  return text || "已完成";
}

export function useSaveStatus() {
  const client = useQueryClient();
  const busy = useIsMutating() > 0;
  const [status, setStatus] = useState<Status>(null);
  const opened = useRef(Date.now());
  useEffect(() => client.getMutationCache().subscribe((event) => {
    if (event.type !== "updated" || event.action.type !== "success" && event.action.type !== "error") return;
    if (event.mutation.state.submittedAt < opened.current) return;
    if (event.action.type === "success") {
      setStatus({ kind: "ok", at: Date.now(), text: okText(event.mutation.state.variables, event.mutation.options.meta) });
      void client.invalidateQueries({ type: "active" });
    } else {
      const error = event.mutation.state.error;
      setStatus({ kind: "bad", message: error instanceof Error ? error.message : "原因未知" });
    }
  }), [client]);
  return { busy, status };
}

// 用弹窗自带的固定底栏：滚动到哪里都看得到结果，错误放在按钮左侧。
export function SaveStatusBar({ busy, status, close }: ReturnType<typeof useSaveStatus> & { close: () => void }) {
  const ok = !busy && status?.kind === "ok" ? `${status.text} · ${date(status.at)}` : "";
  return <DialogFooter
    error={!busy && status?.kind === "bad" ? `没有完成：${status.message}` : undefined}
    status={busy ? "正在处理…" : ok && <span className="text-[var(--weir-ok-ink)]">{ok}</span>}>
    <Button type="button" variant="outline" disabled={busy} onClick={close}>关闭</Button>
  </DialogFooter>;
}
