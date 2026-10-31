// 请求动作：统一忙碌状态、等待提示、成功 / 失败提示与刷新查询，替代各页手写的 run / act / save（busy + try/catch + toast）。
// useRowAction：列表行按钮。按 key 标记正在操作的行，成功 toast + 刷新，失败 toast。
// useFormAction：表单提交。失败时错误码能对应到字段的交给 <Form errors> 标在字段下方，其余作为表单错误显示在底部。
// 等待提示：请求超过 2 秒时，wait / waitFor(key) 返回正在等的事（交给 <WaitText>），写「正在<做什么> · 已等 N 秒，最多 M 秒」；
// 中心把操作转成后台任务时（见 api.ts），请求函数收到的 track 把任务交给等待行，改为显示任务推来的当前步骤。
import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useToast } from "@weir/ui";
import { ApiError, type ApiOptions } from "../api";
import type { Job } from "./jobs";

const messageOf = (e: unknown) => (e instanceof Error ? e.message : String(e));

type Refresh = { invalidate?: QueryKey[]; refetch?: () => unknown };
// 成功提示：固定文案，或按返回结果生成
type RowOk = string | ((r: unknown) => string);
// WaitSpec：label 是「正在<做什么>（<对象>）」，max 是最多等多久（秒，说不准时省略）
export type WaitSpec = { label: string; max?: number };
export type Pending = WaitSpec & { since: number; job?: Job };
// Track 交给 api() 的第四个参数：转成后台任务的操作经它更新等待行
export type Track = Required<Pick<ApiOptions, "onJob">>;
// detail：成功提示的补充说明；fail：失败提示的标题（默认“操作未完成”）；wait：等待提示
type RowExtra = { detail?: (r: unknown) => string | undefined; fail?: string; wait?: WaitSpec };

async function refresh(client: ReturnType<typeof useQueryClient>, opts: Refresh) {
  await Promise.all([
    ...(opts.invalidate ?? []).map((queryKey) => client.invalidateQueries({ queryKey })),
    opts.refetch?.(),
  ]);
}

// useSlow：请求开始 2 秒后才返回它（短请求只有按钮忙碌，不出现等待行）；没有说明也没有转成任务的不显示。
function useSlow<P extends Pending>(p: P | null) {
  const [slow, setSlow] = useState(false);
  const since = p?.since;
  useEffect(() => {
    if (since === undefined) {
      setSlow(false);
      return;
    }
    const left = 2000 - (Date.now() - since);
    setSlow(left <= 0);
    if (left <= 0) return;
    const t = setTimeout(() => setSlow(true), left);
    return () => clearTimeout(t);
  }, [since]);
  return p && slow && (p.label || p.job) ? p : null;
}

function useMounted() {
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return mounted;
}

export function useRowAction<K extends string | number = string>(opts: Refresh = {}) {
  const toast = useToast();
  const client = useQueryClient();
  const [busy, setBusy] = useState<K | null>(null);
  const [pending, setPending] = useState<(Pending & { key: K }) | null>(null);
  const mutation = useMutation({
    mutationFn: ({ key, fn, extra }: { key: K; fn: (track: Track) => Promise<unknown>; ok?: RowOk; extra?: RowExtra }) => {
      setPending({ key, label: extra?.wait?.label ?? "", max: extra?.wait?.max, since: Date.now() });
      return fn({ onJob: (j) => setPending((p) => (p && p.key === key ? { ...p, job: j } : p)) });
    },
    onMutate: ({ key }) => setBusy(key),
    onSuccess: async (r, { ok, extra }) => {
      const title = typeof ok === "function" ? ok(r) : ok;
      if (title) toast(title, { tone: "ok", description: extra?.detail?.(r) });
      await refresh(client, opts);
    },
    onError: (e, { extra }) => toast(extra?.fail ?? "操作未完成", { description: messageOf(e), tone: "bad" }),
    onSettled: () => {
      setBusy(null);
      setPending(null);
    },
  });
  const slow = useSlow(pending);
  // 返回是否成功；失败已提示，调用方不必再 catch
  const run = (key: K, fn: (track: Track) => Promise<unknown>, ok?: RowOk, extra?: RowExtra) =>
    mutation.mutateAsync({ key, fn, ok, extra }).then(
      () => true,
      () => false,
    );
  return {
    run,
    busy,
    isBusy: (key?: K) => (key === undefined ? busy !== null : busy === key),
    // 这一行正在等的事（超过 2 秒才有），交给 <WaitText>
    waitFor: (key: K) => (slow && slow.key === key ? slow : null),
  };
}

export function useFormAction<T = unknown, F extends string = string>(
  fn: (track: Track) => Promise<T>,
  opts: Refresh & {
    ok?: string;
    // 服务端错误码 → 字段 name
    fields?: Partial<Record<string, F>>;
    onSuccess?: (r: T) => unknown;
    // 失败后的清理（如清空已用过的验证码）；错误本身由返回的 errors / error 显示
    onError?: (e: unknown) => void;
    // 等待提示；按提交时的输入生成时传函数
    wait?: WaitSpec | (() => WaitSpec);
  } = {},
) {
  const toast = useToast();
  const client = useQueryClient();
  const mounted = useMounted();
  const [pending, setPending] = useState<Pending | null>(null);
  const last = useRef<Pending | null>(null);
  // left：人在请求进行中关掉了窗口（操作在中心后台继续）。结果改用提示告诉他，不再写回已经关掉的窗口
  const left = useRef(false);
  const away = () => !mounted.current || left.current;
  const track = (p: Pending | null) => {
    last.current = p;
    setPending(p);
  };
  const mutation = useMutation({
    mutationFn: () => {
      const spec = typeof opts.wait === "function" ? opts.wait() : opts.wait;
      left.current = false;
      track({ label: spec?.label ?? "", max: spec?.max, since: Date.now() });
      return fn({ onJob: (j) => last.current && track({ ...last.current, job: j }) });
    },
    onSuccess: async (r) => {
      // 窗口已经关掉（操作在后台做完）：没有地方显示结果，用提示告诉人
      const gone = away();
      if (opts.ok) toast(opts.ok, { tone: "ok" });
      else if (gone) toast((last.current?.job?.title ?? "操作") + "：已完成", { tone: "ok" });
      await refresh(client, opts);
      if (!gone) await opts.onSuccess?.(r);
    },
    onError: (e) => {
      if (away()) toast((last.current?.job?.title ?? "操作") + "：没有完成", { description: messageOf(e), tone: "bad" });
      else opts.onError?.(e);
    },
    onSettled: () => track(null),
  });
  const wait = useSlow(pending);
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
    // 正在等的事（超过 2 秒才有），交给 <WaitText>
    wait,
    // 已经转成后台任务：可以关掉窗口，操作在中心继续
    detachable: !!pending?.job,
    // 请求进行中关掉窗口时调用：结果改用提示显示
    leave: () => {
      if (mutation.isPending) left.current = true;
    },
  };
}
