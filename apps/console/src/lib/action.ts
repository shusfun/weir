// 请求动作：统一忙碌状态、成功 / 失败提示与刷新查询，替代各页手写的 run / act / save（busy + try/catch + toast）。
// useRowAction：列表行按钮。按 key 标记正在操作的行，成功 toast + 刷新，失败 toast。
// useFormAction：表单提交。失败时错误码能对应到字段的交给 <Form errors> 标在字段下方，其余作为表单错误显示在底部。
import { useMemo, useState } from "react";
import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useToast } from "@weir/ui";
import { ApiError } from "../api";

const messageOf = (e: unknown) => (e instanceof Error ? e.message : String(e));

type Refresh = { invalidate?: QueryKey[]; refetch?: () => unknown };
// 成功提示：固定文案，或按返回结果生成
type RowOk = string | ((r: unknown) => string);
// detail：成功提示的补充说明；fail：失败提示的标题（默认“操作未完成”）
type RowExtra = { detail?: (r: unknown) => string | undefined; fail?: string };

async function refresh(client: ReturnType<typeof useQueryClient>, opts: Refresh) {
  await Promise.all([
    ...(opts.invalidate ?? []).map((queryKey) => client.invalidateQueries({ queryKey })),
    opts.refetch?.(),
  ]);
}

export function useRowAction<K extends string | number = string>(opts: Refresh = {}) {
  const toast = useToast();
  const client = useQueryClient();
  const [busy, setBusy] = useState<K | null>(null);
  const mutation = useMutation({
    mutationFn: ({ fn }: { key: K; fn: () => Promise<unknown>; ok?: RowOk; extra?: RowExtra }) => fn(),
    onMutate: ({ key }) => setBusy(key),
    onSuccess: async (r, { ok, extra }) => {
      const title = typeof ok === "function" ? ok(r) : ok;
      if (title) toast(title, { tone: "ok", description: extra?.detail?.(r) });
      await refresh(client, opts);
    },
    onError: (e, { extra }) => toast(extra?.fail ?? "操作未完成", { description: messageOf(e), tone: "bad" }),
    onSettled: () => setBusy(null),
  });
  // 返回是否成功；失败已提示，调用方不必再 catch
  const run = (key: K, fn: () => Promise<unknown>, ok?: RowOk, extra?: RowExtra) =>
    mutation.mutateAsync({ key, fn, ok, extra }).then(
      () => true,
      () => false,
    );
  return { run, busy, isBusy: (key?: K) => (key === undefined ? busy !== null : busy === key) };
}

export function useFormAction<T = unknown, F extends string = string>(
  fn: () => Promise<T>,
  opts: Refresh & {
    ok?: string;
    // 服务端错误码 → 字段 name
    fields?: Partial<Record<string, F>>;
    onSuccess?: (r: T) => unknown;
    // 失败后的清理（如清空已用过的验证码）；错误本身由返回的 errors / error 显示
    onError?: (e: unknown) => void;
  } = {},
) {
  const toast = useToast();
  const client = useQueryClient();
  const mutation = useMutation({
    mutationFn: fn,
    onSuccess: async (r) => {
      if (opts.ok) toast(opts.ok, { tone: "ok" });
      await refresh(client, opts);
      await opts.onSuccess?.(r);
    },
    onError: (e) => opts.onError?.(e),
  });
  const e = mutation.error;
  const field = e instanceof ApiError ? opts.fields?.[e.code] : undefined;
  const errors = useMemo(() => (e && field ? { [field]: messageOf(e) } : undefined), [e, field]);
  return {
    submit: () => mutation.mutateAsync().then(
      () => true,
      () => false,
    ),
    busy: mutation.isPending,
    // 交给 <Form errors>
    errors,
    // 不属于任何字段的错误：显示在表单底部
    error: e && !field ? messageOf(e) : "",
    raw: e,
    reset: mutation.reset,
  };
}
