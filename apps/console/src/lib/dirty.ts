import { useEffect, useRef } from "react";
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
