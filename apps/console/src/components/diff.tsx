// DiffView：逐行差异（新增绿、删除红，未变的长段落折叠成一行）。驱动草稿、驱动历史与保存前检查共用。
export interface DiffLine {
  op: " " | "+" | "-" | "…";
  text: string;
}

const lineClass: Record<string, string> = {
  "+": "bg-[var(--mg-ok-soft)] text-[var(--mg-ok-ink)]",
  "-": "bg-[var(--mg-bad-soft)] text-[var(--mg-bad-ink)]",
  "…": "text-[var(--mg-ink-3)]",
  " ": "",
};

export function DiffView({ lines, empty = "没有改动。" }: { lines: DiffLine[] | null | undefined; empty?: string }) {
  if (!lines || !lines.some((l) => l.op === "+" || l.op === "-")) return <small className="text-[var(--mg-ink-3)]">{empty}</small>;
  return (
    <pre className="max-h-80 overflow-auto rounded-[var(--mg-radius)] border border-[var(--mg-line)] bg-[var(--mg-well)] p-2 text-xs leading-5" aria-label="改动">
      {lines.map((l, i) => <div key={i} className={lineClass[l.op] ?? ""}>{(l.op === "…" ? "  " : l.op + " ") + l.text}</div>)}
    </pre>
  );
}
