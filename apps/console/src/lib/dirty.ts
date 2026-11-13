import { useEffect, useRef, useState, type SetStateAction } from "react";
import { useConfirm } from "@weir/ui";
const dirty = new Set<symbol>();
export function hasUnsavedChanges() { return dirty.size > 0; }

export function useDraft<T>(remote: T) {
  const remoteKey = JSON.stringify(remote);
  const [state, setState] = useState(() => ({ value: remote, baseline: remoteKey, remote: remoteKey }));
  const changed = JSON.stringify(state.value) !== state.baseline;
  const clear = useUnsavedChanges(changed);
  useEffect(() => {
    setState(previous => {
      if (previous.remote === remoteKey) return previous;
      return JSON.stringify(previous.value) === previous.baseline
        ? { value: remote, baseline: remoteKey, remote: remoteKey }
        : { ...previous, remote: remoteKey };
    });
  }, [remoteKey]);
  return {
    value: state.value, changed,
    set: (value: SetStateAction<T>) => setState(previous => ({ ...previous, value: typeof value === "function" ? (value as (p: T) => T)(previous.value) : value })),
    reset: () => { clear(); setState({ value: remote, baseline: remoteKey, remote: remoteKey }); },
    commit: (value?: T) => { clear(); setState(previous => {const next=value??previous.value;return {...previous,value:next,baseline:JSON.stringify(next)};}); },
  };
}
export function useUnsavedChanges(value: boolean) {
  const id = useRef(Symbol("form"));
  useEffect(() => {
    if (value) dirty.add(id.current); else dirty.delete(id.current);
    const token = id.current;
    const before = (event: BeforeUnloadEvent) => { if (hasUnsavedChanges()) { event.preventDefault(); event.returnValue = ""; } };
    window.addEventListener("beforeunload", before);
    return () => { dirty.delete(token); window.removeEventListener("beforeunload", before); };
  }, [value]);
  return () => dirty.delete(id.current);
}

export function useFormGuard(value: unknown, close: () => void, busy = false) {
  const initial = useRef(JSON.stringify(value));
  const changed = initial.current !== JSON.stringify(value);
  const clear = useUnsavedChanges(changed);
  const confirm = useConfirm();
  return {
    changed,
    commit: () => { initial.current = JSON.stringify(value); clear(); },
    close: async () => {
      if (busy) return;
      if (!changed || await confirm({ title: "放弃未保存修改？", description: "关闭后会丢失当前填写内容。", confirmText: "放弃修改", tone: "danger" })) { clear(); close(); }
    },
  };
}
