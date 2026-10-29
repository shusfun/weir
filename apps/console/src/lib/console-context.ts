// 页面感知：每个页面、弹窗、向导主动声明自己的上下文（页面名、对象、步骤、表单字段），不由系统从界面猜测。
// 抽屉打开过的那个浏览器标签把合并后的上下文报给中心，dsh 经 get_console_context 读取；不传截图、DOM 或页面原文。
import { useEffect, useRef } from "react";

export interface CtxObject {
  type: string;
  name: string;
  id: number;
}
export interface CtxFormField {
  key: string;
  label: string;
  value?: string;
  secret?: boolean;
  filled?: boolean;
}
export interface CtxLayer {
  page?: string;
  tab?: string;
  object?: CtxObject;
  dialog?: { title: string; step?: string };
  list?: { filters?: Record<string, string>; selected?: number; items?: CtxObject[] };
  form?: CtxFormField[];
  notices?: string[];
}

// 结构变化（换页、换步、选中对象）与表单输入分开：表单停止输入 1 秒后才上报。
type Change = "structure" | "form";
const layers = new Map<number, CtxLayer>();
const listeners = new Set<(c: Change) => void>();
let seq = 0;
let lastError: { code: string; message: string; at: string } | undefined;

function emit(c: Change) {
  for (const fn of listeners) fn(c);
}

export function subscribeContext(fn: (c: Change) => void) {
  listeners.add(fn);
  return () => void listeners.delete(fn);
}

// useConsoleContext 声明一层上下文；组件卸载时撤回。后声明的层（弹窗、向导）覆盖先声明的（页面）。
export function useConsoleContext(layer: CtxLayer | null) {
  const id = useRef(0);
  if (id.current === 0) id.current = ++seq;
  const key = JSON.stringify(layer);
  useEffect(() => {
    const prev = layers.get(id.current);
    if (layer) layers.set(id.current, layer);
    else layers.delete(id.current);
    const formOnly = prev && layer && JSON.stringify({ ...prev, form: undefined }) === JSON.stringify({ ...layer, form: undefined });
    emit(formOnly ? "form" : "structure");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  useEffect(() => () => {
    layers.delete(id.current);
    emit("structure");
  }, []);
}

// reportError 记下最近一次失败提示（错误码与文案），由 api() 调用。
export function reportError(code: string, message: string) {
  lastError = { code, message, at: new Date().toISOString() };
  emit("structure");
}

// snapshot 合并所有层：标量取最后声明的，表单字段与提示累加。
export function snapshot(): CtxLayer & { declared: boolean; last_error?: typeof lastError } {
  const out: CtxLayer = {};
  const form: CtxFormField[] = [];
  const notices: string[] = [];
  for (const l of layers.values()) {
    for (const k of ["page", "tab", "object", "dialog", "list"] as const) {
      if (l[k] !== undefined) (out as Record<string, unknown>)[k] = l[k];
    }
    form.push(...(l.form ?? []));
    notices.push(...(l.notices ?? []));
  }
  return { ...out, form: form.length ? form : undefined, notices: notices.length ? notices : undefined, declared: layers.size > 0, last_error: lastError };
}

// field 生成一个表单字段的声明；密码类只标已填 / 未填，值不离开浏览器。
export function field(key: string, label: string, value: unknown, secret = false): CtxFormField {
  const text = value === undefined || value === null ? "" : String(value);
  return secret ? { key, label, secret: true, filled: text !== "" } : { key, label, value: text, filled: text !== "" };
}

// drawerId 是这个浏览器标签的抽屉会话编号（只存在本标签的 sessionStorage）；抽屉没打开过时为空。
const drawerKey = "weir-dsh-drawer";
export function drawerId(create = false): string {
  try {
    let id = sessionStorage.getItem(drawerKey) ?? "";
    if (!id && create) {
      const bytes = crypto.getRandomValues(new Uint8Array(16));
      id = "d" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
      sessionStorage.setItem(drawerKey, id);
    }
    return id;
  } catch {
    return "";
  }
}
