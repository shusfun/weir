import { useEffect, useRef } from "react";
import { useConfirm } from "@weir/ui";
const dirty = new Set<symbol>();
export function hasUnsavedChanges() { return dirty.size > 0; }
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
    commit: clear,
    close: async () => {
      if (busy) return;
      if (!changed || await confirm({ title: "放弃未保存修改？", description: "关闭后会丢失当前填写内容。", confirmText: "放弃修改", tone: "danger" })) { clear(); close(); }
    },
  };
}
